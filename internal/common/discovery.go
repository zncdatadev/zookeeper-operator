package common

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
)

var discoveryLogger = ctrl.Log.WithName("discovery")

type ZNodeInfo struct {
	Name      string
	Namespace string
	ZNodePath string
}

func NewDiscoveryReconcilers(
	ctx context.Context,
	client *client.Client,
	zkCluster *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
	znodeInfo *ZNodeInfo,
	options ...builder.Option,
) []reconciler.ResourceReconciler[builder.ConfigBuilder] {
	discoveries := make(map[string]Discoverer, 0)
	// create a default cluster-internal discovery configmap
	discovery := NewDiscoverer(client, zkCluster, zkSecurity, znodeInfo, zkv1alpha1.ClusterInternal)
	discoveries[znodeInfo.Name] = discovery
	if zkv1alpha1.ListenerClass(zkCluster.Spec.ClusterConfig.ListenerClass) == zkv1alpha1.ExternalUnstable {
		// create a external discovery configmap
		discovery = NewDiscoverer(client, zkCluster, zkSecurity, znodeInfo, zkv1alpha1.ExternalUnstable)
		discoveries[znodeInfo.Name+"-nodeport"] = discovery
	}

	// create discovery configmaps of znode
	reconcilers := make([]reconciler.ResourceReconciler[builder.ConfigBuilder], 0, len(discoveries))
	for key, discovery := range discoveries {
		reconcilers = append(reconcilers, reconciler.NewGenericResourceReconciler(
			client,
			NewDiscoverConfigmapBuilder(
				client,
				key,
				discovery,
				options...,
			),
		))
	}

	return reconcilers
}

var _ builder.ConfigBuilder = &DiscoverConfigmapBuilder{}

type DiscoverConfigmapBuilder struct {
	builder.ConfigMapBuilder

	discovery Discoverer
}

func NewDiscoverConfigmapBuilder(
	client *client.Client,
	name string,
	discovery Discoverer,
	options ...builder.Option,
) builder.ConfigBuilder {
	return &DiscoverConfigmapBuilder{
		ConfigMapBuilder: *builder.NewConfigMapBuilder(
			client,
			name,
			options...,
		),
		discovery: discovery,
	}
}

func (dcb *DiscoverConfigmapBuilder) Build(ctx context.Context) (ctrlclient.Object, error) {
	zkconn, err := dcb.discovery.GetZookeeperConnection(ctx)
	if err != nil {
		return nil, err
	}
	dcb.AddItem("ZOOKEEPER", zkconn.URI)
	dcb.AddItem("ZOOKEEPER_HOSTS", strings.Join(zkconn.Hosts, ","))
	dcb.AddItem("ZOOKEEPER_PORT", strconv.Itoa(int(zkconn.Port)))
	dcb.AddItem("ZOOKEEPER_CHROOT", zkconn.ZNode)

	return dcb.ConfigMapBuilder.Build(ctx)
}

type ZookeeperConnection struct {
	URI   string
	Hosts []string
	Port  int32
	ZNode string
}

type Discoverer interface {
	GetZookeeperConnection(ctx context.Context) (*ZookeeperConnection, error)
}

func NewDiscoverer(
	client *client.Client,
	zkCluster *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
	znodeInfo *ZNodeInfo,
	listenerClass zkv1alpha1.ListenerClass,
) Discoverer {
	return &discovery{
		client:        client,
		zkCluster:     zkCluster,
		zkSecurity:    zkSecurity,
		znodeInfo:     znodeInfo,
		listenerClass: listenerClass,
	}
}

var _ Discoverer = &discovery{}

type discovery struct {
	client        *client.Client
	zkCluster     *zkv1alpha1.ZookeeperCluster
	zkSecurity    *security.ZookeeperSecurity
	znodeInfo     *ZNodeInfo
	listenerClass zkv1alpha1.ListenerClass
}

func (d *discovery) GetZookeeperConnection(ctx context.Context) (*ZookeeperConnection, error) {
	var hosts []string
	var err error
	if d.listenerClass == zkv1alpha1.ExternalUnstable {
		hosts, err = d.getNodeport(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		hosts, err = d.getPodHosts()
		if err != nil {
			return nil, err
		}
	}

	znodePath := d.znodeInfo.ZNodePath

	if znodePath == "" {
		znodePath = "/"
	}

	if !strings.HasPrefix(znodePath, "/") {
		return nil, fmt.Errorf("znode must start with /")
	}

	zkconn := &ZookeeperConnection{
		// uri example: "host1:port,host2:port,host3:port/znode"
		URI:   fmt.Sprintf("%s%s", strings.Join(hosts, ","), znodePath),
		Hosts: hosts,
		Port:  int32(d.zkSecurity.ClientPort()),
		ZNode: znodePath,
	}
	return zkconn, nil
}

func (d *discovery) getPodHosts() ([]string, error) {
	servers := d.zkCluster.Spec.Servers

	gvk := d.zkCluster.GetObjectKind().GroupVersionKind()
	roleInfo := reconciler.RoleInfo{
		ClusterInfo: reconciler.ClusterInfo{
			GVK: &metav1.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
			ClusterName: d.zkCluster.Name,
		},
		// zkcluster only has one role group for server, so we can use a fixed role name
		RoleName: "server",
	}

	if servers == nil {
		return nil, fmt.Errorf("servers spec is nil")
	}
	roleGroups := servers.RoleGroups
	if roleGroups == nil {
		roleGroups = map[string]zkv1alpha1.RoleGroupSpec{}
	}

	roleGroupsInfo := make([]reconciler.RoleGroupInfo, 0, len(roleGroups))

	for _, name := range slices.Sorted(maps.Keys(roleGroups)) {
		roleGroupsInfo = append(roleGroupsInfo, reconciler.RoleGroupInfo{
			RoleInfo:      roleInfo,
			RoleGroupName: name,
		})
	}

	clientPort := d.zkSecurity.ClientPort()
	hosts := make([]string, 0)
	for _, rgInfo := range roleGroupsInfo {
		rg := roleGroups[rgInfo.RoleGroupName]
		replicas := int32(1)
		if rg.Replicas > 0 {
			replicas = rg.Replicas
		}
		roleGroupServiceName := rgInfo.GetFullName()
		for i := int32(0); i < replicas; i++ {
			podName := fmt.Sprintf("%s-%d", roleGroupServiceName, i)
			fqdn := fmt.Sprintf("%s.%s.%s.svc.cluster.local:%d", podName, roleGroupServiceName, d.zkCluster.Namespace, clientPort)
			hosts = append(hosts, fqdn)
		}
	}

	discoveryLogger.V(1).Info("got pod hosts", "hosts", hosts, "clientPort", clientPort)
	return hosts, nil
}

func (d *discovery) getNodeport(ctx context.Context) ([]string, error) {

	svcName := d.zkCluster.Name
	namespace := d.zkCluster.Namespace

	var svc corev1.Service
	if err := d.client.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: svcName}, &svc); err != nil {
		return nil, fmt.Errorf("get service %s/%s: %w", namespace, svcName, err)
	}

	var nodePort int32
	found := false
	for _, port := range svc.Spec.Ports {
		if port.Name == zkv1alpha1.ClientPortName {
			nodePort = port.NodePort
			found = true
			break
		}
	}
	if !found || nodePort == 0 {
		return nil, fmt.Errorf("no nodePort found for port 'client' in service %s/%s", namespace, svcName)
	}

	// Find EndpointSlices by label selector instead of direct name lookup
	var endpointSliceList discoveryv1.EndpointSliceList
	labelSelector := ctrlclient.MatchingLabels{
		"kubernetes.io/service-name": svcName,
	}
	if err := d.client.Client.List(ctx, &endpointSliceList, ctrlclient.InNamespace(namespace), labelSelector); err != nil {
		return nil, fmt.Errorf("list endpointslices for service %s/%s: %w", namespace, svcName, err)
	}

	if len(endpointSliceList.Items) == 0 {
		return nil, fmt.Errorf("no endpointslices found for service %s/%s", namespace, svcName)
	}

	nodes := make([]string, 0)
	// Collect unique node names from all EndpointSlices
	for _, endpointSlice := range endpointSliceList.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.NodeName != nil && *endpoint.NodeName != "" && !slices.Contains(nodes, *endpoint.NodeName) {
				nodes = append(nodes, *endpoint.NodeName)
			}
		}
	}

	hosts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		hosts = append(hosts, fmt.Sprintf("%s:%d", node, nodePort))
	}
	sort.Strings(hosts)

	discoveryLogger.V(1).Info("got nodeport hosts", "hosts", hosts, "nodePort", nodePort)
	return hosts, nil
}
