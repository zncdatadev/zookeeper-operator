package common

import (
	"context"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const LogCfgName = "log.properties"

type RoleLoggingDataBuilder interface {
	MakeContainerLogData() map[string]string
}

type LoggingRecociler struct {
	GeneralResourceStyleReconciler[*zkv1alpha1.ZookeeperCluster, any]
	RoleLoggingDataBuilder RoleLoggingDataBuilder
	role                   Role
}

// NewLoggingReconciler new logging reconcile
func NewLoggingReconciler(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg any,
	logDataBuilder RoleLoggingDataBuilder,
	role Role,
) *LoggingRecociler {
	return &LoggingRecociler{
		GeneralResourceStyleReconciler: *NewGeneraResourceStyleReconciler[*zkv1alpha1.ZookeeperCluster, any](
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg,
		),
		RoleLoggingDataBuilder: logDataBuilder,
		role:                   role,
	}
}

// Build log4j config map
func (l *LoggingRecociler) Build(_ context.Context) (client.Object, error) {
	cmData := l.RoleLoggingDataBuilder.MakeContainerLogData()
	if len(cmData) == 0 {
		return nil, nil
	}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CreateRoleGroupLoggingConfigMapName(l.Instance.Name, string(l.role), l.GroupName),
			Namespace: l.Instance.Namespace,
			Labels:    l.MergedLabels,
		},
		Data: cmData,
	}
	return obj, nil
}
