package server

import (
	"context"
	"strconv"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	opconstants "github.com/zncdatadev/operator-go/pkg/constants"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
)

const (
	TrueValue = "true"
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
	listenerClass opconstants.ListenerClass,
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

	svcBuilder := &ServiceBuilder{
		BaseServiceBuilder: *builder.NewServiceBuilder(
			client,
			option.GetFullName(),
			ports,
			func(sbo *builder.ServiceBuilderOptions) {
				sbo.ListenerClass = listenerClass
				sbo.Headless = true
				sbo.Labels = option.GetLabels()
				sbo.Annotations = option.GetAnnotations()
			},
		),
	}

	return &reconciler.Service{
		GenericResourceReconciler: *reconciler.NewGenericResourceReconciler[builder.ServiceBuilder](
			client,
			svcBuilder,
		),
	}
}

// NewRoleGroupMetricsService creates a metrics service reconciler using a simple function approach
// This creates a headless service for metrics with Prometheus labels and annotations
func NewRoleGroupMetricsService(
	client *client.Client,
	roleGroupInfo *reconciler.RoleGroupInfo,
) reconciler.Reconciler {
	// Get metrics port
	metricsPort := zkv1alpha1.MetricsPort

	// Create service ports
	servicePorts := []corev1.ContainerPort{
		{
			Name:          zkv1alpha1.MetricsPortName,
			ContainerPort: int32(zkv1alpha1.MetricsPort),
			Protocol:      corev1.ProtocolTCP,
		},
	}

	// Create service name with -metrics suffix
	serviceName := GetMetricsServiceName(roleGroupInfo)

	scheme := "http"
	// Prepare labels (copy from roleGroupInfo and add metrics labels)
	labels := make(map[string]string)
	for k, v := range roleGroupInfo.GetLabels() {
		labels[k] = v
	}
	labels["prometheus.io/scrape"] = TrueValue

	// Prepare annotations (copy from roleGroupInfo and add Prometheus annotations)
	annotations := make(map[string]string)
	for k, v := range roleGroupInfo.GetAnnotations() {
		annotations[k] = v
	}
	annotations["prometheus.io/scrape"] = TrueValue
	// annotations["prometheus.io/path"] = "/metrics"  // Uncomment and modify if a specific path is needed, default is /metrics
	annotations["prometheus.io/port"] = strconv.Itoa(metricsPort)
	annotations["prometheus.io/scheme"] = scheme

	// Create base service builder
	baseBuilder := builder.NewServiceBuilder(
		client,
		serviceName,
		servicePorts,
		func(sbo *builder.ServiceBuilderOptions) {
			sbo.Headless = true
			sbo.ListenerClass = opconstants.ClusterInternal
			sbo.Labels = labels
			sbo.MatchingLabels = roleGroupInfo.GetLabels() // Use original labels for matching
			sbo.Annotations = annotations
		},
	)

	return reconciler.NewGenericResourceReconciler(
		client,
		baseBuilder,
	)
}

func GetMetricsServiceName(roleGroupInfo *reconciler.RoleGroupInfo) string {
	return roleGroupInfo.GetFullName() + "-metrics"
}
