package clustercontroller

import (
	"context"
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigMapReconciler create configmap for zookeeper servers(zoo.cfg, logback.xml)
// create configmap for zookeeper script
type ConfigMapReconciler struct {
	common.MultiConfigurationStyleReconciler[*zkv1alpha1.ZookeeperCluster, *zkv1alpha1.RoleGroupSpec]
}

// NewConfigMap new a ConfigMapReconcile
func NewConfigMap(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg *zkv1alpha1.RoleGroupSpec,
) *ConfigMapReconciler {
	return &ConfigMapReconciler{
		MultiConfigurationStyleReconciler: *common.NewMultiConfigurationStyleReconciler(
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg,
		),
	}
}

// Build implements the MultiResourceReconcilerBuilder interface
func (c *ConfigMapReconciler) Build(_ context.Context) ([]common.ResourceBuilder, error) {
	return []common.ResourceBuilder{
		c.createServerConfigmapReconciler(),
		c.createScriptConfigmapReconciler(),
	}, nil
}

// create configmap for zookeeper servers(zoo.cfg, logback.xml, java.env)
func (c *ConfigMapReconciler) createServerConfigmapReconciler() common.ResourceBuilder {
	return common.NewGeneralConfigMap(
		c.Scheme,
		c.Instance,
		c.Client,
		c.GroupName,
		c.MergedLabels,
		c.MergedCfg,
		c.createServerConfigmap, nil)
}

// create configmap for zookeeper script
func (c *ConfigMapReconciler) createScriptConfigmapReconciler() common.ResourceBuilder {
	return common.NewGeneralConfigMap(
		c.Scheme,
		c.Instance,
		c.Client,
		c.GroupName,
		c.MergedLabels,
		c.MergedCfg,
		c.createScriptConfigmap, nil)
}

func (c *ConfigMapReconciler) createServerConfigmap() (client.Object, error) {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createClusterConfigName(c.Instance.Name),
			Namespace: c.Instance.Namespace,
			Labels:    c.MergedLabels,
		},
		Data: c.makeServerEnvData(),
	}, nil
}

func (c *ConfigMapReconciler) createScriptConfigmap() (client.Object, error) {

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createScriptConfigName(c.Instance.Name),
			Namespace: c.Instance.Namespace,
			Labels:    c.MergedLabels,
		},
		Data: map[string]string{
			"init-certs.sh": c.createInitCertsScriptData(),
			"setup.sh":      c.createServerSetupScriptData(),
		},
	}, nil
}

func (c *ConfigMapReconciler) makeServerEnvData() map[string]string {
	var data = map[string]string{
		"BITNAMI_DEBUG":              "false",
		"ZOO_DATA_LOG_DIR":           "",
		"ZOO_PORT_NUMBER":            "2181",
		"ZOO_TICK_TIME":              "2000",
		"ZOO_INIT_LIMIT":             "10",
		"ZOO_SYNC_LIMIT":             "5",
		"ZOO_PRE_ALLOC_SIZE":         "65536",
		"ZOO_SNAPCOUNT":              "100000",
		"ZOO_MAX_CLIENT_CNXNS":       "60",
		"ZOO_4LW_COMMANDS_WHITELIST": "srvr, mntr, ruok",
		"ZOO_LISTEN_ALLIPS_ENABLED":  "no",
		"ZOO_AUTOPURGE_INTERVAL":     "1",
		"ZOO_AUTOPURGE_RETAIN_COUNT": "10",
		"ZOO_MAX_SESSION_TIMEOUT":    "40000",
		"ZOO_SERVERS":                c.createZooServerNetworkName(),
		"ZOO_ENABLE_AUTH":            "no",
		"ZOO_ENABLE_QUORUM_AUTH":     "no",
		"ZOO_HEAP_SIZE":              "1024",
		"ZOO_LOG_LEVEL":              "ERROR",
		"ALLOW_ANONYMOUS_LOGIN":      "yes",
	}
	if extraEnv := c.MergedCfg.Config.ExtraEnv; extraEnv != nil {
		for k, v := range extraEnv {
			data[k] = v
		}
	}
	// todo auth and tls env
	return data
}

// createZooServerNetworkName
// pattern: {instanceName}-{index}.{svc-headless}.{namespace}.svc.{clusterDomain}:{followerPort}:{electionPort}::{index+minId}
// example: "zk0391-3-zookeeper-0.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::1,zk0391-3-zookeeper-1.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::2,zk0391-3-zookeeper-2.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::3",
func (c *ConfigMapReconciler) createZooServerNetworkName() string {
	clusterCfg := c.Instance.Spec.ClusterConfig
	svcIngresName := createHeadlessServiceName(c.Instance.Name, c.GroupName)
	podName := createStatefulSetName(c.Instance.Name, c.GroupName)
	roleCfg := c.MergedCfg
	return createZooServerNetworkName(
		podName,
		roleCfg.Replicas,
		clusterCfg.MinServerId,
		svcIngresName,
		c.Instance.Namespace,
		clusterCfg.ClusterDomain,
	)
}

// create init-certs script data for tls
// todo: tls cert config script
func (c *ConfigMapReconciler) createInitCertsScriptData() string {
	return "#!/bin/bash"
}

// create server setup script data
func (c *ConfigMapReconciler) createServerSetupScriptData() string {
	return `#!/bin/bash

set -ex
# Execute entrypoint as usual after obtaining ZOO_SERVER_ID
# check ZOO_SERVER_ID in persistent volume via myid
# if not present, set based on POD hostname
if [[ -f "/bitnami/zookeeper/data/myid" ]]; then
    export ZOO_SERVER_ID="$(cat /bitnami/zookeeper/data/myid)"
else
    HOSTNAME="$(hostname -s)"
    if [[ $HOSTNAME =~ (.*)-([0-9]+)$ ]]; then
        ORD=${BASH_REMATCH[2]}
        export ZOO_SERVER_ID="$((ORD + 1 ))"
    else
        echo "Failed to get index from hostname $HOSTNAME"
        exit 1
    fi
fi
exec /entrypoint.sh /run.sh
`
}
