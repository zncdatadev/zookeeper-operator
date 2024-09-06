package server

import (
	"context"

	"emperror.dev/errors"
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var vectorLogger = ctrl.Log.WithName("vector")

const ContainerVector = "vector"

func IsVectorEnable(roleLoggingConfig *zkv1alpha1.ContainerLoggingSpec) bool {
	if roleLoggingConfig != nil {
		return roleLoggingConfig.EnableVectorAgent
	}
	return false

}

type VectorConfigParams struct {
	Client        client.Client
	ClusterConfig *zkv1alpha1.ClusterConfigSpec
	Namespace     string
	InstanceName  string
	Role          string
	GroupName     string
}

func generateVectorYAML(ctx context.Context, params VectorConfigParams) (string, error) {
	aggregatorConfigMapName := params.ClusterConfig.VectorAggregatorConfigMapName
	if aggregatorConfigMapName == nil {
		return "", errors.New("vectorAggregatorConfigMapName is not set")
	}
	return productlogging.MakeVectorYaml(ctx, params.Client, params.Namespace, params.InstanceName, params.Role,
		params.GroupName, *aggregatorConfigMapName)
}

func ExtendConfigMapByVector(ctx context.Context, params VectorConfigParams, data map[string]string) {
	if data == nil {
		data = map[string]string{}
	}
	vectorYaml, err := generateVectorYAML(ctx, params)
	if err != nil {
		vectorLogger.Error(errors.Wrap(err, "error creating vector YAML"), "failed to create vector YAML")
	} else {
		data[builder.VectorConfigFile] = vectorYaml
	}
}

func ExtendWorkloadByVector(
	logProvider []string,
	dep *appsv1.StatefulSet,
	vectorConfigMapName string) {
	decorator := builder.VectorDecorator{
		WorkloadObject:           dep,
		LogVolumeName:            zkv1alpha1.LogDirName,
		VectorConfigVolumeName:   zkv1alpha1.ConfigDirName,
		VectorConfigMapName:      vectorConfigMapName,
		LogProviderContainerName: logProvider,
	}
	err := decorator.Decorate()
	if err != nil {
		return
	}

	var vectorContainer *corev1.Container
	for i, container := range dep.Spec.Template.Spec.Containers {
		if container.Name == ContainerVector {
			vectorContainer = &dep.Spec.Template.Spec.Containers[i]
			break
		}
	}
	if vectorContainer == nil {
		return
	}

	vectorDataVolumeName := "vector-data"

	// todo: update operator to to support kubedoop vector
	// check if volume exists
	for _, volume := range dep.Spec.Template.Spec.Volumes {
		if volume.Name == vectorDataVolumeName {
			return
		}
	}

	// add emptydir volume
	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "vector-data",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	// check if volume mount exists
	for _, volumeMount := range vectorContainer.VolumeMounts {
		if volumeMount.Name == vectorDataVolumeName {
			return
		}
	}

	// mount emptydir with data to /kubedoop/vector/var
	vectorContainer.VolumeMounts = append(vectorContainer.VolumeMounts, corev1.VolumeMount{
		Name:      vectorDataVolumeName,
		MountPath: "/kubedoop/vector/var",
	})
}
