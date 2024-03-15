package clustercontroller

import (
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewServerLogging(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg any,
	logDataBuilder common.RoleLoggingDataBuilder,
	role common.Role,
) *common.LoggingRecociler {
	return common.NewLoggingReconciler(scheme, instance, client, groupName, mergedLabels, mergedCfg, logDataBuilder, role)
}

type LogDataBuilder struct {
	cfg *zkv1alpha1.RoleGroupSpec
}

// MakeContainerLogData MakeContainerLog4jData implement RoleLoggingDataBuilder
func (c *LogDataBuilder) MakeContainerLogData() map[string]string {
	cfg := c.cfg
	data := make(map[string]string)
	// logger data
	if logging := cfg.Config.Logging; logging != nil {
		loggers := logging.Zookeeper.Loggers
		if len(loggers) > 0 {
			var lines string
			for logger, level := range loggers {
				lines = lines + logger + "=" + level.Level + "\n"
			}
			data[common.LogCfgName] = lines
		}
	}
	return data
}
