package server

import (
	"context"

	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/operator-go/pkg/util"
	zkv1alph1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	ctrl "sigs.k8s.io/controller-runtime"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
)

var (
	logger = ctrl.Log.WithName("controller").WithName("zk-server")
)

var _ reconciler.RoleReconciler = &Reconciler{}

type Reconciler struct {
	reconciler.BaseRoleReconciler[*zkv1alph1.ServerSpec]
	ClusterConfig *zkv1alph1.ClusterConfigSpec
	Image         *util.Image
}

func NewReconciler(
	client *client.Client,
	roleInfo reconciler.RoleInfo,
	clusterOperation *commonsv1alpha1.ClusterOperationSpec,
	clusterConfig *zkv1alph1.ClusterConfigSpec,
	image *util.Image,
	spec *zkv1alph1.ServerSpec,
) *Reconciler {
	clusterStopped := false
	if clusterOperation != nil {
		clusterStopped = clusterOperation.Stopped
	}
	return &Reconciler{
		BaseRoleReconciler: *reconciler.NewBaseRoleReconciler(
			client,
			clusterStopped,
			roleInfo,
			spec,
		),
		Image:         image,
		ClusterConfig: clusterConfig,
	}
}

func (r *Reconciler) RegisterResources(ctx context.Context) error {
	for name, roleGroup := range r.Spec.RoleGroups {
		mergedRoleGroup := r.MergeRoleGroupSpec(&roleGroup)
		defaultConfig := common.DefaultServerConfig(r.RoleInfo.ClusterName)
		mergedCfg := mergedRoleGroup.(*zkv1alph1.RoleGroupSpec)
		// merge default config to the provided config
		defaultConfig.MergeDefaultConfig(mergedCfg)

		info := &reconciler.RoleGroupInfo{
			RoleInfo:      r.RoleInfo,
			RoleGroupName: name,
		}
		reconcilers, err := r.RegisterResourceWithRoleGroup(ctx, info, mergedRoleGroup)
		if err != nil {
			return err
		}

		for _, reconciler := range reconcilers {
			r.AddResource(reconciler)
			logger.Info("registered resource", "role", r.GetName(), "roleGroup", name, "reconciler", reconciler.GetName())
		}

	}
	return nil
}

func (r *Reconciler) RegisterResourceWithRoleGroup(ctx context.Context, info *reconciler.RoleGroupInfo,
	roleGroupSpec any) ([]reconciler.Reconciler, error) {
	var reconcilers []reconciler.Reconciler
	// security
	zkSecurity, err := security.NewZookeeperSecurity(r.ClusterConfig)
	if err != nil {
		logger.V(1).Info("failed to create zookeeper security", "error", err)
		return nil, err
	}

	// 1. statefulset
	statefulSet, err := NewStatefulsetReconciler(r.Client, r.ClusterConfig, info, r.Image, r.ClusterStopped, roleGroupSpec.(*zkv1alph1.RoleGroupSpec), zkSecurity)
	if err != nil {
		logger.V(1).Info("failed to create statefulset reconciler", "error", err)
		return nil, err
	}
	reconcilers = append(reconcilers, statefulSet)

	// 2. service
	listenerClass := r.ClusterConfig.ListenerClass
	service := NewServiceReconciler(r.Client, info, listenerClass, zkSecurity)
	reconcilers = append(reconcilers, service)

	// 3. cofigmap
	configMap := NewConfigMapReconciler(ctx, r.Client, info, roleGroupSpec.(*zkv1alph1.RoleGroupSpec), zkSecurity)
	reconcilers = append(reconcilers, configMap)

	return reconcilers, nil
}
