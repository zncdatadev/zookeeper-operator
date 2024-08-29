package server

import (
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
)

// headless service for role group
func NewRoleGroupServiceReconciler(
	client *client.Client,
	option *reconciler.RoleGroupInfo,
	listenerClass string,
	zkSecurity *security.ZookeeperSecurity,
) *reconciler.Service {
	ports := []corev1.ContainerPort{
		{
			Name:          zkv1alpha1.ClientPortName,
			ContainerPort: int32(zkSecurity.ClientPort()),
		},
		{
			Name:          zkv1alpha1.MetricsPortName,
			ContainerPort: int32(zkv1alpha1.MetricsPort),
		},
	}

	var serviceType corev1.ServiceType = corev1.ServiceTypeClusterIP

	svcBuilder := builder.NewServiceBuilder(
		client,
		option.GetFullName(),
		option.GetLabels(),
		option.GetAnnotations(),
		ports,
		&serviceType,
		true, // as workload is statefulset, so the service is headless
	)

	return &reconciler.Service{
		GenericResourceReconciler: *reconciler.NewGenericResourceReconciler[builder.ServiceBuilder](
			client,
			option.GetFullName(),
			svcBuilder,
		),
	}
}
