package controller

import (
	"fmt"
	"path"
	"strings"

	"github.com/zncdatadev/zookeeper-operator/internal/util"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	opgoconstant "github.com/zncdatadev/operator-go/pkg/constant"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// ZooKeeper role group defaults. The base-operator-go framework applies resources, affinity and
// gracefulShutdownTimeout only when the merged role group config already carries them, so
// ZooKeeper supplies its own defaults here (matching the pre-framework behavior). Storage is only
// ensured non-nil so a minimal cluster still gets a data PVC; the framework's GetCapacity() then
// applies the 10Gi default capacity.
const (
	defaultCPUMin           = "100m"
	defaultCPUMax           = "200m"
	defaultMemoryLimit      = "512Mi"
	defaultGracefulShutdown = "120s"
	// antiAffinityWeight biases (does not force) the scheduler to spread ensemble members across
	// nodes, so a single node failure cannot take down the quorum.
	antiAffinityWeight = 70
)

// ensureServerConfigDefaults fills in the ZooKeeper role group defaults the framework does not
// supply on its own: a data PVC, CPU/memory requests+limits, a preferred pod anti-affinity that
// spreads ensemble members across nodes, and a 120s graceful-shutdown window.
//
// It only ever fills fields that are still unset. buildCtx.RoleGroupSpec.Config is already the
// result of the framework folding the role's config beneath the role group's
// (GenericReconciler: MergeRoleGroupConfig(roleSpec.Config, groupSpec.Config)), so anything the
// user stated at either level is present here and is left alone.
func (h *ZkRoleGroupHandler) ensureServerConfigDefaults(buildCtx *reconciler.RoleGroupBuildContext) error {
	if buildCtx.RoleGroupSpec.Config == nil {
		buildCtx.RoleGroupSpec.Config = &commonsv1alpha1.RoleGroupConfigSpec{}
	}
	cfg := buildCtx.RoleGroupSpec.Config

	if cfg.Resources == nil {
		cfg.Resources = &commonsv1alpha1.ResourcesSpec{}
	}

	// Storage: ensure a data PVC exists. The framework builds the VolumeClaimTemplate only when
	// Resources.Storage is non-nil, so default it to an empty StorageResource. Its capacity is left
	// to the framework, whose StorageResource.GetCapacity() applies DefaultStorageCapacity (10Gi) —
	// the value ZooKeeper used — when unset.
	if cfg.Resources.Storage == nil {
		cfg.Resources.Storage = &commonsv1alpha1.StorageResource{}
	}

	if cfg.Resources.CPU == nil {
		cfg.Resources.CPU = &commonsv1alpha1.CPUResource{
			Min: ptr.To(resource.MustParse(defaultCPUMin)),
			Max: ptr.To(resource.MustParse(defaultCPUMax)),
		}
	}

	// Memory also drives ZK_SERVER_HEAP in getEnvVars.
	if cfg.Resources.Memory == nil {
		cfg.Resources.Memory = &commonsv1alpha1.MemoryResource{Limit: ptr.To(resource.MustParse(defaultMemoryLimit))}
	}

	// A preferred (not required) anti-affinity, so a single node failure cannot take the quorum
	// down while a cluster larger than the node count still schedules.
	if cfg.Affinity == nil {
		affinity, err := reconciler.EncodeAffinity(reconciler.DefaultAntiAffinity(
			buildCtx.ClusterName, buildCtx.RoleName, reconciler.TopologyKeyHostname, antiAffinityWeight))
		if err != nil {
			return fmt.Errorf("failed to encode the default anti-affinity: %w", err)
		}
		cfg.Affinity = affinity
	}

	// gracefulShutdownTimeout is a *string, so "unset" is a nil pointer: the CRD does not default
	// it, and an explicit user value (including the platform's own "30s") is honored as written.
	if cfg.GracefulShutdownTimeout == nil {
		cfg.GracefulShutdownTimeout = ptr.To(defaultGracefulShutdown)
	}
	return nil
}

// customizeMainContainer applies Zookeeper specifics to the primary container the framework
// assembles: the start command, exec probes, env and heap sizing. It is registered as
// buildCtx.MainContainerCustomizer and runs during the build. Pod identity (ServiceAccount), the
// default pod/container SecurityContext, the config ConfigMap mount, the data PVC, the shared
// Vector log volume (on the renamed "zookeeper" container), the CSI secret (TLS) volumes
// (registered via buildCtx.VolumeProviders), the image, ports, resources and injected
// sidecars/init containers are already in place from the framework builder.
func (h *ZkRoleGroupHandler) customizeMainContainer(
	main *corev1.Container,
	buildCtx *reconciler.RoleGroupBuildContext,
	zkSecurity *security.ZookeeperSecurity,
) error {
	roleGroupConfig := buildCtx.RoleGroupSpec.GetConfig()

	// The framework hands us the assembled primary container: already named "zookeeper"
	// (BaseRoleGroupHandler.MainContainerName), already carrying the framework-managed
	// config/data/log mounts plus the registered CSI secret (TLS) volume mounts and the resolved
	// image, so we only set the command, env and probes here.
	main.Command = []string{"/bin/bash", "-x", "-euo", "pipefail", "-c"}
	main.Args = h.getMainContainerArgs()
	// User envOverrides (already on the container from the builder) win over our defaults.
	main.Env = append(h.getEnvVars(roleGroupConfig), main.Env...)
	main.ReadinessProbe = h.getReadinessProbe(zkSecurity)
	main.LivenessProbe = h.getLivenessProbe(zkSecurity)
	main.StartupProbe = h.getStartupProbe(zkSecurity)
	return nil
}

// buildPrepareContainer builds the myid init container. It is one-shot (nil RestartPolicy)
// and registered through the SidecarManager (see registerServerContainers).
func (h *ZkRoleGroupHandler) buildPrepareContainer(image string, minServerID int32) corev1.Container {
	return corev1.Container{
		Name:            "prepare",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/bash", "-x", "-euo", "pipefail", "-c"},
		Args: []string{
			"expr $MYID_OFFSET + $(echo $POD_NAME | sed 's/.*-//') > /kubedoop/data/myid",
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: zkv1alpha1.DataDirName, MountPath: constant.KubedoopDataDir},
		},
		Env: []corev1.EnvVar{
			// Must equal resolveMinServerID (which keys the zoo.cfg server.N entries) so each pod's
			// myid file — MYID_OFFSET + pod ordinal — matches the server.N id the config expects.
			{Name: "MYID_OFFSET", Value: fmt.Sprintf("%d", minServerID)},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
		},
	}
}

// getMainContainerArgs returns the command arguments for the main container.
func (h *ZkRoleGroupHandler) getMainContainerArgs() []string {
	zkConfigPath := path.Join(constant.KubedoopConfigDir, "zoo.cfg")

	args := []string{
		// The framework mounts the config ConfigMap read-only at KubedoopConfigDirMount; copy it
		// into the writable config dir (logback.xml and zoo.cfg included — all in that ConfigMap).
		fmt.Sprintf(`CONFIG_DIR_MOUNT=%s
CONFIG_DIR=%s
mkdir --parents ${CONFIG_DIR}
echo copying ${CONFIG_DIR_MOUNT} to ${CONFIG_DIR}
cp -RL ${CONFIG_DIR_MOUNT}* ${CONFIG_DIR}`, opgoconstant.KubedoopConfigDirMount, constant.KubedoopConfigDir),
		`echo "Starting Zookeeper"`,
		// exec so the JVM replaces this shell and becomes the container's main process,
		// receiving SIGTERM directly for graceful shutdown on pod termination. Vector
		// shutdown ordering is handled by the framework's native sidecar (init container
		// with restartPolicy: Always), so the old background+trap+wait dance is unnecessary.
		fmt.Sprintf("exec bin/zkServer.sh start-foreground %s", zkConfigPath),
	}
	return []string{strings.Join(args, "\n")}
}

// getEnvVars returns environment variables for the main container.
func (h *ZkRoleGroupHandler) getEnvVars(
	roleGroupConfig *commonsv1alpha1.RoleGroupConfigSpec,
) []corev1.EnvVar {
	// The myid file is written by the prepare init container (buildPrepareContainer); the main
	// container never reads MYID_OFFSET, so it is intentionally not set here.
	envs := []corev1.EnvVar{
		{Name: "SERVER_JVMFLAGS", Value: util.JvmJmxOpts(zkv1alpha1.MetricsPort)},
	}

	// Heap limit from memory resources.
	if roleGroupConfig != nil && roleGroupConfig.Resources != nil && roleGroupConfig.Resources.Memory != nil {
		memoryLimit := roleGroupConfig.Resources.Memory.Limit
		heapLimit := float64(memoryLimit.Value()/(1024*1024)) * 0.8
		if heapLimit > 0 {
			envs = append(envs, corev1.EnvVar{
				Name:  "ZK_SERVER_HEAP",
				Value: fmt.Sprintf("%.0f", heapLimit),
			})
		}
	}

	return envs
}

// containerPorts returns the main container ports.
func (h *ZkRoleGroupHandler) containerPorts(zkSecurity *security.ZookeeperSecurity) []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{Name: zkv1alpha1.ClientPortName, ContainerPort: int32(zkSecurity.ClientPort())},
		{Name: zkv1alpha1.LeaderPortName, ContainerPort: zkv1alpha1.LeaderPort},
		{Name: zkv1alpha1.ElectionPortName, ContainerPort: zkv1alpha1.ElectionPort},
		{Name: zkv1alpha1.MetricsPortName, ContainerPort: zkv1alpha1.MetricsPort},
	}
}

// servicePorts returns the ports exposed by the headless and client services.
func (h *ZkRoleGroupHandler) servicePorts(zkSecurity *security.ZookeeperSecurity) []corev1.ServicePort {
	clientPort := int32(zkSecurity.ClientPort())
	return []corev1.ServicePort{
		{Name: zkv1alpha1.ClientPortName, Port: clientPort, TargetPort: intstr.FromInt(int(clientPort))},
		{Name: zkv1alpha1.MetricsPortName, Port: zkv1alpha1.MetricsPort, TargetPort: intstr.FromInt(int(zkv1alpha1.MetricsPort))},
	}
}

// getLivenessProbe returns the liveness probe for Zookeeper.
func (h *ZkRoleGroupHandler) getLivenessProbe(zkSecurity *security.ZookeeperSecurity) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					bashShell,
					"-c",
					fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo ruok >&3 && grep 'imok' <&3", zkSecurity.ClientPort()),
				},
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
		FailureThreshold:    3,
		SuccessThreshold:    1,
		TimeoutSeconds:      5,
	}
}

// getStartupProbe returns the startup probe for Zookeeper. ZooKeeper does not bind the client
// port until ~25s into JVM startup, which is slower than the liveness probe's ~30s budget under a
// constrained CPU limit — without a startup probe a slow first start trips liveness and the
// container is killed (exit 143) into a CrashLoop before it ever serves. The startup probe runs
// the same ruok check but with a generous budget (30 * 10s = 5m) and suspends both the liveness
// and readiness probes until the server first answers, so only genuinely stuck starts fail.
func (h *ZkRoleGroupHandler) getStartupProbe(zkSecurity *security.ZookeeperSecurity) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					bashShell,
					"-c",
					fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo ruok >&3 && grep 'imok' <&3", zkSecurity.ClientPort()),
				},
			},
		},
		PeriodSeconds:    10,
		FailureThreshold: 30,
		SuccessThreshold: 1,
		TimeoutSeconds:   5,
	}
}

// getReadinessProbe returns the readiness probe for Zookeeper.
//
// The one-second period and timeout this probe used to carry are not survivable for an exec probe
// on a CPU-limited JVM container: the check forks bash, opens a TCP connection and greps, and under
// the default 200m limit the container is CFS-throttled in the large majority of scheduling periods,
// so the command's tail latency runs past a second even though ZooKeeper is serving normally. Three
// such samples in a row — three seconds — dropped a healthy server out of the Service endpoints and
// the StatefulSet never reported all replicas available. A five-second budget covers the throttled
// tail; keeping FailureThreshold at 3 still removes a genuinely unresponsive server within 15s.
func (h *ZkRoleGroupHandler) getReadinessProbe(zkSecurity *security.ZookeeperSecurity) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					bashShell,
					"-c",
					fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo srvr >&3 && grep '^Mode: ' <&3", zkSecurity.ClientPort()),
				},
			},
		},
		FailureThreshold: 3,
		PeriodSeconds:    5,
		SuccessThreshold: 1,
		TimeoutSeconds:   5,
	}
}
