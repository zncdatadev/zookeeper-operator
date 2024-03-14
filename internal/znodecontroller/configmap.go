package znodecontroller

import (
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
)

type ConfigmapReconciler struct {
	common.ConfigurationStyleReconciler[*zkv1alpha1.ZookeeperZnode, *zkv1alpha1.RoleGroupSpec]
}

// NewConfigmap new a ConfigmapReconciler
