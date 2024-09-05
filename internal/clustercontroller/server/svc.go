package server

import (
	"context"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
)

var _ builder.ServiceBuilder = &ServiceBuilder{}

type ServiceBuilder struct {
	builder.BaseServiceBuilder
}

func (b *ServiceBuilder) Build(_ context.Context) (ctrlclient.Object, error) {
	obj := b.GetObject()

	// !!! WARNING: !!! This is a workaround for the fact that wether the statefulset headless service return endpoints until the pod is ready
	// Set publishNotReadyAddresses to true to allow the service to return endpoints before the pod is ready
	// So statefulset headless service to to propagate SRV DNS records for its Pods for the purpose of peer discovery
	obj.Spec.PublishNotReadyAddresses = true

	return obj, nil
}

func NewServiceReconciler(
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

	svcBuilder := &ServiceBuilder{
		BaseServiceBuilder: *builder.NewServiceBuilder(
			client,
			option.GetFullName(),
			option.GetLabels(),
			option.GetAnnotations(),
			ports,
			&serviceType,
			true, // as workload is statefulset, so the service is headless
		),
	}

	return &reconciler.Service{
		GenericResourceReconciler: *reconciler.NewGenericResourceReconciler[builder.ServiceBuilder](
			client,
			option.GetFullName(),
			svcBuilder,
		),
	}
}
