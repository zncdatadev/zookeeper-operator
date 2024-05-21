package znodecontroller

import (
	"context"
	"fmt"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ZNodeReconciler struct {
	scheme   *runtime.Scheme
	instance *zkv1alpha1.ZookeeperZnode
	client   client.Client
}

// NewZNodeReconciler new a ZNodeReconciler
func NewZNodeReconciler(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperZnode,
	client client.Client,
) *ZNodeReconciler {
	return &ZNodeReconciler{
		scheme:   scheme,
		instance: instance,
		client:   client,
	}
}

// reconcile
func (z *ZNodeReconciler) reconcile(ctx context.Context) error {
	cluster, err := z.getClusterInstance(ctx)
	if err != nil {
		return err
	}
	// 1. create znode
	znodePath := z.createZnodePath()
	if err = z.createZookeeperZnode(znodePath, cluster); err != nil {
		return err
	}
	// 2. reconcile config map
	cm := NewConfigmap(z.scheme, z.instance, z.client, "", z.instance.Labels, nil, znodePath, cluster)
	_, err = cm.ReconcileResource(ctx, common.NewSingleResourceBuilder(cm))
	if err != nil {
		return err
	}
	return nil
}

// get cluster instance
func (z *ZNodeReconciler) getClusterInstance(ctx context.Context) (*zkv1alpha1.ZookeeperCluster, error) {
	clusterRef := z.instance.Spec.ClusterRef
	if clusterRef == nil {
		return nil, fmt.Errorf("clusterRef is nil")
	}
	var namespace string
	if ns := clusterRef.Namespace; ns == "" {
		namespace = metav1.NamespaceDefault
	}
	clusterInstance := &zkv1alpha1.ZookeeperCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRef.Name,
			Namespace: namespace,
		},
	}
	resourceClient := common.NewResourceClient(ctx, z.client, clusterRef.Namespace)
	err := resourceClient.Get(clusterInstance)
	if err != nil {
		return nil, err
	}
	return clusterInstance, nil
}

// create znode Path
// like: "/znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1"
func (z *ZNodeReconciler) createZnodePath() string {
	return fmt.Sprintf("/znode-%s", z.instance.GetUID())
}

// create zookeeper znode
func (z *ZNodeReconciler) createZookeeperZnode(path string, cluster *zkv1alpha1.ZookeeperCluster) error {
	svcDns := z.getClusterSvcUrl(cluster)
	logger.Info("zookeeper cluster service client dns url", "dns", svcDns)
	zkCli, err := NewZkClient(svcDns)
	if err != nil {
		return err
	}
	defer zkCli.Close()
	err = zkCli.Create(path, []byte{})
	if err != nil {
		return err
	}
	return nil
}

// get custer service url
func (z *ZNodeReconciler) getClusterSvcUrl(cluster *zkv1alpha1.ZookeeperCluster) string {
	svcHost := common.CreateClusterServiceName(cluster.Name)
	dns := util.CreateDnsAccess(svcHost, cluster.Namespace, cluster.Spec.ClusterConfig.ClusterDomain)
	return fmt.Sprintf("%s:%d", dns, zkv1alpha1.ClientPort)
}
