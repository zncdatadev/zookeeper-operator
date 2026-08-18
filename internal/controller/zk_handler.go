package controller

import (
	"context"
	"fmt"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	"github.com/zncdatadev/operator-go/pkg/sidecar"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
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
	*reconciler.BaseRoleGroupHandler[*zkv1alpha1.ZookeeperCluster]
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
	base := reconciler.NewBaseRoleGroupHandler[*zkv1alpha1.ZookeeperCluster](scheme)
	// Product-owned identity labels drive all resource selectors (decoupled from the
	// descriptive app.kubernetes.io/* labels). This is reconcile-invariant, so it stays on the
	// handler; everything a ROLE is made of is declared per pass in DeclareRoles.
	base.LabelDomain = LabelDomain
	return &ZkRoleGroupHandler{BaseRoleGroupHandler: base}
}

// DeclareRoles implements reconciler.RoleProvider: everything the server role is made of, produced
// once per reconcile pass with the CR in hand.
//
// Taking the CR is what lets this be static data. The client port moves when the CR enables TLS and
// the anti-affinity selector names the cluster, and both are computed here from THIS cluster rather
// than assigned into handler state that the next cluster would inherit.
func (h *ZkRoleGroupHandler) DeclareRoles(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
) (reconciler.RoleCatalog, error) {
	// The security resolution reads the referenced SecretClasses, which is why this hook takes a
	// client: the ports below depend on whether the cluster speaks TLS.
	zkSecurity, err := security.NewZookeeperSecurity(ctx, k8sClient, cr.Spec.ClusterConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create zookeeper security: %w", err)
	}

	configDefaults, err := serverConfigDefaults(cr.Name)
	if err != nil {
		return nil, err
	}

	return reconciler.RoleCatalog{
		serverRoleName: {
			// Name the primary container "zookeeper" (backward compat with the pre-framework
			// layout); it is also the logging producer, so the shared Vector log volume is mounted
			// on that name.
			MainContainerName: common.ZkServerContainerName,
			ContainerPorts:    h.containerPorts(zkSecurity),
			ServicePorts:      h.servicePorts(zkSecurity),
			// The entrypoint carries the script inline: arguments are deliberately not a
			// declaration field, because cliOverrides is the user's channel for them and a
			// product appending args would silently delete what the user wrote.
			Command:                  h.mainContainerCommand(),
			ReadinessProbe:           h.getReadinessProbe(zkSecurity),
			LivenessProbe:            h.getLivenessProbe(zkSecurity),
			StartupProbe:             h.getStartupProbe(zkSecurity),
			DataVolume:               &reconciler.DataVolume{Name: zkv1alpha1.DataDirName, MountPath: constant.KubedoopDataDir},
			PublishNotReadyAddresses: true, // ZK peers must resolve each other before readiness.
			LogProducers:             []productlogging.ContainerLogging{zkServerLogging},
			// Keep the pre-framework log volume size (the framework default is larger).
			LogVolumeSize:  zkv1alpha1.MaxZKLogFileSize,
			ConfigDefaults: configDefaults,
			Env:            h.staticEnvVars(),
		},
	}, nil
}

// ResolveRoleGroup implements reconciler.RoleGroupResolver: the values that follow from a role
// group's EFFECTIVE config, which only exists after the framework has folded the product's defaults
// under the CR's role and role group levels.
//
// The JVM heap is exactly that kind of value — it is a function of the memory limit that survived
// the fold, so it cannot be declared alongside the defaults that feed it.
func (h *ZkRoleGroupHandler) ResolveRoleGroup(
	_ context.Context,
	_ client.Client,
	_ *zkv1alpha1.ZookeeperCluster,
	rg *reconciler.RoleGroupBuildContext,
) (*reconciler.Contribution, error) {
	heap := serverHeapEnv(rg.RoleGroupSpec.GetConfig())
	if heap == "" {
		return nil, nil
	}
	return &reconciler.Contribution{EnvVars: map[string]string{zkServerHeapEnvName: heap}}, nil
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

	// Register the containers that the SidecarManager will inject. This must happen before
	// base.BuildResources(), which runs SidecarManager.InjectAll() internally.
	// The prepare init container writes each pod's myid as (base + ordinal); the base is this
	// role group's non-overlapping myid start, so myids stay unique across the whole ensemble. It
	// runs the product image, taken from the reference the framework already resolved for the main
	// container so the two cannot drift.
	h.registerServerContainers(buildCtx, buildCtx.ResolvedImage.Reference,
		serverGroupBaseIDs(cr)[buildCtx.RoleGroupName])

	// Hand the CSI secret (TLS) volumes to the framework so base.BuildResources() injects them
	// into the pod and the main container, instead of appending them by hand afterwards.
	// VolumeProviders lives on the build context (rebuilt each reconcile), so registrations never
	// accumulate across reconciles or leak across CRs.
	buildCtx.VolumeProviders = append(buildCtx.VolumeProviders, secretProvisioner)

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
