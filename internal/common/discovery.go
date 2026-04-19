package common

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

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

type ZookeeperConnection struct {
	URI   string
	Hosts []string
	Port  int32
	ZNode string
}

// CreateDiscoveryConfigMap creates a discovery ConfigMap for Zookeeper client connections.
func CreateDiscoveryConfigMap(
	ctx context.Context,
	k8sClient ctrlclient.Client,
	owner metav1.Object,
	zkCluster *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
	znodeInfo *ZNodeInfo,
	listenerClass zkv1alpha1.ListenerClass,
) (*corev1.ConfigMap, error) {
	discoverer := &discoverer{
		client:        k8sClient,
		zkCluster:     zkCluster,
		zkSecurity:    zkSecurity,
		znodeInfo:     znodeInfo,
		listenerClass: listenerClass,
	}

	zkconn, err := discoverer.GetZookeeperConnection(ctx)
	if err != nil {
		return nil, err
	}

	suffix := ""
	if listenerClass == zkv1alpha1.ExternalUnstable {
		suffix = "-nodeport"
	}

	cmName := znodeInfo.Name + suffix
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: znodeInfo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       strings.ToLower(zkCluster.Name),
				"app.kubernetes.io/managed-by": "zookeeper-operator",
			},
		},
		Data: map[string]string{
			"ZOOKEEPER":        zkconn.URI,
			"ZOOKEEPER_HOSTS":  strings.Join(zkconn.Hosts, ","),
			"ZOOKEEPER_PORT":   strconv.Itoa(int(zkconn.Port)),
			"ZOOKEEPER_CHROOT": zkconn.ZNode,
		},
	}
	_ = owner // caller sets owner reference
	return cm, nil
}

// Discoverer interface for getting Zookeeper connection info
type Discoverer interface {
	GetZookeeperConnection(ctx context.Context) (*ZookeeperConnection, error)
}

type discoverer struct {
	client        ctrlclient.Client
	zkCluster     *zkv1alpha1.ZookeeperCluster
	zkSecurity    *security.ZookeeperSecurity
	znodeInfo     *ZNodeInfo
	listenerClass zkv1alpha1.ListenerClass
}

func (d *discoverer) GetZookeeperConnection(ctx context.Context) (*ZookeeperConnection, error) {
	var hosts []string
	var err error
	if d.listenerClass == zkv1alpha1.ExternalUnstable {
		hosts, err = d.getNodeportHosts(ctx)
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
		URI:   fmt.Sprintf("%s%s", strings.Join(hosts, ","), znodePath),
		Hosts: hosts,
		Port:  int32(d.zkSecurity.ClientPort()),
		ZNode: znodePath,
	}
	return zkconn, nil
}

func (d *discoverer) getPodHosts() ([]string, error) {
	servers := d.zkCluster.Spec.Servers
	if servers == nil {
		return nil, fmt.Errorf("servers spec is nil")
	}

	roleGroups := servers.RoleGroups
	if roleGroups == nil {
		roleGroups = map[string]zkv1alpha1.RoleGroupSpec{}
	}

	clientPort := d.zkSecurity.ClientPort()
	hosts := make([]string, 0)
	for _, name := range slices.Sorted(maps.Keys(roleGroups)) {
		rg := roleGroups[name]
		replicas := int32(1)
		if rg.Replicas > 0 {
			replicas = rg.Replicas
		}
		roleGroupServiceName := fmt.Sprintf("%s-%s", d.zkCluster.Name, name)
		for i := int32(0); i < replicas; i++ {
			podName := fmt.Sprintf("%s-%d", roleGroupServiceName, i)
			fqdn := fmt.Sprintf("%s.%s.%s.svc.cluster.local:%d",
				podName, roleGroupServiceName, d.zkCluster.Namespace, clientPort)
			hosts = append(hosts, fqdn)
		}
	}

	discoveryLogger.V(1).Info("got pod hosts", "hosts", hosts, "clientPort", clientPort)
	return hosts, nil
}

func (d *discoverer) getNodeportHosts(ctx context.Context) ([]string, error) {
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

	var endpointSliceList discoveryv1.EndpointSliceList
	labelSelector := ctrlclient.MatchingLabels{
		"kubernetes.io/service-name": svcName,
	}
	if err := d.client.List(ctx, &endpointSliceList, ctrlclient.InNamespace(namespace), labelSelector); err != nil {
		return nil, fmt.Errorf("list endpointslices for service %s/%s: %w", namespace, svcName, err)
	}

	if len(endpointSliceList.Items) == 0 {
		return nil, fmt.Errorf("no endpointslices found for service %s/%s", namespace, svcName)
	}

	nodes := make([]string, 0)
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
