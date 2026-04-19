package controller

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/zncdatadev/zookeeper-operator/internal/util"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// buildStatefulSet creates the StatefulSet for a server role group.
func (h *ZkRoleGroupHandler) buildStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
	labels map[string]string,
	zkSecurity *security.ZookeeperSecurity,
	secretProvisioner *opgosecurity.SecretProvisioner,
	image string,
	replicas int32,
) (*appsv1.StatefulSet, error) {
	// Get merged config for resources, env vars, etc.
	roleGroupConfig := buildCtx.RoleGroupSpec.GetConfig()

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildCtx.ResourceName,
			Namespace: buildCtx.ClusterNamespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         buildCtx.ResourceName,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: zkv1alpha1.DefaultProductName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  int64Ptr(1001),
						RunAsGroup: int64Ptr(0),
						FSGroup:    int64Ptr(1001),
					},
					EnableServiceLinks: boolPtr(false),
					InitContainers:     h.buildInitContainers(buildCtx, image),
					Containers:         h.buildContainers(buildCtx, image, zkSecurity, roleGroupConfig),
					Volumes:            h.buildVolumes(buildCtx),
				},
			},
		},
	}

	// Data volume claim template
	if roleGroupConfig != nil && roleGroupConfig.Resources != nil && roleGroupConfig.Resources.Storage != nil {
		sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: zkv1alpha1.DataDirName,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					VolumeMode:  volumeModePtr(corev1.PersistentVolumeFilesystem),
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: roleGroupConfig.Resources.Storage.Capacity,
						},
					},
				},
			},
		}
	}

	// TLS: add CSI secret volumes and volume mounts
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, secretProvisioner.Volumes()...)
	found := false
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == "zookeeper" {
			sts.Spec.Template.Spec.Containers[i].VolumeMounts = append(
				sts.Spec.Template.Spec.Containers[i].VolumeMounts,
				secretProvisioner.VolumeMounts()...)
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("container 'zookeeper' not found in StatefulSet spec")
	}

	return sts, nil
}

// buildContainers creates the main Zookeeper container.
func (h *ZkRoleGroupHandler) buildContainers(
	buildCtx *reconciler.RoleGroupBuildContext,
	image string,
	zkSecurity *security.ZookeeperSecurity,
	roleGroupConfig *commonsv1alpha1.RoleGroupConfigSpec,
) []corev1.Container {
	mainContainer := corev1.Container{
		Name:            "zookeeper",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/bash", "-x", "-euo", "pipefail", "-c"},
		Args:            h.getMainContainerArgs(zkSecurity),
		Env:             h.getEnvVars(roleGroupConfig, zkSecurity),
		Ports:           h.getContainerPorts(zkSecurity),
		VolumeMounts:    h.getVolumeMounts(),
		ReadinessProbe:  h.getReadinessProbe(zkSecurity),
		LivenessProbe:   h.getLivenessProbe(zkSecurity),
		SecurityContext: &corev1.SecurityContext{},
	}

	// Set resources if configured
	if roleGroupConfig != nil && roleGroupConfig.Resources != nil {
		req := &corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
		}
		if roleGroupConfig.Resources.CPU != nil {
			if !roleGroupConfig.Resources.CPU.Min.IsZero() {
				req.Requests[corev1.ResourceCPU] = roleGroupConfig.Resources.CPU.Min
			}
			if !roleGroupConfig.Resources.CPU.Max.IsZero() {
				req.Limits[corev1.ResourceCPU] = roleGroupConfig.Resources.CPU.Max
			}
		}
		if roleGroupConfig.Resources.Memory != nil {
			if !roleGroupConfig.Resources.Memory.Limit.IsZero() {
				req.Limits[corev1.ResourceMemory] = roleGroupConfig.Resources.Memory.Limit
				req.Requests[corev1.ResourceMemory] = roleGroupConfig.Resources.Memory.Limit
			}
		}
		mainContainer.Resources = *req
	}

	return []corev1.Container{mainContainer}
}

// buildInitContainers creates the init container for myid generation.
func (h *ZkRoleGroupHandler) buildInitContainers(
	buildCtx *reconciler.RoleGroupBuildContext,
	image string,
) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "prepare",
			Image:           image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/bash", "-x", "-euo", "pipefail", "-c"},
			Args: []string{
				"expr $MYID_OFFSET + $(echo $POD_NAME | sed 's/.*-//') > /kubedoop/data/myid",
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      zkv1alpha1.DataDirName,
					MountPath: constant.KubedoopDataDir,
				},
			},
			Env: []corev1.EnvVar{
				{Name: "MYID_OFFSET", Value: "1"},
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
			},
		},
	}
}

// getMainContainerArgs returns the command arguments for the main container.
func (h *ZkRoleGroupHandler) getMainContainerArgs(zkSecurity *security.ZookeeperSecurity) []string {
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
		fmt.Sprintf("bin/zkServer.sh start-foreground %s", zkConfigPath),
	}
	script := strings.Join(args, "\n")
	return []string{script}
}

// getEnvVars returns environment variables for the main container.
func (h *ZkRoleGroupHandler) getEnvVars(
	roleGroupConfig *commonsv1alpha1.RoleGroupConfigSpec,
	zkSecurity *security.ZookeeperSecurity,
) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "MYID_OFFSET", Value: "1"},
		{Name: "SERVER_JVMFLAGS", Value: util.JvmJmxOpts(zkv1alpha1.MetricsPort)},
	}

	// Heap limit from memory resources
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

// getContainerPorts returns container ports.
func (h *ZkRoleGroupHandler) getContainerPorts(zkSecurity *security.ZookeeperSecurity) []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{Name: zkv1alpha1.ClientPortName, ContainerPort: int32(zkSecurity.ClientPort())},
		{Name: zkv1alpha1.LeaderPortName, ContainerPort: zkv1alpha1.LeaderPort},
		{Name: zkv1alpha1.ElectionPortName, ContainerPort: zkv1alpha1.ElectionPort},
		{Name: zkv1alpha1.MetricsPortName, ContainerPort: zkv1alpha1.MetricsPort},
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

// buildVolumes returns volumes for the pod.
func (h *ZkRoleGroupHandler) buildVolumes(
	buildCtx *reconciler.RoleGroupBuildContext,
) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: zkv1alpha1.ConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: buildCtx.ResourceName,
					},
				},
			},
		},
		{
			Name: zkv1alpha1.LogConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: buildCtx.ResourceName,
					},
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
					"bash",
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
					"bash",
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
func int64Ptr(v int64) *int64                                                  { return &v }
func boolPtr(v bool) *bool                                                     { return &v }
func volumeModePtr(v corev1.PersistentVolumeMode) *corev1.PersistentVolumeMode { return &v }
