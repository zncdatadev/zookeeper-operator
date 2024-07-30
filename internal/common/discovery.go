package common

import (
	"context"
	"fmt"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type DiscoveryReconciler struct {
	ctx    context.Context
	chroot *string
	MultiConfigurationStyleReconciler[*zkv1alpha1.ZookeeperCluster, *zkv1alpha1.RoleGroupSpec]
}

// NewZookeeperDiscovery new a DiscoveryReconciler
func NewZookeeperDiscovery(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	chroot *string,
) *DiscoveryReconciler {
	var cfg *zkv1alpha1.RoleGroupSpec
	return &DiscoveryReconciler{
		MultiConfigurationStyleReconciler: *NewMultiConfigurationStyleReconciler(
			scheme,
			instance,
			client,
			"",
			instance.Labels,
			cfg,
		),
		chroot: chroot,
	}
}

func (c *DiscoveryReconciler) Build(ctx context.Context) ([]ResourceBuilder, error) {
	c.ctx = ctx
	if c.chroot != nil && (*c.chroot)[0] != '/' {
		return nil, fmt.Errorf("chroot path %s was relative (must be absolute)", *c.chroot)
	}
	discoveries := []ResourceBuilder{c.createPodHostDiscovery()}
	if c.Instance.Spec.ClusterConfig.ListenerClass == string(zkv1alpha1.ExternalUnstable) {
		discoveries = append(discoveries, c.createNodePortDiscovery())
	}
	return discoveries, nil
}

// create pod host discovery
func (c *DiscoveryReconciler) createPodHostDiscovery() ResourceBuilder {
	return NewGeneralConfigMap(
		c.Scheme,
		c.Instance,
		c.Client,
		c.GroupName,
		c.MergedLabels,
		c.MergedCfg,
		c.createPodHostDiscoveryConfigMap, nil)
}

// crate node port discovery
func (c *DiscoveryReconciler) createNodePortDiscovery() ResourceBuilder {
	return NewGeneralConfigMap(
		c.Scheme,
		c.Instance,
		c.Client,
		c.GroupName,
		c.MergedLabels,
		c.MergedCfg,
		c.createNodePortDiscoveryConfigMap, nil)
}

func (c *DiscoveryReconciler) createPodHostDiscoveryConfigMap() (client.Object, error) {
	configmapBuilder := NewConfigMapBuilder(&metav1.ObjectMeta{
		Name:      c.Instance.GetName(),
		Namespace: c.Instance.GetNamespace(),
		Labels:    c.MergedLabels,
	})
	roleGroupConnections := c.Instance.Status.ClientConnections
	var connections []string
	for _, roleGroupConnection := range roleGroupConnections {
		connections = append(connections, roleGroupConnection)
	}
	configmapBuilder.SetData(c.makeData(c.getAccessHosts(connections)))
	return configmapBuilder.Build(), nil
}

// reconcile configMap
// data like below:
//
//	ZOOKEEPER: simple-zk-server-primary-0.simple-zk-server-primary.default.svc.cluster.local:2181/znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1
//	ZOOKEEPER_CHROOT: /znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1
//	ZOOKEEPER_CLIENT_PORT: "2181"
//	ZOOKEEPER_HOSTS: simple-zk-server-primary-0.simple-zk-server-primary.default.svc.cluster.local:2181
func (c *DiscoveryReconciler) makeData(hosts string) map[string]string {
	var zkChroot string = ""
	if c.chroot != nil {
		zkChroot = *c.chroot
	}
	return map[string]string{
		"ZOOKEEPER": c.createSvcConnectionString(hosts, zkChroot),
		"ZOOKEEPER_CHROOT": func() string {
			if c.chroot == nil {
				return "/"
			} else {
				return zkChroot
			}
		}(),
		"ZOOKEEPER_CLIENT_PORT": strconv.Itoa(zkv1alpha1.ServiceClientPort),
		"ZOOKEEPER_HOSTS":       hosts,
	}
}

// create svc connection string
// A connection string, as accepted by the official Java client,
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282/znode-4e169890-d2eb-4d62-9515-e4786f0ac58e
// pattern: {node1}:{port1},{node2}:{port2}/{chroot}
func (c *DiscoveryReconciler) createSvcConnectionString(hosts string, path string) string {
	return fmt.Sprintf("%s%s", hosts, path)
}

// get cluster svc client url
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282
// pattern: {node1}:{port1},{node2}:{port2}
func (c *DiscoveryReconciler) getAccessHosts(connections []string) string {
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

func (c *DiscoveryReconciler) createNodePortDiscoveryConfigMap() (client.Object, error) {
	configmapBuilder := NewConfigMapBuilder(&metav1.ObjectMeta{
		Name:      c.Instance.GetName() + "-nodeport",
		Namespace: c.Instance.GetNamespace(),
		Labels:    c.MergedLabels,
	})
	nodeHosts, err := c.getNodeHosts(c.ctx)
	if err != nil {
		return nil, err
	}
	configmapBuilder.SetData(c.makeData(c.getAccessHosts(nodeHosts)))
	return configmapBuilder.Build(), nil
}

// get node hosts
func (c *DiscoveryReconciler) getNodeHosts(ctx context.Context) ([]string, error) {
	cli := NewResourceClient(ctx, c.Client, c.Instance.Namespace)

	// 1. get node port service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterServiceName(c.Instance.GetName()),
			Namespace: c.Instance.GetNamespace(),
		},
	}
	err := cli.Get(svc)
	if err != nil {
		return nil, err
	}

	// 2. get endpoints
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterServiceName(c.Instance.GetName()),
			Namespace: c.Instance.GetNamespace(),
		},
	}
	err = cli.Get(endpoints)
	if err != nil {
		return nil, err
	}

	// 3. get node port from node port service
	var nodePort int
	for _, v := range svc.Spec.Ports {
		if v.Name == zkv1alpha1.ClientPortName {
			nodePort = int(v.NodePort)
		}
	}

	// 4. get node name from endpoints
	nodes := make(map[string]bool)
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.NodeName != nil {
				nodes[fmt.Sprintf("%s:%d", *addr.NodeName, nodePort)] = true
			}
		}
	}
	return maps.Keys(nodes), nil
}
