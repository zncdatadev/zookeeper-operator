package cluster

import (
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
)

func NewClusterServiceReconciler(
	client *client.Client,
	option reconciler.ClusterInfo,
	listenerClass string,
	zkSecurity *security.ZookeeperSecurity,
) *reconciler.Service {
	ports := []corev1.ContainerPort{
		{
			Name:          zkv1alpha1.ClientPortName,
			ContainerPort: int32(zkSecurity.ClientPort()),
		},
	}

	var serviceType corev1.ServiceType
	switch listenerClass {
	case string(zkv1alpha1.ClusterInternal):
		serviceType = corev1.ServiceTypeClusterIP
	case string(zkv1alpha1.ExternalUnstable):
		serviceType = corev1.ServiceTypeNodePort
	default:
		serviceType = corev1.ServiceTypeClusterIP
	}

	svcBuilder := builder.NewServiceBuilder(
		client,
		option.GetFullName(),
		option.GetLabels(),
		option.GetAnnotations(),
		ports,
		&serviceType,
		false,
	)

	return &reconciler.Service{
		GenericResourceReconciler: *reconciler.NewGenericResourceReconciler[builder.ServiceBuilder](
			client,
			option.GetFullName(),
			svcBuilder,
		),
	}
}
