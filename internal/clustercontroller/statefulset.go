package clustercontroller

import (
	"context"
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulSetReconciler struct {
	common.WorkloadStyleReconciler[*zkv1alpha1.ZookeeperCluster, *zkv1alpha1.RoleGroupSpec]
}

// NewStatefulSet new a StatefulSetReconciler
func NewStatefulSet(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	labels map[string]string,
	mergedCfg *zkv1alpha1.RoleGroupSpec,
	replicate int32,
) *StatefulSetReconciler {
	return &StatefulSetReconciler{
		WorkloadStyleReconciler: *common.NewWorkloadStyleReconciler(
			scheme,
			instance,
			client,
			groupName,
			labels,
			mergedCfg,
			replicate,
		),
	}
}

// GetConditions implement the ConditionGetter interface
func (s *StatefulSetReconciler) GetConditions() *[]metav1.Condition {
	return &s.Instance.Status.Conditions
}

// CommandOverride implement the WorkloadOverride interface
func (s *StatefulSetReconciler) CommandOverride(resource client.Object) {
	dep := resource.(*appsv1.StatefulSet)
	containers := dep.Spec.Template.Spec.Containers
	if cmdOverride := s.MergedCfg.CommandArgsOverrides; cmdOverride != nil {
		for i := range containers {
			containers[i].Command = cmdOverride
		}
	}
}

// EnvOverride implement the WorkloadOverride interface
func (s *StatefulSetReconciler) EnvOverride(resource client.Object) {
	dep := resource.(*appsv1.StatefulSet)
	containers := dep.Spec.Template.Spec.Containers
	if envOverride := s.MergedCfg.EnvOverrides; envOverride != nil {
		for i := range containers {
			envVars := containers[i].Env
			common.OverrideEnvVars(&envVars, s.MergedCfg.EnvOverrides)
		}
	}
}

// LogOverride implement the WorkloadOverride interface
func (s *StatefulSetReconciler) LogOverride(resource client.Object) {
	if s.isLoggersOverrideEnabled() {
		s.logVolumesOverride(resource)
		s.logVolumeMountsOverride(resource)
	}
}

// is loggers override enabled
func (s *StatefulSetReconciler) isLoggersOverrideEnabled() bool {
	return s.MergedCfg.Config.Logging != nil
}

func (s *StatefulSetReconciler) logVolumesOverride(resource client.Object) {
	dep := resource.(*appsv1.StatefulSet)
	volumes := dep.Spec.Template.Spec.Volumes
	if len(volumes) == 0 {
		volumes = make([]corev1.Volume, 1)
	}
	volumes = append(volumes, corev1.Volume{
		Name: s.logVolumeName(),
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: common.CreateRoleGroupLoggingConfigMapName(s.Instance.Name, string(common.Server),
						s.GroupName),
				},
				Items: []corev1.KeyToPath{
					{
						Key:  zkv1alpha1.LogbackFileName,
						Path: zkv1alpha1.LogbackFileName,
					},
				},
			},
		},
	})
	dep.Spec.Template.Spec.Volumes = volumes
}

func (s *StatefulSetReconciler) logVolumeMountsOverride(resource client.Object) {
	dep := resource.(*appsv1.StatefulSet)
	containers := dep.Spec.Template.Spec.Containers
	for i := range containers {
		containers[i].VolumeMounts = append(containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      s.logVolumeName(),
			MountPath: "/opt/bitnami/zookeeper/conf/" + zkv1alpha1.LogbackFileName,
			SubPath:   zkv1alpha1.LogbackFileName,
		})
	}
}

// Build implement the ResourceBuilder interface
func (s *StatefulSetReconciler) Build(_ context.Context) (client.Object, error) {
	saToken := false
	obj := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createStatefulSetName(s.Instance.GetName(), s.GroupName),
			Namespace: s.Instance.Namespace,
			Labels:    s.MergedLabels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &s.MergedCfg.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: s.MergedLabels,
			},
			ServiceName: createHeadlessServiceName(s.Instance.GetName(), s.GroupName),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: s.MergedLabels,
				},
				Spec: corev1.PodSpec{
					SecurityContext:              s.MergedCfg.Config.SecurityContext,
					AutomountServiceAccountToken: &saToken,
					Containers:                   s.createZookeeperContainers(),
					ServiceAccountName:           createServiceAccountName(s.Instance.GetName(), s.GroupName),
					Volumes:                      s.createVolumes(),
				},
			},
			VolumeClaimTemplates: s.createPvcTemplates(),
		},
	}
	// update client connections in status of cluster instance
	// can be used by znode creation
	s.appendClientConnections()
	return obj, nil
}

// append client connections to status of instance
func (s *StatefulSetReconciler) appendClientConnections() {
	connection := createClientConnectionString(s.Instance.Name, s.Replicas,
		createHeadlessServiceName(s.Instance.Name, s.GroupName), s.Instance.Namespace,
		s.Instance.Spec.ClusterConfig.ClusterDomain)
	statusConnections := s.Instance.Status.ClientConnections
	if statusConnections == nil {
		statusConnections = make(map[string]string)
	}
	statusConnections[s.GroupName] = connection
}

// create zookeeper container
func (s *StatefulSetReconciler) createZookeeperContainers() []corev1.Container {
	imageSpec := s.Instance.Spec.Image
	return []corev1.Container{{
		Name:            "zookeeper",
		Image:           imageSpec.Repository + ":" + imageSpec.Tag,
		ImagePullPolicy: imageSpec.PullPolicy,
		Resources:       *common.ConvertToResourceRequirements(s.MergedCfg.Config.Resources),
		Env:             s.createEnvVars(),
		EnvFrom:         s.createEnvFrom(),
		Ports:           s.createContainerPorts(),
		Command:         []string{"/scripts/setup.sh"},
		VolumeMounts:    s.createVolumesMounts(),
		LivenessProbe:   s.createHealthLiveProbe(),
		ReadinessProbe:  s.createReadinessProbe(),
	}}
}

// create envVars
func (s *StatefulSetReconciler) createEnvVars() []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				},
			},
		},
	}
	return envVars
}

// create env from
func (s *StatefulSetReconciler) createEnvFrom() []corev1.EnvFromSource {
	return []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: createClusterConfigName(s.Instance.Name),
				},
			},
		},
	}
}

// create container ports
func (s *StatefulSetReconciler) createContainerPorts() []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{
			Name:          zkv1alpha1.ClientPortName,
			ContainerPort: zkv1alpha1.ClientPort,
		},
		{
			Name:          zkv1alpha1.FollowerPortName,
			ContainerPort: zkv1alpha1.FollowerPort,
		},
		{
			Name:          zkv1alpha1.ElectionPortName,
			ContainerPort: zkv1alpha1.ElectionPort,
		},
	}
}

// create volume mounts
func (s *StatefulSetReconciler) createVolumesMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			MountPath: "/tmp",
			Name:      emptyDirVolumeName(),
			SubPath:   "tmp-dir",
		},
		{
			MountPath: "/opt/bitnami/zookeeper/conf",
			Name:      emptyDirVolumeName(),
			SubPath:   "app-conf-dir",
		},
		{
			MountPath: "/opt/bitnami/zookeeper/logs",
			Name:      emptyDirVolumeName(),
			SubPath:   "app-logs-dir",
		},
		{
			MountPath: "/scripts/setup.sh",
			Name:      scriptVolumeName(),
			SubPath:   "setup.sh",
		},
		{
			MountPath: "/bitnami/zookeeper",
			Name:      createDataPvcName(),
		},
	}
}

// create volumes
func (s *StatefulSetReconciler) createVolumes() []corev1.Volume {
	accessMode := int32(493)
	return []corev1.Volume{
		{
			Name: emptyDirVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: scriptVolumeName(),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &accessMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: createScriptConfigName(s.Instance.GetName()),
					},
				},
			},
		},
	}
}

func emptyDirVolumeName() string {
	return "empty-dir"
}

func scriptVolumeName() string {
	return "script"
}

// create health live probe
func (s *StatefulSetReconciler) createHealthLiveProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-ec",
					"ZOO_HC_TIMEOUT=2 /opt/bitnami/scripts/zookeeper/healthcheck.sh",
				},
			},
		},
		FailureThreshold:    6,
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      5,
	}
}

// create readiness probe
func (s *StatefulSetReconciler) createReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-ec",
					"ZOO_HC_TIMEOUT=2 /opt/bitnami/scripts/zookeeper/healthcheck.sh",
				},
			},
		},
		FailureThreshold:    6,
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      5,
	}
}

// create pvc template
func (s *StatefulSetReconciler) createPvcTemplates() []corev1.PersistentVolumeClaim {
	mode := corev1.PersistentVolumeFilesystem
	return []corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createDataPvcName(),
			Namespace: s.Instance.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			VolumeMode: &mode,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(s.MergedCfg.Config.StorageSize),
				},
			},
		},
	}}
}

// create log4j2 volume name
func (s *StatefulSetReconciler) logVolumeName() string {
	return "log-volume"
}
