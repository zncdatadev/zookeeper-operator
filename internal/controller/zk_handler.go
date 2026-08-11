package controller

import (
	"context"
	"fmt"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	"github.com/zncdatadev/operator-go/pkg/sidecar"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util/version"
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

// zkServerLogging is the single source of truth for the ZooKeeper main container's logging.
// It drives both BaseRoleGroupHandler.LoggingContainers (the framework's shared Vector log
// volume producer/consumer wiring) and the logback.xml + vector.yaml rendered into the role
// group ConfigMap. Only the ZooKeeper-specific bits live here: the encoder pattern (myid MDC).
// The framework derives the per-container log file the Vector sidecar collects
// (/kubedoop/log/<container>/<container>.log4j.xml).
var zkServerLogging = productlogging.ContainerLogging{
	Container: common.ZkServerContainerName,
	Framework: productlogging.LoggingFrameworkLogback,
	Pattern:   "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n",
}

// NewZkRoleGroupHandler creates a handler with the framework-level options that are constant
// across reconciliations. Per-CR options (image, ports) are set in BuildResources.
func NewZkRoleGroupHandler(scheme *runtime.Scheme) *ZkRoleGroupHandler {
	h := &ZkRoleGroupHandler{}
	h.Scheme = scheme
	h.ImagePullPolicy = corev1.PullIfNotPresent
	h.RoleImages = map[string]string{}
	// ProductName names the product: it supplies the repository path segment of the resolved image
	// and the app.kubernetes.io/{name,version} labels. ImageDefaults fills in whatever spec.image
	// leaves empty, re-evaluated every reconcile — which is why KubedoopVersion can be the
	// operator's own build version, so an operator upgrade moves existing clusters onto the
	// co-released product image. Kubedoop publishes ZooKeeper images only with the
	// "-kubedoop<version>" suffix, so that field must always resolve to something.
	h.ProductName = zkv1alpha1.DefaultProductName
	h.ImageDefaults = commonsv1alpha1.ImageSpec{
		Repo:            zkv1alpha1.DefaultRepository,
		ProductVersion:  zkv1alpha1.DefaultProductVersion,
		KubedoopVersion: version.BuildVersion,
	}
	// ZK peers must resolve each other before readiness, and data must be persistent.
	h.PublishNotReadyAddresses = true
	h.StorageMountPath = constant.KubedoopDataDir
	// Rename the primary container to "zookeeper" (backward compat with the pre-framework
	// layout) and declare it as the logging container. The framework renames the container
	// before injecting the shared Vector log volume, so the producer mounts it on "zookeeper".
	h.MainContainerName = common.ZkServerContainerName
	h.LoggingContainers = []productlogging.ContainerLogging{zkServerLogging}
	// Keep the pre-framework log volume size (the framework default is larger).
	h.LogVolumeSize = zkv1alpha1.MaxZKLogFileSize
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
	// Resolve the image from the same input and through the same function the framework uses for
	// the main container, because the prepare init container below runs the product image too and
	// must not drift from it. An unresolvable spec.image is reported rather than silently replaced
	// with some other version.
	var imageSpec *commonsv1alpha1.ImageSpec
	if buildCtx.ClusterSpec != nil {
		imageSpec = buildCtx.ClusterSpec.Image
	}
	image, err := imageSpec.ResolveImage(h.ProductName, h.ImageDefaults)
	if err != nil {
		return nil, err
	}

	// Per-CR inputs go on the build context, not the handler: one handler instance serves every
	// ZookeeperCluster, so writing them to its fields would let concurrent reconciles of different
	// clusters overwrite each other.
	buildCtx.ContainerPorts = h.containerPorts(zkSecurity)
	buildCtx.ServicePorts = h.servicePorts(zkSecurity)
	// Fill in ZooKeeper role group defaults (storage, CPU/memory, anti-affinity, graceful
	// shutdown) the framework does not supply, before base.BuildResources consumes the config.
	if err := h.ensureServerConfigDefaults(buildCtx); err != nil {
		return nil, err
	}

	// Register the containers that the SidecarManager will inject (myid init container +
	// product image on Vector). This must happen before base.BuildResources(), which runs
	// SidecarManager.InjectAll() internally.
	// The prepare init container writes each pod's myid as (base + ordinal); the base is this
	// role group's non-overlapping myid start, so myids stay unique across the whole ensemble.
	h.registerServerContainers(buildCtx, image, serverGroupBaseIDs(cr)[buildCtx.RoleGroupName])

	// Hand the CSI secret (TLS) volumes to the framework so base.BuildResources() injects them
	// into the pod and the main container, instead of appending them by hand afterwards.
	// VolumeProviders lives on the build context (rebuilt each reconcile), so registrations never
	// accumulate across reconciles or leak across CRs.
	buildCtx.VolumeProviders = append(buildCtx.VolumeProviders, secretProvisioner)

	// Declare the ZooKeeper specifics of the primary container through the framework's hook rather
	// than editing the built StatefulSet: the customizer is handed the assembled container by name,
	// where the old post-build code indexed Containers[0] — a position the framework never promised
	// and that any sidecar provider inserting a container earlier would silently break. It runs
	// after the framework has applied envOverrides (so appending them after ours keeps the user's
	// last word) and before podOverrides are strategic-merged (so those still outrank us).
	buildCtx.MainContainerCustomizer = func(c *corev1.Container) error {
		return h.customizeMainContainer(c, buildCtx, zkSecurity)
	}

	// Let the framework build the skeleton: canonical labels, headless Service (with
	// PublishNotReadyAddresses), client Service, StatefulSet (data PVC + injected
	// sidecars/init), and PodDisruptionBudget.
	res, err := h.BaseRoleGroupHandler.BuildResources(ctx, k8sClient, cr, buildCtx)
	if err != nil {
		return nil, fmt.Errorf("base build failed: %w", err)
	}

	// Replace the ConfigMap with computed Zookeeper config (zoo.cfg, security.properties,
	// logback.xml, vector.yaml). Reuse the framework labels base put on the StatefulSet.
	cm, err := h.buildConfigMap(cr, buildCtx, res.StatefulSet.Labels, zkSecurity, secretProvisioner)
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
	).WithSelector(h.SelectorLabels(buildCtx)).
		// Target the container port by name so the Service stays valid regardless of the numeric
		// port and matches the metrics port the pods actually expose.
		WithTargetPortName(zkv1alpha1.MetricsPortName).
		Build()

	return res, nil
}

// registerServerContainers registers the myid init container on the SidecarManager so that
// base.BuildResources() injects it. The product image is propagated to the framework-constructed
// Vector sidecar by base.BuildResources() itself (operator-go #536), so no manual SetProductImage
// call is needed here.
func (h *ZkRoleGroupHandler) registerServerContainers(buildCtx *reconciler.RoleGroupBuildContext, image string, minServerID int32) {
	mgr := buildCtx.SidecarManager // always non-nil (GenericReconciler guarantees it)

	// myid init container — a one-shot init (nil RestartPolicy), injected through the manager.
	mgr.Register(
		sidecar.NewStaticContainerProvider(h.buildPrepareContainer(image, minServerID)),
		&sidecar.SidecarConfig{Enabled: true},
	)
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
