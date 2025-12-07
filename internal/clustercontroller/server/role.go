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

		mergedConfig, err := util.MergeObject(r.Spec.Config, roleGroup.Config)
		if err != nil {
			return err
		}
		overrides, err := util.MergeObject(r.Spec.OverridesSpec, roleGroup.OverridesSpec)
		if err != nil {
			return err
		}
		// merge default config to the provided config
		defaultConfig := common.DefaultServerConfig(r.RoleInfo.ClusterName)
		if mergedConfig == nil {
			mergedConfig = &zkv1alph1.ConfigSpec{}
		}
		if overrides == nil {
			overrides = &commonsv1alpha1.OverridesSpec{}
		}
		err = defaultConfig.MergeDefaultConfig(mergedConfig, overrides)
		if err != nil {
			return err
		}

		info := &reconciler.RoleGroupInfo{
			RoleInfo:      r.RoleInfo,
			RoleGroupName: name,
		}
		reconcilers, err := r.RegisterResourceWithRoleGroup(ctx, info, &roleGroup.Replicas, mergedConfig.RoleGroupConfigSpec, overrides)
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

func (r *Reconciler) RegisterResourceWithRoleGroup(
	ctx context.Context,
	info *reconciler.RoleGroupInfo,
	repilicates *int32,
	mergedRoleGroupConfig *commonsv1alpha1.RoleGroupConfigSpec,
	mergedOverrides *commonsv1alpha1.OverridesSpec,
) ([]reconciler.Reconciler, error) {
	var reconcilers []reconciler.Reconciler
	// security
	zkSecurity, err := security.NewZookeeperSecurity(r.ClusterConfig)
	if err != nil {
		logger.V(1).Info("failed to create zookeeper security", "error", err)
		return nil, err
	}

	// 1. statefulset
	statefulSet, err := NewStatefulsetReconciler(
		r.Client,
		info,
		r.ClusterConfig,
		r.Image,
		repilicates,
		r.ClusterStopped(),
		mergedOverrides,
		mergedRoleGroupConfig,
		zkSecurity)
	if err != nil {
		logger.V(1).Info("failed to create statefulset reconciler", "error", err)
		return nil, err
	}
	reconcilers = append(reconcilers, statefulSet)

	// 2. service
	listenerClass := r.ClusterConfig.ListenerClass
	service := NewServiceReconciler(r.Client, info, listenerClass, zkSecurity)
	reconcilers = append(reconcilers, service)

	// 3. metrics service
	metricsService := NewRoleGroupMetricsService(r.Client, info)
	reconcilers = append(reconcilers, metricsService)

	// 4. cofigmap
	configMap := NewConfigMapReconciler(ctx, r.Client, repilicates, info, mergedOverrides, mergedRoleGroupConfig, zkSecurity)
	reconcilers = append(reconcilers, configMap)

	return reconcilers, nil
}
