package clustercontroller

import (
	"context"
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
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
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.CreateClusterServiceName(r.Instance.GetName()),
			Namespace: r.Instance.Namespace,
			Labels:    r.MergedLabels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "",
			Type:      corev1.ServiceTypeClusterIP,
			Selector:  r.MergedLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "tcp-client",
					Port:       zkv1alpha1.ServiceClientPort,
					TargetPort: intstr.FromString(zkv1alpha1.ClientPortName),
				},
				{
					Name:       "tcp-follower",
					Port:       zkv1alpha1.ServiceFollowerPort,
					TargetPort: intstr.FromString(zkv1alpha1.FollowerPortName),
				},
				{
					Name:       "tcp-election",
					Port:       zkv1alpha1.ServiceElectionPort,
					TargetPort: intstr.FromString(zkv1alpha1.ElectionPortName),
				},
			},
		},
	}, nil
}
