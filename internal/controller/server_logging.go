package controller

import (
	"context"
	"fmt"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/operator-go/pkg/vector"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// buildVectorConfigMapData generates Vector log aggregation config for the ConfigMap.
// Returns nil if Vector is not enabled (SidecarManager is nil).
// Returns an error if Vector is enabled but VectorAggregatorConfigMapName is not configured.
func (h *ZkRoleGroupHandler) buildVectorConfigMapData(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
) (map[string]string, error) {
	if buildCtx.SidecarManager == nil {
		return nil, nil
	}

	if cr.Spec.ClusterConfig == nil || cr.Spec.ClusterConfig.VectorAggregatorConfigMapName == nil || *cr.Spec.ClusterConfig.VectorAggregatorConfigMapName == "" {
		return nil, fmt.Errorf("vector sidecar is enabled but vectorAggregatorConfigMapName is not configured")
	}

	configMapName := *cr.Spec.ClusterConfig.VectorAggregatorConfigMapName

	aggregatorAddress, err := vector.DiscoverAggregatorAddress(ctx, k8sClient, buildCtx.ClusterNamespace, configMapName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover vector aggregator address: %w", err)
	}

	configStr, err := vector.RenderVectorConfig(vector.VectorConfigData{
		LogDir:            constant.KubedoopLogDir,
		AggregatorAddress: aggregatorAddress,
		Namespace:         buildCtx.ClusterNamespace,
		ClusterName:       buildCtx.ClusterName,
		RoleName:          buildCtx.RoleName,
		RoleGroupName:     buildCtx.RoleGroupName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render vector config: %w", err)
	}

	return map[string]string{
		"vector.yaml": configStr,
	}, nil
}
