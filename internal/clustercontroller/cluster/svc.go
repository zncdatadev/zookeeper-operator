package cluster

import (
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/constants"
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

	svcBuilder := builder.NewServiceBuilder(
		client,
		option.GetFullName(),
		ports,
		func(s *builder.ServiceBuilderOption) {
			s.Labels = option.GetLabels()
			s.Annotations = option.GetAnnotations()
			s.Headless = false
			s.ListenerClass = constants.ListenerClass(listenerClass)
		},
	)

	return &reconciler.Service{
		GenericResourceReconciler: *reconciler.NewGenericResourceReconciler[builder.ServiceBuilder](
			client,
			option.GetFullName(),
			svcBuilder,
		),
	}
}
