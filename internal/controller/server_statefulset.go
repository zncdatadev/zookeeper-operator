package controller

import (
	"fmt"
	"path"
	"strings"

	"github.com/zncdatadev/zookeeper-operator/internal/util"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	"github.com/zncdatadev/operator-go/pkg/sidecar"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// defaultStorageCapacity is the fallback data PVC size when resources.storage is not
// specified. It mirrors the CRD default (StorageResource.Capacity) so a minimal
// ZookeeperCluster (no resources block) still gets persistent storage.
const defaultStorageCapacity = "10Gi"

// ensureStorageDefault makes sure the merged role group config carries a storage spec, so
// the framework's StatefulSetBuilder.WithStorage builds the data PVC even when the user
// omits resources.storage. Without this the "data" volume mount has no backing PVC and the
// StatefulSet is rejected.
func (h *ZkRoleGroupHandler) ensureStorageDefault(buildCtx *reconciler.RoleGroupBuildContext) {
	cfg := buildCtx.RoleGroupSpec.Config
	if cfg == nil {
		cfg = &commonsv1alpha1.RoleGroupConfigSpec{}
		buildCtx.RoleGroupSpec.Config = cfg
	}
	if cfg.Resources == nil {
		cfg.Resources = &commonsv1alpha1.ResourcesSpec{}
	}
	switch {
	case cfg.Resources.Storage == nil:
		cfg.Resources.Storage = &commonsv1alpha1.StorageResource{Capacity: resource.MustParse(defaultStorageCapacity)}
	case cfg.Resources.Storage.Capacity.IsZero():
		cfg.Resources.Storage.Capacity = resource.MustParse(defaultStorageCapacity)
	}
}

// customizeStatefulSet applies Zookeeper specifics to the StatefulSet built by the base
// handler: pod identity/security, the start command and exec probes, the config/log
// volumes, and the TLS CSI volumes. The data PVC, ports, resources and injected
// sidecars/init containers are already in place from the framework builder.
func (h *ZkRoleGroupHandler) customizeStatefulSet(
	sts *appsv1.StatefulSet,
	buildCtx *reconciler.RoleGroupBuildContext,
	zkSecurity *security.ZookeeperSecurity,
	secretProvisioner *opgosecurity.SecretProvisioner,
) error {
	roleGroupConfig := buildCtx.RoleGroupSpec.GetConfig()
	podSpec := &sts.Spec.Template.Spec

	// Pod-level settings the framework builder does not set.
	podSpec.ServiceAccountName = zkv1alpha1.DefaultProductName
	podSpec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:  int64Ptr(1001),
		RunAsGroup: int64Ptr(0),
		FSGroup:    int64Ptr(1001),
	}
	podSpec.EnableServiceLinks = boolPtr(false)

	// The base handler may add a generic "config" ConfigMap volume mounted at /etc/config.
	// Drop it; Zookeeper mounts its own config/log/log-config volumes at Kubedoop paths
	// (the main container's volume mounts are replaced wholesale below).
	removeNamedVolume(podSpec, zkv1alpha1.ConfigDirName)
	podSpec.Volumes = append(podSpec.Volumes, h.buildVolumes(buildCtx)...)
	podSpec.Volumes = append(podSpec.Volumes, secretProvisioner.Volumes()...)

	if len(podSpec.Containers) == 0 {
		return fmt.Errorf("base handler produced no main container")
	}
	main := &podSpec.Containers[0]
	// Name the main container "zookeeper" (common.ZkServerContainerName) for backward
	// compatibility with the pre-framework layout. The role name ("server") is still used
	// for the component label, but the container name is intentionally decoupled from it.
	main.Name = common.ZkServerContainerName
	main.Command = []string{"/bin/bash", "-x", "-euo", "pipefail", "-c"}
	main.Args = h.getMainContainerArgs()
	// User envOverrides (already on the container from the builder) win over our defaults.
	main.Env = append(h.getEnvVars(roleGroupConfig), main.Env...)
	main.ReadinessProbe = h.getReadinessProbe(zkSecurity)
	main.LivenessProbe = h.getLivenessProbe(zkSecurity)
	if main.SecurityContext == nil {
		main.SecurityContext = &corev1.SecurityContext{}
	}
	main.VolumeMounts = h.getVolumeMounts()
	main.VolumeMounts = append(main.VolumeMounts, secretProvisioner.VolumeMounts()...)

	// Bridge the ZK log volume into the Vector native sidecar (an init container with
	// restartPolicy: Always) so it can read ZK logs.
	if vectorContainer := sidecar.FindInitContainer(podSpec, "vector"); vectorContainer != nil {
		vectorContainer.VolumeMounts = append(vectorContainer.VolumeMounts, corev1.VolumeMount{
			Name:      zkv1alpha1.LogDirName,
			MountPath: constant.KubedoopLogDir,
			ReadOnly:  true,
		})
	}
	return nil
}

// removeNamedVolume removes a pod volume by name (used to drop the base handler's generic
// config volume).
func removeNamedVolume(podSpec *corev1.PodSpec, name string) {
	kept := podSpec.Volumes[:0]
	for _, v := range podSpec.Volumes {
		if v.Name != name {
			kept = append(kept, v)
		}
	}
	podSpec.Volumes = kept
}

// buildPrepareContainer builds the myid init container. It is one-shot (nil RestartPolicy)
// and registered through the SidecarManager (see registerServerContainers).
func (h *ZkRoleGroupHandler) buildPrepareContainer(image string) corev1.Container {
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
			{Name: "MYID_OFFSET", Value: "1"},
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
		fmt.Sprintf(`LOG_CONFIG_DIR_MOUNT=%s
CONFIG_DIR_MOUNT=%s
CONFIG_DIR=%s
mkdir --parents ${CONFIG_DIR}
echo copying ${LOG_CONFIG_DIR_MOUNT} to ${CONFIG_DIR}, ${CONFIG_DIR_MOUNT} to ${CONFIG_DIR}
cp -RL ${LOG_CONFIG_DIR_MOUNT}* ${CONFIG_DIR}
cp -RL ${CONFIG_DIR_MOUNT}* ${CONFIG_DIR}`, constant.KubedoopLogDirMount, constant.KubedoopConfigDirMount, constant.KubedoopConfigDir),
		`ls /kubedoop/ > /dev/null 2>&1 || true`,
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
	envs := []corev1.EnvVar{
		{Name: "MYID_OFFSET", Value: "1"},
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

// getVolumeMounts returns volume mounts for the main container.
func (h *ZkRoleGroupHandler) getVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: zkv1alpha1.DataDirName, MountPath: constant.KubedoopDataDir},
		{Name: zkv1alpha1.ConfigDirName, MountPath: constant.KubedoopConfigDirMount},
		{Name: zkv1alpha1.LogConfigDirName, MountPath: constant.KubedoopLogDirMount},
		{Name: zkv1alpha1.LogDirName, MountPath: constant.KubedoopLogDir},
	}
}

// buildVolumes returns the config/log/log-config volumes for the pod (the data volume is a
// PVC template added by the framework builder).
func (h *ZkRoleGroupHandler) buildVolumes(
	buildCtx *reconciler.RoleGroupBuildContext,
) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: zkv1alpha1.ConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: buildCtx.ResourceName},
				},
			},
		},
		{
			Name: zkv1alpha1.LogConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: buildCtx.ResourceName},
				},
			},
		},
		{
			Name: zkv1alpha1.LogDirName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: func() *resource.Quantity {
						q := resource.MustParse(zkv1alpha1.MaxZKLogFileSize)
						return &q
					}(),
				},
			},
		},
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

// getReadinessProbe returns the readiness probe for Zookeeper.
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
		PeriodSeconds:    1,
		SuccessThreshold: 1,
		TimeoutSeconds:   1,
	}
}

// Helper functions for pointer types
func int64Ptr(v int64) *int64 { return &v }
func boolPtr(v bool) *bool    { return &v }
