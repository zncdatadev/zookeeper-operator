package clustercontroller

import (
	"context"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceReconciler cluster service reconcile
type ServiceReconciler struct {
	common.GeneralResourceStyleReconciler[*zkv1alpha1.ZookeeperCluster, *zkv1alpha1.RoleGroupSpec]
}

// NewClusterService  new a ServiceHeadlessReconciler
func NewClusterService(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg *zkv1alpha1.RoleGroupSpec,
) *ServiceReconciler {
	return &ServiceReconciler{
		GeneralResourceStyleReconciler: *common.NewGeneraResourceStyleReconciler(
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg,
		),
	}
}

// Build implements the ResourceBuilder interface
func (r *ServiceReconciler) Build(_ context.Context) (client.Object, error) {
	var serviceType corev1.ServiceType
	listenerClass := r.Instance.Spec.ClusterConfig.ListenerClass
	switch listenerClass {
	case string(zkv1alpha1.ClusterInternal):
		serviceType = corev1.ServiceTypeClusterIP
	case string(zkv1alpha1.ExternalUnstable):
		serviceType = corev1.ServiceTypeNodePort
	default:
		serviceType = corev1.ServiceTypeClusterIP
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ClusterServiceName(r.Instance.GetName()),
			Namespace: r.Instance.Namespace,
			Labels:    r.MergedLabels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: r.MergedLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       zkv1alpha1.ClientPortName,
					Port:       zkv1alpha1.ServiceClientPort,
					TargetPort: intstr.FromString(zkv1alpha1.ClientPortName),
				},
				{
					Name:       zkv1alpha1.FollowerPortName,
					Port:       zkv1alpha1.ServiceFollowerPort,
					TargetPort: intstr.FromString(zkv1alpha1.FollowerPortName),
				},
				{
					Name:       zkv1alpha1.ElectionPortName,
					Port:       zkv1alpha1.ServiceElectionPort,
					TargetPort: intstr.FromString(zkv1alpha1.ElectionPortName),
				},
			},
		},
	}, nil
}
