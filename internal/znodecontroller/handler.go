package znodecontroller

import (
	"context"
	"fmt"
	ctrl "sigs.k8s.io/controller-runtime"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var znodeLogger = ctrl.Log.WithName("znode-controller")

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
func (z *ZNodeReconciler) reconcile(ctx context.Context) (ctrl.Result, error) {
	cluster, err := z.getClusterInstance(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	// 1. create znode in zookeeper
	znodePath := z.createZnodePath()
	if err = z.createZookeeperZnode(znodePath, cluster); err != nil {
		return ctrl.Result{}, err
	}
	// 2. reconcile config map
	cm := NewConfigmap(z.scheme, z.instance, z.client, "", z.instance.Labels, nil, znodePath, cluster)

	var res ctrl.Result
	res, err = cm.ReconcileResource(ctx, common.NewSingleResourceBuilder(cm))
	if err != nil {
		return ctrl.Result{}, err
	}

	if res.RequeueAfter > 0 {
		return res, nil
	}
	return ctrl.Result{}, nil
}

// get cluster instance
func (z *ZNodeReconciler) getClusterInstance(ctx context.Context) (*zkv1alpha1.ZookeeperCluster, error) {
	clusterRef := z.instance.Spec.ClusterRef
	if clusterRef == nil {
		return nil, fmt.Errorf("clusterRef is nil")
	}
	// deprecated: when cluster reference namespace is empty, use znode cr's namespace.
	//var namespace string =
	//if ns := clusterRef.Namespace; ns == "" {
	//	namespace = metav1.NamespaceDefault
	//}
	namespace := clusterRef.Namespace
	if namespace == "" {
		namespace = z.instance.Namespace
	}

	clusterInstance := &zkv1alpha1.ZookeeperCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRef.Name,
			Namespace: namespace,
		},
	}
	resourceClient := common.NewResourceClient(ctx, z.client, namespace)
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
	// for local testing, you must add the zk service to your hosts, and then create port forwarding.
	// example:
	//    127.0.0.1       zookeepercluster-sample-cluster.default.svc.cluster.local
	zkCli, err := NewZkClient(svcDns)
	if err != nil {
		return err
	}
	defer zkCli.Close()
	exists, err := zkCli.Exists(path)
	if err != nil {
		znodeLogger.Error(err, "failed to check if znode exists", "namespace", z.instance.Namespace,
			"name", z.instance.Name, "path", path)
		return err
	}
	if exists {
		znodeLogger.Info("znode already exists", "namespace", z.instance.Namespace,
			"name", z.instance.Name, "zookeeper cluster svc dns", svcDns, "path", path)
		return nil
	}
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
