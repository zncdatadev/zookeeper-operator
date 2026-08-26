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

// serverConfigDefaults is the ZooKeeper server role's own config defaults: a data PVC, CPU/memory
// requests+limits, a preferred pod anti-affinity that spreads ensemble members across nodes, and a
// 120s graceful-shutdown window.
//
// It is DECLARED, not applied. The framework folds it beneath the CR's role and role group levels
// through the same rules those two use, so `resources` folds per leaf (a default cpu.min survives a
// user who set only cpu.max) and anything the user states anywhere still wins.
func serverConfigDefaults(clusterName string) (*commonsv1alpha1.RoleGroupConfigSpec, error) {
	// A preferred (not required) anti-affinity, so a single node failure cannot take the quorum
	// down while a cluster larger than the node count still schedules.
	affinity, err := reconciler.EncodeAffinity(&corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				reconciler.PreferredAffinityTerm(antiAffinityWeight, reconciler.TopologyKeyHostname,
					reconciler.RoleSelectorLabels(clusterName, serverRoleName)),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode the default anti-affinity: %w", err)
	}

	return &commonsv1alpha1.RoleGroupConfigSpec{
		Resources: &commonsv1alpha1.ResourcesSpec{
			// Storage only has to be non-nil for the framework to build the data PVC; its capacity
			// is left to StorageResource.GetCapacity(), which applies the 10Gi default.
			Storage: &commonsv1alpha1.StorageResource{},
			CPU: &commonsv1alpha1.CPUResource{
				Min: ptr.To(resource.MustParse(defaultCPUMin)),
				Max: ptr.To(resource.MustParse(defaultCPUMax)),
			},
			// Memory also drives ZK_SERVER_HEAP, derived from the FOLDED value in ResolveRoleGroup.
			Memory: &commonsv1alpha1.MemoryResource{Limit: ptr.To(resource.MustParse(defaultMemoryLimit))},
		},
		Affinity:                affinity,
		GracefulShutdownTimeout: ptr.To(defaultGracefulShutdown),
	}, nil
}

// mainContainerCommand is the primary container's entrypoint. The script is carried inline as the
// final `-c` argument rather than as container args, because args are the user's channel
// (cliOverrides) and a product writing them would silently delete what the user wrote.
func (h *ZkRoleGroupHandler) mainContainerCommand() []string {
	return append([]string{"/bin/bash", "-x", "-euo", "pipefail", "-c"}, h.getMainContainerArgs()...)
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

// zkServerHeapEnvName is the env var ZooKeeper's start script reads for the JVM heap, in MiB.
const zkServerHeapEnvName = "ZK_SERVER_HEAP"

// staticEnvVars returns the env the role can DECLARE — the values that follow from the product
// alone, with no dependency on the role group's resolved config.
//
// The myid file is written by the prepare init container (buildPrepareContainer); the main
// container never reads MYID_OFFSET, so it is intentionally not set here.
func (h *ZkRoleGroupHandler) staticEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "SERVER_JVMFLAGS", Value: util.JvmJmxOpts(zkv1alpha1.MetricsPort)},
	}
}

// serverHeapEnv derives the JVM heap (MiB) from a role group's EFFECTIVE memory limit — 80% of it,
// leaving the remainder for the JVM's own non-heap footprint. It returns "" when no limit survived
// the fold, which leaves ZooKeeper on its built-in default rather than pinning a guessed number.
//
// This cannot be declared beside the memory default it reads: the value only exists after the
// framework has folded that default under the CR's two levels, which is why it is contributed from
// ResolveRoleGroup instead.
func serverHeapEnv(roleGroupConfig *commonsv1alpha1.RoleGroupConfigSpec) string {
	if roleGroupConfig == nil || roleGroupConfig.Resources == nil || roleGroupConfig.Resources.Memory == nil {
		return ""
	}
	memoryLimit := roleGroupConfig.Resources.Memory.Limit
	if memoryLimit == nil {
		return ""
	}
	heapLimit := float64(memoryLimit.Value()/(1024*1024)) * 0.8
	if heapLimit <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f", heapLimit)
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
