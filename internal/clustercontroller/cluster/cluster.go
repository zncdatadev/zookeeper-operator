package cluster

import (
	"context"

	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/operator-go/pkg/util"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/clustercontroller/server"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
)

var _ reconciler.Reconciler = &Reconciler{}

type Reconciler struct {
	reconciler.BaseCluster[*zkv1alpha1.ZookeeperClusterSpec]
	ClusterConfig *zkv1alpha1.ClusterConfigSpec
}

func NewClusterReconciler(
	client *client.Client,
	clusterInfo reconciler.ClusterInfo,
	spec *zkv1alpha1.ZookeeperClusterSpec,
) *Reconciler {
	return &Reconciler{
		BaseCluster: *reconciler.NewBaseCluster(
			client,
			clusterInfo,
			spec.ClusterOperationSpec,
			spec,
		),
		ClusterConfig: spec.ClusterConfig,
	}
}

func (r *Reconciler) GetImage() *util.Image {
	return zkv1alpha1.TransformImage(r.Spec.Image)
}

func (r *Reconciler) RegisterResources(ctx context.Context) error {
	client := r.GetClient()
	clusterLables := r.ClusterInfo.GetLabels()
	annotations := r.ClusterInfo.GetAnnotations()
	zkSecurity, err := security.NewZookeeperSecurity(r.ClusterConfig)
	if err != nil {
		return err
	}
	//rbac
	sa := NewServiceAccountReconciler(*r.Client, clusterLables)
	r.AddResource(sa)
	// rb := NewClusterRoleBindingReconciler(*r.Client, clusterLables)
	// r.AddResource(rb)

	//role
	// zkServerRole :
	roleInfo := reconciler.RoleInfo{ClusterInfo: r.ClusterInfo, RoleName: string(common.Server)}
	zkServerRole := server.NewReconciler(client, roleInfo, r.ClusterOperation, r.ClusterConfig, r.GetImage(), r.Spec.Server)
	if err := zkServerRole.RegisterResources(ctx); err != nil {
		return err
	}
	r.AddResource(zkServerRole)

	// cluster svc
	listenerClass := r.ClusterConfig.ListenerClass
	svc := NewClusterServiceReconciler(r.Client, r.ClusterInfo, listenerClass, zkSecurity)
	r.AddResource(svc)

	// discovery
	listnerClass := r.ClusterConfig.ListenerClass
	dicoveries := common.NewDiscoveries(ctx, zkv1alpha1.ListenerClass(listnerClass), client, nil, nil, clusterLables, annotations, zkSecurity)
	if len(dicoveries) != 0 {
		for _, d := range dicoveries {
			r.AddResource(d)
		}
	}
	return nil
}
