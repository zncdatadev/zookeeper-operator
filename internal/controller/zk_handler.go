package controller

import (
	"context"
	"fmt"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	"github.com/zncdatadev/operator-go/pkg/sidecar"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ZkRoleGroupHandler builds the resources for a Zookeeper server role group.
//
// It embeds reconciler.BaseRoleGroupHandler to inherit the framework's canonical resource
// construction (labels, headless/client Services, builder-built StatefulSet with the data
// PVC, PodDisruptionBudget, sidecar injection), then customizes the returned StatefulSet
// and ConfigMap with Zookeeper specifics (start command, exec probes, TLS volumes, config
// files). The myid init container is injected through the SidecarManager like any other
// container — see registerServerContainers.
type ZkRoleGroupHandler struct {
	reconciler.BaseRoleGroupHandler[*zkv1alpha1.ZookeeperCluster]
}

var _ reconciler.RoleGroupHandler[*zkv1alpha1.ZookeeperCluster] = &ZkRoleGroupHandler{}

// LabelDomain is the product domain used for identity (selector) labels:
// zookeeper.kubedoop.dev/{cluster,role,role-group}. The product-domain prefix guarantees
// these selectors never match another product's pods.
const LabelDomain = "zookeeper.kubedoop.dev"

// serverRoleName is the single ZooKeeper role name, used both as the role key and as the
// component label value.
const serverRoleName = "server"

// bashShell is the shell used for exec probes and the container entrypoint script.
const bashShell = "bash"

// NewZkRoleGroupHandler creates a handler with the framework-level options that are constant
// across reconciliations. Per-CR options (image, ports) are set in BuildResources.
func NewZkRoleGroupHandler(scheme *runtime.Scheme) *ZkRoleGroupHandler {
	h := &ZkRoleGroupHandler{}
	h.Scheme = scheme
	h.ImagePullPolicy = corev1.PullIfNotPresent
	h.RoleImages = map[string]string{}
	h.RoleContainerPorts = map[string][]corev1.ContainerPort{}
	h.RoleServicePorts = map[string][]corev1.ServicePort{}
	// app.kubernetes.io/name identifies the product on every resource/pod. The framework's
	// canonical labels (instance + component=role + managed-by=operator-go) are not enough to
	// distinguish products in a shared namespace — component=server is generic and managed-by
	// is shared by all operator-go operators — so the cluster Service selector also keys on
	// this name (see ClusterServiceExtension) to avoid selecting another product's pods.
	h.ExtraLabels = map[string]string{
		"app.kubernetes.io/name": zkv1alpha1.DefaultProductName,
	}
	h.ExtraAnnotations = map[string]string{}
	// ZK peers must resolve each other before readiness, and data must be persistent.
	h.PublishNotReadyAddresses = true
	h.StorageMountPath = constant.KubedoopDataDir
	// Product-owned identity labels drive all resource selectors (decoupled from the
	// descriptive app.kubernetes.io/* labels).
	h.LabelDomain = LabelDomain
	return h
}

// BuildResources builds all Kubernetes resources for a Zookeeper server role group.
func (h *ZkRoleGroupHandler) BuildResources(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
) (*reconciler.RoleGroupResources, error) {
	if buildCtx.RoleName != serverRoleName {
		return nil, fmt.Errorf("unsupported role: %s", buildCtx.RoleName)
	}

	// Resolve Zookeeper-specific inputs.
	zkSecurity, err := security.NewZookeeperSecurity(ctx, k8sClient, cr.Spec.ClusterConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create zookeeper security: %w", err)
	}
	secretProvisioner := h.buildSecretProvisioner(zkSecurity)
	image := h.resolveImage(cr)
	replicas := buildCtx.RoleGroupSpec.GetReplicas()

	// Configure the per-CR base inputs.
	h.Image = image
	h.SetRoleContainerPorts(serverRoleName, h.containerPorts(zkSecurity))
	h.SetRoleServicePorts(serverRoleName, h.servicePorts(zkSecurity))
	// Ensure the data PVC is built even when the user omits resources.storage.
	h.ensureStorageDefault(buildCtx)

	// Register the containers that the SidecarManager will inject (myid init container +
	// product image on Vector). This must happen before base.BuildResources(), which runs
	// SidecarManager.InjectAll() internally.
	if err := h.registerServerContainers(buildCtx, image); err != nil {
		return nil, err
	}

	// Let the framework build the skeleton: canonical labels, headless Service (with
	// PublishNotReadyAddresses), client Service, StatefulSet (data PVC + injected
	// sidecars/init), and PodDisruptionBudget.
	res, err := h.BaseRoleGroupHandler.BuildResources(ctx, k8sClient, cr, buildCtx)
	if err != nil {
		return nil, fmt.Errorf("base build failed: %w", err)
	}

	// Customize the StatefulSet with Zookeeper specifics.
	if err := h.customizeStatefulSet(res.StatefulSet, buildCtx, zkSecurity, secretProvisioner); err != nil {
		return nil, err
	}

	// Replace the ConfigMap with computed Zookeeper config (zoo.cfg, security.properties,
	// logback.xml, vector.yaml). Reuse the framework labels base put on the StatefulSet.
	cm, err := h.buildConfigMap(ctx, k8sClient, cr, buildCtx, res.StatefulSet.Labels, zkSecurity, secretProvisioner, replicas)
	if err != nil {
		return nil, fmt.Errorf("failed to build configmap: %w", err)
	}
	res.ConfigMap = cm

	// The client Service is NodePort for the external-unstable listener class.
	if res.Service != nil && cr.Spec.ClusterConfig != nil &&
		cr.Spec.ClusterConfig.ListenerClass == zkv1alpha1.ExternalUnstable {
		res.Service.Spec.Type = corev1.ServiceTypeNodePort
	}

	// Metrics Service (headless with Prometheus scrape annotations). Its selector uses the
	// identity labels, consistent with the other role-group resources.
	res.MetricsService = builder.NewMetricsServiceBuilder(
		buildCtx.ResourceName,
		buildCtx.ClusterNamespace,
		zkv1alpha1.MetricsPort,
		res.StatefulSet.Labels,
	).WithSelector(h.SelectorLabels(buildCtx)).Build()

	return res, nil
}

// registerServerContainers registers the myid init container and configures the Vector
// sidecar on the SidecarManager so that base.BuildResources() injects them.
func (h *ZkRoleGroupHandler) registerServerContainers(buildCtx *reconciler.RoleGroupBuildContext, image string) error {
	mgr := buildCtx.SidecarManager // always non-nil (GenericReconciler guarantees it)

	// myid init container — a one-shot init (nil RestartPolicy), injected through the manager.
	mgr.Register(
		sidecar.NewStaticContainerProvider(h.buildPrepareContainer(image)),
		&sidecar.SidecarConfig{Enabled: true},
	)

	// Point the Vector sidecar at our ConfigMap. (GenericReconciler does not call
	// SetProductImage for embedding handlers, so do it here.)
	if provider, ok := mgr.GetProvider("vector"); ok {
		if vp, ok := provider.(*sidecar.VectorSidecarProvider); ok {
			vp.WithConfigMapName(buildCtx.ResourceName)
		}
	}
	if err := mgr.SetProductImage(image, corev1.PullIfNotPresent); err != nil {
		return fmt.Errorf("failed to set product image on sidecars: %w", err)
	}
	return nil
}

// buildSecretProvisioner creates a SecretProvisioner with all CSI secret volumes
// needed by the Zookeeper server based on the security configuration.
func (h *ZkRoleGroupHandler) buildSecretProvisioner(zkSecurity *security.ZookeeperSecurity) *opgosecurity.SecretProvisioner {
	provisioner := opgosecurity.NewSecretProvisioner()

	// Server TLS: always register when TLS is enabled.
	if zkSecurity.TLSEnabled() {
		serverClass := zkSecurity.ServerSecretClass()
		if serverClass == "" {
			serverClass = security.TlsDefaultSecretClass
			log.Log.Info("TLS enabled without serverSecretClass; falling back to default secret class",
				"serverSecretClass", serverClass)
		}
		provisioner.Register(opgosecurity.TLS(
			security.ServerTlsVolumeName,
			serverClass,
		).WithPassword(zkSecurity.SSLStorePassword()))
	}

	// Client TLS: needed if auth TLS class provides a client cert secret class.
	if clientSecretClass := zkSecurity.ClientTLSSecretClass(); clientSecretClass != "" {
		provisioner.Register(opgosecurity.TLS(
			security.ClientTlsVolumeName,
			clientSecretClass,
		).WithPassword(zkSecurity.SSLStorePassword()))
	}

	// Quorum TLS: needed if quorumSecretClass is set.
	if quorumClass := zkSecurity.QuorumSecretClass(); quorumClass != "" {
		provisioner.Register(opgosecurity.TLS(
			security.QuorumTlsVolumeName,
			quorumClass,
		).WithPassword(zkSecurity.SSLStorePassword()))
	}

	return provisioner
}

// resolveImage constructs the container image string from the CR spec.
func (h *ZkRoleGroupHandler) resolveImage(cr *zkv1alpha1.ZookeeperCluster) string {
	if cr.Spec.Image == nil {
		return fmt.Sprintf("%s/%s:%s", zkv1alpha1.DefaultRepository,
			zkv1alpha1.DefaultProductName, zkv1alpha1.DefaultProductVersion)
	}
	img := cr.Spec.Image
	if img.Custom != "" {
		return img.Custom
	}
	repo := img.Repo
	if repo == "" {
		repo = zkv1alpha1.DefaultRepository
	}
	productVersion := img.ProductVersion
	if productVersion == "" {
		productVersion = zkv1alpha1.DefaultProductVersion
	}
	if img.KubedoopVersion != "" {
		return fmt.Sprintf("%s/%s:%s-kubedoop%s",
			repo, zkv1alpha1.DefaultProductName, productVersion, img.KubedoopVersion)
	}
	return fmt.Sprintf("%s/%s:%s", repo, zkv1alpha1.DefaultProductName, productVersion)
}
