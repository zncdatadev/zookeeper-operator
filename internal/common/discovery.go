package common

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func NewDiscoveries(
	ctx context.Context,
	listenerClass zkv1alpha1.ListenerClass,
	client *client.Client,
	cluster *zkv1alpha1.ZookeeperCluster,
	chroot *string,
	zkSecurity *security.ZookeeperSecurity,
	options ...builder.Option,
) []reconciler.ResourceReconciler[builder.ConfigBuilder] {
	// pod host discovery
	// didcovery name is cr name , like "zookeeper-cluster"
	podHostDiscoveryBuilder := NewPodHostsDiscovery(client, cluster, zkSecurity, chroot, options...)
	podHostDiscoveryConfigMapBuilder := DiscoveryToConfigBuilder(podHostDiscoveryBuilder)
	discoveries := []reconciler.ResourceReconciler[builder.ConfigBuilder]{
		reconciler.NewGenericResourceReconciler(client, podHostDiscoveryConfigMapBuilder),
	}
	// if listener class is external unstable, add node port discovery
	// discovery name is fmt.Sprintf("%s-nodeport", cr name), like "zookeeper-cluster-nodeport"
	if listenerClass == zkv1alpha1.ExternalUnstable {
		// node port discovery
		nodePortDiscoveryBuilder := NewNodePortDiscovery(client, cluster, zkSecurity, chroot, options...)
		nodePortDiscoveryConfigMapBuilder := DiscoveryToConfigBuilder(nodePortDiscoveryBuilder)
		discoveries = append(discoveries, reconciler.NewGenericResourceReconciler(client, nodePortDiscoveryConfigMapBuilder))
	}
	return discoveries
}

func NewPodHostsDiscovery(
	client *client.Client,
	cluster *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
	chroot *string,
	options ...builder.Option,
) Discovery {
	cluster = getZkCluster(client, cluster)
	podHostDiscovery := &PodHostDiscoveryBuilder{clusterStatus: &cluster.Status, crName: client.GetOwnerName()}
	podHostDiscovery.DiscoveryBuilder = NewDiscoveryBuilder(client, chroot, zkSecurity, podHostDiscovery)
	return podHostDiscovery
}

func getZkCluster(client *client.Client, cluster *zkv1alpha1.ZookeeperCluster) *zkv1alpha1.ZookeeperCluster {
	if cluster == nil {
		owner := client.GetOwnerReference()
		cluster = owner.(*zkv1alpha1.ZookeeperCluster)
	}
	return cluster
}

func NewNodePortDiscovery(
	client *client.Client,
	cluster *zkv1alpha1.ZookeeperCluster,
	zkSecrity *security.ZookeeperSecurity,
	chroot *string,
	options ...builder.Option,
) Discovery {
	crName := client.GetOwnerName()
	cluster = getZkCluster(client, cluster)
	clusterSvcName := ClusterServiceName(cluster.GetName())
	nodePortDiscovery := &NodePortDiscoveryBuilder{crName: crName, clusterServiceName: clusterSvcName}
	nodePortDiscovery.DiscoveryBuilder = NewDiscoveryBuilder(client, chroot, zkSecrity, nodePortDiscovery)
	return nodePortDiscovery
}

func DiscoveryToConfigBuilder(discovery Discovery) builder.ConfigBuilder {
	return discovery.(builder.ConfigBuilder)
}

// interface that builds the data of configmap
type Discovery interface {
	GetHosts(ctx context.Context) ([]string, error)
	Name() string
}

func NewDiscoveryBuilder(
	client *client.Client,
	chroot *string,
	zkSecrity *security.ZookeeperSecurity,
	impl Discovery,
	options ...builder.Option,
) *DiscoveryBuilder {
	return &DiscoveryBuilder{
		ConfigMapBuilder: builder.NewConfigMapBuilder(client, impl.Name(), options...),
		chroot:           chroot,
		zkSecurity:       zkSecrity,
		impl:             impl,
	}
}

type DiscoveryBuilder struct {
	*builder.ConfigMapBuilder
	chroot     *string
	zkSecurity *security.ZookeeperSecurity

	impl Discovery
}

// Override ConfigMapBuilder.Build method
func (c *DiscoveryBuilder) Build(ctx context.Context) (ctrlclient.Object, error) {
	hosts, err := c.impl.GetHosts(ctx)
	if err != nil {
		return nil, err
	}
	c.AddData(c.MakeData(hosts))
	return c.GetObject(), nil
}

func (c *DiscoveryBuilder) MakeData(hosts []string) map[string]string {
	connectionHosts := c.getAccessHosts(hosts)
	var zkChroot string = ""
	if c.chroot != nil {
		zkChroot = *c.chroot
	}
	return map[string]string{
		"ZOOKEEPER": c.createSvcConnectionString(connectionHosts, zkChroot),
		"ZOOKEEPER_CHROOT": func() string {
			if c.chroot == nil {
				return "/"
			} else {
				return zkChroot
			}
		}(),
		"ZOOKEEPER_CLIENT_PORT": strconv.Itoa(int(c.zkSecurity.ClientPort())),
		"ZOOKEEPER_HOSTS":       connectionHosts,
	}
}

// create svc connection string
// A connection string, as accepted by the official Java client,
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282/znode-4e169890-d2eb-4d62-9515-e4786f0ac58e
// pattern: {node1}:{port1},{node2}:{port2}/{chroot}
func (c *DiscoveryBuilder) createSvcConnectionString(hosts string, path string) string {
	return fmt.Sprintf("%s%s", hosts, path)
}

// get cluster svc client url
// e.g. test-zk-server-default-0.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282,test-zk-server-default-1.test-zk-server-default.kuttl-test-proper-spaniel.svc.cluster.local:2282
// pattern: {node1}:{port1},{node2}:{port2}
func (c *DiscoveryBuilder) getAccessHosts(connections []string) string {
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

var _ Discovery = &PodHostDiscoveryBuilder{}

type PodHostDiscoveryBuilder struct {
	*DiscoveryBuilder
	crName        string
	clusterStatus *zkv1alpha1.ZookeeperClusterStatus
}

// Name implements Discovery.
func (p *PodHostDiscoveryBuilder) Name() string {
	return p.crName
}

// GetHosts implements Discovery.
func (p *PodHostDiscoveryBuilder) GetHosts(_ context.Context) ([]string, error) {
	roleGroupConnections := p.clusterStatus.ClientConnections
	connections := make([]string, 0, len(roleGroupConnections))
	for _, roleGroupConnection := range roleGroupConnections {
		connections = append(connections, roleGroupConnection)
	}
	return connections, nil
}

// node port host discovery
var _ Discovery = &NodePortDiscoveryBuilder{}

type NodePortDiscoveryBuilder struct {
	*DiscoveryBuilder
	clusterServiceName string
	crName             string
}

// Name implements Discovery.
func (n *NodePortDiscoveryBuilder) Name() string {
	return fmt.Sprintf("%s-nodeport", n.crName)
}

// GetHosts implements Discovery.
func (n *NodePortDiscoveryBuilder) GetHosts(ctx context.Context) ([]string, error) {
	cli := n.ConfigMapBuilder.GetClient()
	ns := cli.GetOwnerNamespace()
	// 1. get node port service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.clusterServiceName,
			Namespace: ns,
		},
	}
	err := cli.GetWithObject(ctx, svc)
	if err != nil {
		return nil, err
	}

	// 2. get endpoints
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.clusterServiceName,
			Namespace: ns,
		},
	}
	err = cli.GetWithObject(ctx, endpoints)
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
