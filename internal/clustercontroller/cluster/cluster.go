package cluster

import (
	"context"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/operator-go/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/clustercontroller/server"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	zkutil "github.com/zncdatadev/zookeeper-operator/internal/util"
)

var _ reconciler.Reconciler = &Reconciler{}

type Reconciler struct {
	reconciler.BaseCluster[*zkv1alpha1.ZookeeperClusterSpec]
	ClusterConfig *zkv1alpha1.ClusterConfigSpec

	cluster *zkv1alpha1.ZookeeperCluster
}

func NewClusterReconciler(
	client *client.Client,
	cluster *zkv1alpha1.ZookeeperCluster,
) *Reconciler {
	gvk := cluster.GetObjectKind().GroupVersionKind()

	clusterInfo := reconciler.ClusterInfo{
		GVK: &metav1.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
		},
		ClusterName: cluster.Name,
	}
	return &Reconciler{
		BaseCluster: *reconciler.NewBaseCluster(
			client,
			clusterInfo,
			cluster.Spec.ClusterOperationSpec,
			&cluster.Spec,
		),
		ClusterConfig: cluster.Spec.ClusterConfig,

		cluster: cluster,
	}
}

func (r *Reconciler) GetImage() *util.Image {
	return zkutil.TransformImage(r.Spec.Image)
}

func (r *Reconciler) RegisterResources(ctx context.Context) error {
	client := r.GetClient()
	clusterLables := r.ClusterInfo.GetLabels()
	annotations := r.ClusterInfo.GetAnnotations()
	zkSecurity, err := security.NewZookeeperSecurity(r.ClusterConfig)
	if err != nil {
		return err
	}
	// rbac
	sa := NewServiceAccountReconciler(*r.Client, clusterLables)
	r.AddResource(sa)
	// rb := NewClusterRoleBindingReconciler(*r.Client, clusterLables)
	// r.AddResource(rb)

	// role
	// zkServerRole :
	roleInfo := reconciler.RoleInfo{ClusterInfo: r.ClusterInfo, RoleName: string(common.Server)}
	zkServerRole := server.NewReconciler(client, roleInfo, r.ClusterOperation, r.ClusterConfig, r.GetImage(), r.Spec.Servers)
	if err := zkServerRole.RegisterResources(ctx); err != nil {
		return err
	}
	r.AddResource(zkServerRole)

	// cluster svc
	listenerClass := r.ClusterConfig.ListenerClass
	svc := NewClusterServiceReconciler(r.Client, r.ClusterInfo, listenerClass, zkSecurity)
	r.AddResource(svc)

	// Add znode root to discovery
	znodeInfo := &common.ZNodeInfo{
		Name:      r.cluster.Name,
		Namespace: r.cluster.Namespace,
		ZNodePath: "/",
	}
	discoveryReconcilers := common.NewDiscoveryReconcilers(
		ctx,
		client,
		r.cluster,
		zkSecurity,
		znodeInfo,
		func(o *builder.Options) {
			o.Labels = clusterLables
			o.Annotations = annotations
		},
	)
	if len(discoveryReconcilers) != 0 {
		for _, d := range discoveryReconcilers {
			r.AddResource(d)
		}
	}
	return nil
}
