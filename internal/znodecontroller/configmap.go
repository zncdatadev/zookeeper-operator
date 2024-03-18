package znodecontroller

import (
	"context"
	"fmt"
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type ConfigmapReconciler struct {
	common.GeneralResourceStyleReconciler[*zkv1alpha1.ZookeeperZnode, *zkv1alpha1.RoleGroupSpec]
	znodePath string
	cluster   *zkv1alpha1.ZookeeperCluster
}

// NewConfigmap new a ConfigmapReconciler
func NewConfigmap(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperZnode,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg *zkv1alpha1.RoleGroupSpec,
	znodePath string,
	cluster *zkv1alpha1.ZookeeperCluster,
) *ConfigmapReconciler {
	return &ConfigmapReconciler{
		GeneralResourceStyleReconciler: *common.NewGeneraResourceStyleReconciler(
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg,
		),
		znodePath: znodePath,
		cluster:   cluster,
	}
}

func (c *ConfigmapReconciler) Build(_ context.Context) (client.Object, error) {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.getConfigmapName(),
			Namespace: c.Instance.Namespace,
			Labels:    c.MergedLabels,
		},
		Data: c.makeData(),
	}, nil
}

// reconcile configMap
// data like below:
//
//	ZOOKEEPER: simple-zk-server-primary-0.simple-zk-server-primary.default.svc.cluster.local:2181/znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1
//	ZOOKEEPER_CHROOT: /znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1
//	ZOOKEEPER_CLIENT_PORT: "2181"
//	ZOOKEEPER_HOSTS: simple-zk-server-primary-0.simple-zk-server-primary.default.svc.cluster.local:2181
func (c *ConfigmapReconciler) makeData() map[string]string {
	return map[string]string{
		"ZOOKEEPER":           c.createSvcConnectionString(c.cluster, c.znodePath),
		"ZOOKEEPER_CHROOT":    c.znodePath,
		"ZOOKEEP_CLIENT_PORT": strconv.Itoa(zkv1alpha1.ServiceClientPort),
		"ZOOKEEPER_HOSTS":     c.getZkClientUrl(),
	}
}

// create svc connection string
// A connection string, as accepted by the official Java client,
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282/znode-4e169890-d2eb-4d62-9515-e4786f0ac58e
// pattern: {node1}:{port1},{node2}:{port2}/{chroot}
func (c *ConfigmapReconciler) createSvcConnectionString(cluster *zkv1alpha1.ZookeeperCluster, path string) string {
	clientUrl := c.getZkClientUrl()
	return fmt.Sprintf("%s%s", clientUrl, path)
}

// get cluster svc client url
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282
// pattern: {node1}:{port1},{node2}:{port2}
func (c *ConfigmapReconciler) getZkClientUrl() string {
	connections := c.cluster.Status.ClientConnections
	var zkNodes string
	if len(connections) > 0 {
		for _, conn := range connections {
			zkNodes += conn + ","
		}
		zkNodes = zkNodes[:len(zkNodes)-1]
		return zkNodes
	}
	return ""
}

// create configmap name
func (c *ConfigmapReconciler) getConfigmapName() string {
	return c.Instance.Name
}
