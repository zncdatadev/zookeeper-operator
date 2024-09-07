package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/constants"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	oputil "github.com/zncdatadev/operator-go/pkg/util"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

func NewStatefulsetReconciler(
	client *client.Client,
	clusterConfig *zkv1alpha1.ClusterConfigSpec,
	roleGroupInfo *reconciler.RoleGroupInfo,
	image *oputil.Image,
	stopped bool,
	spec *zkv1alpha1.RoleGroupSpec,
	zkSecurity *security.ZookeeperSecurity,
) (reconciler.ResourceReconciler[builder.StatefulSetBuilder], error) {

	options := builder.WorkloadOptions{
		Options: builder.Options{
			ClusterName:   roleGroupInfo.GetClusterName(),
			RoleName:      roleGroupInfo.GetRoleName(),
			RoleGroupName: roleGroupInfo.GetGroupName(),
			Labels:        roleGroupInfo.GetLabels(),
			Annotations:   roleGroupInfo.GetAnnotations(),
		},
		PodOverrides:     spec.PodOverrides,
		CommandOverrides: spec.CommandOverrides,
		EnvOverrides:     spec.EnvOverrides,
	}

	if spec.Config != nil {

		var gracefulShutdownTimeout time.Duration
		var err error

		if spec.Config.GracefulShutdownTimeout != nil {
			gracefulShutdownTimeout, err = time.ParseDuration(*spec.Config.GracefulShutdownTimeout)
			if err != nil {
				return nil, err
			}
		}

		options.TerminationGracePeriod = &gracefulShutdownTimeout
		options.Resource = spec.Config.Resources
		options.Affinity = spec.Config.Affinity
	}

	stsBuilder := NewStatefulSetBuilder(client, roleGroupInfo, spec, clusterConfig, image, zkSecurity, options)

	return reconciler.NewStatefulSet(client, common.StatefulsetName(roleGroupInfo), stsBuilder, stopped), nil
}

var _ builder.StatefulSetBuilder = &StatefulsetBuilder{}

func NewStatefulSetBuilder(
	client *client.Client,
	info *reconciler.RoleGroupInfo,
	mergedCfg *zkv1alpha1.RoleGroupSpec,
	clusterConfig *zkv1alpha1.ClusterConfigSpec,
	image *oputil.Image,
	zkSecurity *security.ZookeeperSecurity,
	options builder.WorkloadOptions,
) *StatefulsetBuilder {
	return &StatefulsetBuilder{
		StatefulSet: *builder.NewStatefulSetBuilder(client, common.StatefulsetName(info), &mergedCfg.Replicas, image, options),
		mergedCfg:   mergedCfg,
		RoleName:    options.RoleName,
		zkSecurity:  zkSecurity,
		info:        info,
	}
}

type StatefulsetBuilder struct {
	builder.StatefulSet
	info          *reconciler.RoleGroupInfo
	mergedCfg     *zkv1alpha1.RoleGroupSpec
	ClusterConfig *zkv1alpha1.ClusterConfigSpec
	RoleName      string

	zkSecurity *security.ZookeeperSecurity
}

func (b *StatefulsetBuilder) Build(ctx context.Context) (ctrlClient.Object, error) {
	b.SetAffinity(b.mergedCfg.Config.Affinity)
	b.AddContainers(b.buildContainers())
	b.AddInitContainer(b.buildInitContainer())
	b.AddVolumes(b.getVolumes())
	b.AddVolumeClaimTemplate(b.createVolumeClaimTemplate())

	obj, err := b.GetObject()
	if err != nil {
		return nil, err
	}

	// tls add volume and volume mount
	podTemplateSpec := &obj.Spec.Template
	zkContainer := &podTemplateSpec.Spec.Containers[0]
	b.zkSecurity.AddVolumeMounts(podTemplateSpec, zkContainer)

	obj.Spec.PodManagementPolicy = appv1.ParallelPodManagement // parallel pod management
	obj.Spec.ServiceName = common.RoleGroupServiceName(b.info) // headless service name
	obj.Spec.Template.Spec.ServiceAccountName = builder.ServiceAccountName(zkv1alpha1.DefaultProductName)

	userId := int64(1001) // service account name
	userGroup := int64(0)
	fsGroup := int64(1001)

	obj.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:  &userId,
		RunAsGroup: &userGroup,
		FSGroup:    &fsGroup,
	}
	isServiceLinks := false
	obj.Spec.Template.Spec.EnableServiceLinks = &isServiceLinks

	//vector
	if IsVectorEnable(b.mergedCfg.Config.Logging) {
		ExtendWorkloadByVector(b.GetImage(), obj, common.RoleGroupConfigMapName(b.info))
	}

	// apend pos host connection to instance status
	b.appendClientConnections(ctx)
	return obj, nil
}

// append client connections to status of instance
func (b *StatefulsetBuilder) appendClientConnections(ctx context.Context) {
	stsName := common.StatefulsetName(b.info)
	svcName := common.RoleGroupServiceName(b.info)
	clientPort := b.zkSecurity.ClientPort()
	replicas := b.GetReplicas()
	connection := common.CreateClientConnectionString(stsName, *replicas, int32(clientPort), svcName, b.GetObjectMeta().Namespace)

	instance := b.GetClient().GetOwnerReference().(*zkv1alpha1.ZookeeperCluster)
	statusConnections := instance.Status.ClientConnections
	groupName := b.info.GetRoleName()
	if statusConnections == nil {
		statusConnections = make(map[string]string)
	}
	statusConnections[groupName] = connection
	instance.Status.ClientConnections = statusConnections
	if err := b.Client.GetCtrlClient().Status().Update(ctx, instance); err != nil {
		logger.Error(err, "failed to update instance status", "namespace", instance.Namespace, "name", instance.Name)
	}
}

func (b *StatefulsetBuilder) buildContainers() []corev1.Container {
	containers := []corev1.Container{}
	image := b.GetImageWithTag()
	mainContainerBuilder := builder.NewContainer(b.RoleName, image).
		SetImagePullPolicy(b.GetImage().GetPullPolicy()).
		SetResources(b.mergedCfg.Config.Resources).
		SetCommand([]string{"/bin/bash", "-x", "-euo", "pipefail", "-c"}).
		SetArgs(b.getMainContainerCommanArgs()).
		AddVolumeMounts(b.getVolumeMounts()).AddEnvVars(b.getEnvVars()).
		AddPorts(b.getPorts()).
		SetReadinessProbe(b.GetReadinessProbe())
	containers = append(containers, *mainContainerBuilder.Build())
	return containers
}

// build init container
func (b *StatefulsetBuilder) buildInitContainer() *corev1.Container {
	image := b.GetImageWithTag()
	prepareContainerBuilder := builder.NewContainer("prepare", image).
		SetImagePullPolicy(b.GetImage().GetPullPolicy()).
		SetCommand([]string{"/bin/bash", "-x", "-euo", "pipefail", "-c"}).
		SetArgs([]string{"expr $MYID_OFFSET + $(echo $POD_NAME | sed 's/.*-//') > /kubedoop/data/myid"}).
		AddVolumeMounts([]corev1.VolumeMount{
			{
				Name:      zkv1alpha1.DataDirName,
				MountPath: constants.KubedoopDataDir,
			},
		}).
		AddEnvVars([]corev1.EnvVar{
			{
				Name:  common.MyIdOffset,
				Value: "1",
			},
			{
				Name:  common.ServerJvmFlags,
				Value: util.JvmJmxOpts(zkv1alpha1.MetricsPort),
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name:  common.ZKServerHeap,
				Value: "409",
			},
		})
	return prepareContainerBuilder.Build()
}

// main container command args
func (b *StatefulsetBuilder) getMainContainerCommanArgs() []string {
	zkConfigPath := path.Join(constants.KubedoopConfigDir, "zoo.cfg")

	var args []string
	args = append(args, fmt.Sprintf(`LOG_CONFIG_DIR_MOUNT=%s
CONFIG_DIR_MOUNT=%s
CONFIG_DIR=%s
mkdir --parents ${CONFIG_DIR}
echo copying ${LOG_CONFIG_DIR_MOUNT} to ${CONFIG_DIR}, ${CONFIG_DIR_MOUNT} to ${CONFIG_DIR}
cp -RL ${LOG_CONFIG_DIR_MOUNT}* ${CONFIG_DIR}
cp -RL ${CONFIG_DIR_MOUNT}* ${CONFIG_DIR}`, constants.KubedoopLogDirMount, constants.KubedoopConfigDirMount, constants.KubedoopConfigDir))
	args = append(args, oputil.CommonBashTrapFunctions)
	args = append(args, oputil.RemoveVectorShutdownFileCommand())
	args = append(args, oputil.InvokePrepareSignalHandlers)
	args = append(args, fmt.Sprintf("bin/zkServer.sh start-foreground %s &", zkConfigPath))
	args = append(args, oputil.InvokeWaitForTermination)
	args = append(args, oputil.CreateVectorShutdownFileCommand())

	script := strings.Join(args, "\n")
	return []string{script}
}

// main container volume mounts
func (b *StatefulsetBuilder) getVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      zkv1alpha1.DataDirName,
			MountPath: constants.KubedoopDataDir,
		},
		{
			Name:      zkv1alpha1.ConfigDirName,
			MountPath: constants.KubedoopConfigDirMount,
		},
		{
			Name:      zkv1alpha1.LogConfigDirName,
			MountPath: constants.KubedoopLogDirMount,
		},
		{
			Name:      zkv1alpha1.LogDirName,
			MountPath: constants.KubedoopLogDir,
		},
	}
}

// main container env vars
func (b *StatefulsetBuilder) getEnvVars() []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{
			Name:  common.MyIdOffset,
			Value: "1",
		},
		{
			Name:  common.ServerJvmFlags,
			Value: util.JvmJmxOpts(zkv1alpha1.MetricsPort),
		},
	}
	heapLimit := common.HeapLimit(b.mergedCfg.Config.Resources)
	if heapLimit != nil {
		envs = append(envs, corev1.EnvVar{
			Name:  common.ZKServerHeap,
			Value: *heapLimit,
		})
	}
	return envs
}

// main container ports
func (b *StatefulsetBuilder) getPorts() []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{
			Name:          zkv1alpha1.ClientPortName,
			ContainerPort: int32(b.zkSecurity.ClientPort()),
		},
		{
			Name:          zkv1alpha1.LeaderPortName,
			ContainerPort: int32(zkv1alpha1.LeaderPort),
		},
		{
			Name:          zkv1alpha1.ElectionPortName,
			ContainerPort: int32(zkv1alpha1.ElectionPort),
		},
		{
			Name:          zkv1alpha1.MetricsPortName,
			ContainerPort: int32(zkv1alpha1.MetricsPort),
		},
	}
}

func (b *StatefulsetBuilder) GetReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"bash",
					"-c",
					// srvr command returns mode of the server, ruok command checks if the server is running
					// !!!Note !!!: if you wanner srvr command work well that you must set `publishNotReadyAddresses=true` in headless service
					fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo srvr >&3 && grep '^Mode: ' <&3", b.zkSecurity.ClientPort()),

					// fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo ruok >&3 && grep 'imok' <&3", b.zkSecurity.ClientPort()),
					// fmt.Sprintf(`exec 3<>/dev/tcp/127.0.0.1/%d && echo srvr >&3 && filename="/tmp/foo_$(date +"%%H%%M%%S%%N")" && cat <&3 > "$filename" && grep "^Mode: " "$filename"`, b.zkSecurity.ClientPort()),
				},
			},
		},
		FailureThreshold: 3,
		PeriodSeconds:    1,
		SuccessThreshold: 1,
		TimeoutSeconds:   1,
	}
}

// get volumes
func (b *StatefulsetBuilder) getVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: zkv1alpha1.ConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: b.GetName(),
					},
				},
			},
		},
		{
			Name: zkv1alpha1.LogConfigDirName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: b.GetName(),
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
						size := productlogging.CalculateLogVolumeSizeLimit([]resource.Quantity{q})
						return &size
					}(),
				},
			},
		},
	}
}

// create data pvc template
func (b *StatefulsetBuilder) createVolumeClaimTemplate() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: zkv1alpha1.DataDirName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeMode:  func() *corev1.PersistentVolumeMode { v := corev1.PersistentVolumeFilesystem; return &v }(),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: b.mergedCfg.Config.Resources.Storage.Capacity,
				},
			},
		},
	}

}
