package controller

import (
	"context"
	"fmt"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ZkRoleGroupHandler implements reconciler.RoleGroupHandler for Zookeeper clusters.
// It builds all Kubernetes resources (ConfigMap, Services, StatefulSet, PDB) for each server role group.
type ZkRoleGroupHandler struct{}

// Verify interface compliance
var _ reconciler.RoleGroupHandler[*zkv1alpha1.ZookeeperCluster] = &ZkRoleGroupHandler{}

// BuildResources builds all Kubernetes resources for a Zookeeper server role group.
func (h *ZkRoleGroupHandler) BuildResources(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
) (*reconciler.RoleGroupResources, error) {
	if buildCtx.RoleName != "server" {
		return nil, fmt.Errorf("unsupported role: %s", buildCtx.RoleName)
	}
	return h.buildServerResources(ctx, k8sClient, cr, buildCtx)
}

// buildServerResources creates all resources for the server role group.
func (h *ZkRoleGroupHandler) buildServerResources(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
) (*reconciler.RoleGroupResources, error) {
	// Resolve security configuration (TLS, authentication)
	zkSecurity, err := security.NewZookeeperSecurity(ctx, k8sClient, cr.Spec.ClusterConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create zookeeper security: %w", err)
	}

	// Build SecretProvisioner and register CSI secret volumes
	secretProvisioner := h.buildSecretProvisioner(zkSecurity)

	// Resolve container image
	image := h.resolveImage(cr)
	replicas := buildCtx.RoleGroupSpec.GetReplicas()
	labels := h.buildLabels(buildCtx)

	// Build ConfigMap (zoo.cfg, security.properties, logback.xml)
	configMap, err := h.buildConfigMap(ctx, k8sClient, cr, buildCtx, labels, zkSecurity, secretProvisioner, replicas)
	if err != nil {
		return nil, fmt.Errorf("failed to build configmap: %w", err)
	}

	// Build Headless Service (for StatefulSet network identity)
	headlessSvc := h.buildHeadlessService(buildCtx, labels, zkSecurity)

	// Build Client Service (ClusterIP or NodePort based on listener class)
	isNodePort := cr.Spec.ClusterConfig != nil &&
		cr.Spec.ClusterConfig.ListenerClass == zkv1alpha1.ExternalUnstable
	clientSvc := h.buildClientService(buildCtx, labels, zkSecurity, bool(isNodePort))

	// Build StatefulSet
	sts, err := h.buildStatefulSet(ctx, k8sClient, cr, buildCtx, labels, zkSecurity, secretProvisioner, image, replicas)
	if err != nil {
		return nil, fmt.Errorf("failed to build statefulset: %w", err)
	}

	// Build PodDisruptionBudget
	pdb := h.buildPDB(buildCtx, labels)

	// Build Metrics Service (headless with Prometheus annotations)
	metricsSvc := builder.NewMetricsServiceBuilder(
		buildCtx.ResourceName,
		buildCtx.ClusterNamespace,
		zkv1alpha1.MetricsPort,
		labels,
	).Build()

	return &reconciler.RoleGroupResources{
		ConfigMap:           configMap,
		HeadlessService:     headlessSvc,
		Service:             clientSvc,
		StatefulSet:         sts,
		PodDisruptionBudget: pdb,
		MetricsService:      metricsSvc,
	}, nil
}

// buildSecretProvisioner creates a SecretProvisioner with all CSI secret volumes
// needed by the Zookeeper server based on the security configuration.
func (h *ZkRoleGroupHandler) buildSecretProvisioner(zkSecurity *security.ZookeeperSecurity) *opgosecurity.SecretProvisioner {
	provisioner := opgosecurity.NewSecretProvisioner()

	// Server TLS: always register when TLS is enabled.
	// If serverSecretClass is not explicitly set, fall back to the default secret class.
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

	// Client TLS: needed if auth TLS class provides a client cert secret class
	if clientSecretClass := zkSecurity.ClientTLSSecretClass(); clientSecretClass != "" {
		provisioner.Register(opgosecurity.TLS(
			security.ClientTlsVolumeName,
			clientSecretClass,
		).WithPassword(zkSecurity.SSLStorePassword()))
	}

	// Quorum TLS: needed if quorumSecretClass is set
	if quorumClass := zkSecurity.QuorumSecretClass(); quorumClass != "" {
		provisioner.Register(opgosecurity.TLS(
			security.QuorumTlsVolumeName,
			quorumClass,
		).WithPassword(zkSecurity.SSLStorePassword()))
	}

	return provisioner
}

// buildPDB creates a PodDisruptionBudget for the role group.
func (h *ZkRoleGroupHandler) buildPDB(
	buildCtx *reconciler.RoleGroupBuildContext,
	labels map[string]string,
) *policyv1.PodDisruptionBudget {
	pdbBuilder := builder.NewPDBBuilder(buildCtx.ResourceName, buildCtx.ClusterNamespace).
		WithLabels(labels).
		WithSelector(labels)

	// Apply PDB spec from role config if available (PDB is a role-level setting)
	if buildCtx.RoleSpec != nil && buildCtx.RoleSpec.RoleConfig != nil &&
		buildCtx.RoleSpec.RoleConfig.PodDisruptionBudget != nil {
		pdbBuilder.WithSpec(buildCtx.RoleSpec.RoleConfig.PodDisruptionBudget)
	}

	if !pdbBuilder.IsEnabled() {
		return nil
	}

	return pdbBuilder.Build()
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

// buildLabels creates labels for the role group resources.
func (h *ZkRoleGroupHandler) buildLabels(buildCtx *reconciler.RoleGroupBuildContext) map[string]string {
	labels := make(map[string]string)
	for k, v := range buildCtx.ClusterLabels {
		labels[k] = v
	}
	labels["app.kubernetes.io/component"] = buildCtx.RoleGroupName
	labels["zookeeper.kubedoop.dev/role"] = buildCtx.RoleName
	return labels
}
