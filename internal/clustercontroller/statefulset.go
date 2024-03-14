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

// Build implement the ResourceBuilder interface
func (s *StatefulSetReconciler) Build(_ context.Context) (client.Object, error) {
	return nil, nil
}

// create statefulset for zookeeper cluster
func (s *StatefulSetReconciler) createStatefulSet() (client.Object, error) {
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
	return obj, nil
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
		Command:         []string{"/script/setup.sh"},
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
			Name:      createDataPvcName(s.Instance.GetName(), s.GroupName),
		},
	}
}

// create volumes
func (s *StatefulSetReconciler) createVolumes() []corev1.Volume {
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
					LocalObjectReference: corev1.LocalObjectReference{
						Name: createScriptConfigName(s.Instance.GetName(), s.GroupName),
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
	return []corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createDataPvcName(s.Instance.GetName(), s.GroupName),
			Namespace: s.Instance.Namespace,
			Labels:    s.MergedLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(s.MergedCfg.Config.StorageSize),
				},
			},
		},
	}}
}
