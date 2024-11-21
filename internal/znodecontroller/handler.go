package znodecontroller

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var znodeLogger = ctrl.Log.WithName("znode-controller")

type ZNodeReconciler struct {
	scheme     *runtime.Scheme
	instance   *zkv1alpha1.ZookeeperZnode
	client     ctrlclient.Client
	zkSecurity *security.ZookeeperSecurity
}

// NewZNodeReconciler new a ZNodeReconciler
func NewZNodeReconciler(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperZnode,
	client ctrlclient.Client,
	zkSecurity *security.ZookeeperSecurity,
) *ZNodeReconciler {
	return &ZNodeReconciler{
		scheme:     scheme,
		instance:   instance,
		client:     client,
		zkSecurity: zkSecurity,
	}
}

// reconcile
func (z *ZNodeReconciler) reconcile(ctx context.Context, cluster *zkv1alpha1.ZookeeperCluster) (ctrl.Result, string, error) {
	// 1. create znode in zookeeper
	znodePath := z.createZnodePath()
	znodeLogger.Info("create znode in zookeeper", "znode path", znodePath)
	if err := z.createZookeeperZnode(znodePath, cluster); err != nil {
		return ctrl.Result{}, "", err
	}

	// 2. create configmap in zookeeper to display zookeeper cluster info
	znodeLogger.Info("create configmap for zookeeper discovery", "namaspace", z.instance.Namespace,
		"name", z.instance.Name, "path", znodePath)
	client := client.NewClient(z.client, z.instance)
	listenerClass := cluster.Spec.ClusterConfig.ListenerClass
	gvk := z.instance.GetObjectKind().GroupVersionKind()
	clusterInfo := reconciler.ClusterInfo{GVK: &metav1.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind}, ClusterName: z.instance.Name}
	discoveries := common.NewDiscoveries(
		ctx,
		zkv1alpha1.ListenerClass(listenerClass),
		client,
		cluster,
		&znodePath,
		z.zkSecurity,
		func(o *builder.Options) {
			o.Labels = clusterInfo.GetLabels()
			o.Annotations = clusterInfo.GetAnnotations()
		},
	)
	res, err := z.reconcileDiscovery(ctx, discoveries)
	if err != nil {
		znodeLogger.Error(err, "create configmap for zookeeper discovery error",
			"namaspace", z.instance.Namespace, "discovery owner", z.instance.Name, "path", znodePath)
		return ctrl.Result{}, "", err
	}
	if res.RequeueAfter > 0 {
		return res, "", nil
	}
	return ctrl.Result{}, znodePath, nil
}

func (z *ZNodeReconciler) reconcileDiscovery(ctx context.Context, discoveries []reconciler.ResourceReconciler[builder.ConfigBuilder]) (ctrl.Result, error) {
	for _, d := range discoveries {
		res, err := d.Reconcile(ctx)

		if err != nil {
			return ctrl.Result{}, err
		}
		if res.RequeueAfter > 0 {
			return ctrl.Result{RequeueAfter: res.RequeueAfter}, nil
		}
	}
	return ctrl.Result{}, nil
}

// create znode Path
// like: "/znode-d744b792-6194-43bd-a9f6-46d2a6ffeea1"
func (z *ZNodeReconciler) createZnodePath() string {
	return fmt.Sprintf("/znode-%s", z.instance.GetUID())
}

// create zookeeper znode
func (z *ZNodeReconciler) createZookeeperZnode(path string, cluster *zkv1alpha1.ZookeeperCluster) error {
	svcDns := getClusterSvcUrl(cluster, int32(z.zkSecurity.ClientPort()))
	znodeLogger.V(1).Info("zookeeper cluster service client dns url", "dns", svcDns)
	// for local testing, you must add the zk service to your hosts, and then create port forwarding.
	// example:
	//    127.0.0.1       zookeepercluster-sample-cluster.default.svc.cluster.local
	zkCli, err := NewZkClient(svcDns)
	if err != nil {
		return err
	}
	defer zkCli.Close()
	znodeLogger.V(1).Info("check if znode exists", "dns", svcDns, "path", path)
	exists, err := zkCli.Exists(path)
	if err != nil {
		znodeLogger.Error(err, "failed to check if znode exists", "namespace", z.instance.Namespace,
			"name", z.instance.Name, "zookeeper cluster svc dns", svcDns, "path", path)
		return err
	}
	if exists {
		znodeLogger.V(1).Info("znode already exists", "namespace", z.instance.Namespace,
			"name", z.instance.Name, "zookeeper cluster svc dns", svcDns, "path", path)
		return nil
	}
	znodeLogger.V(1).Info("create new znode in zookeeper cluster", "zk cluster svc dns", svcDns, "path", path)
	err = zkCli.Create(path, []byte{})
	if err != nil {
		znodeLogger.Error(err, "failed to create znode", "namespace", z.instance.Namespace, "name",
			z.instance.Name, "zookeeper cluster svc dns", svcDns, "path", path)
		return err
	}
	return nil
}

// get custer service url
func getClusterSvcUrl(cluster *zkv1alpha1.ZookeeperCluster, clientProt int32) string {
	svcHost := common.ClusterServiceName(cluster.Name)
	dns := util.CreateDnsAccess(svcHost, cluster.Namespace, cluster.Spec.ClusterConfig.ClusterDomain)
	return fmt.Sprintf("%s:%d", dns, clientProt)
}

const ZNodeDeleteFinalizer = "znode.kubedoop.dev/delete-znode"

type ZnodeDeleteFinalizer struct {
	clientPort int32
	Chroot     string
	ZkCluster  *zkv1alpha1.ZookeeperCluster
}

func (z ZnodeDeleteFinalizer) Finalize(context.Context, ctrlclient.Object) (finalizer.Result, error) {
	zkAddress := getClusterSvcUrl(z.ZkCluster, z.clientPort)
	// remove znode from zookeeper cluster
	zkCli, err := NewZkClient(zkAddress)
	if err != nil {
		return finalizer.Result{}, err
	}
	defer zkCli.Close()
	znodeLogger.Info("delete znode from zookeeper", "znode path", z.Chroot)
	err = zkCli.Delete(z.Chroot)
	if err != nil {
		znodeLogger.Error(err, "delete znode from zookeeper error", "zookeeper cluster dns", zkAddress,
			"znode path", z.Chroot)
		return finalizer.Result{}, err
	}
	znodeLogger.Info("delete znode from zookeeper success", "znode path", z.Chroot)
	return finalizer.Result{}, nil
}
