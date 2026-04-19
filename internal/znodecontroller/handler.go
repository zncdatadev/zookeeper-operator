package znodecontroller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
)

var znodeLogger = ctrl.Log.WithName("znode-controller")

type ZNodeReconciler struct {
	scheme     *runtime.Scheme
	instance   *zkv1alpha1.ZookeeperZnode
	client     client.Client
	zkSecurity *security.ZookeeperSecurity
}

func NewZNodeReconciler(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperZnode,
	client client.Client,
	zkSecurity *security.ZookeeperSecurity,
) *ZNodeReconciler {
	return &ZNodeReconciler{
		scheme:     scheme,
		instance:   instance,
		client:     client,
		zkSecurity: zkSecurity,
	}
}

func (z *ZNodeReconciler) reconcile(ctx context.Context, cluster *zkv1alpha1.ZookeeperCluster) (ctrl.Result, string, error) {
	// 1. Create znode in zookeeper
	znodePath := z.createZnodePath()
	znodeLogger.Info("create znode in zookeeper", "znode path", znodePath)
	if err := z.createZookeeperZnode(znodePath, cluster); err != nil {
		return ctrl.Result{}, "", err
	}

	// 2. Create discovery ConfigMaps
	znodeLogger.Info("create configmap for zookeeper discovery", "namespace", z.instance.Namespace,
		"name", z.instance.Name, "path", znodePath)

	znodeInfo := &common.ZNodeInfo{
		Name:      z.instance.Name,
		Namespace: z.instance.Namespace,
		ZNodePath: znodePath,
	}

	// Create cluster-internal discovery ConfigMap
	if err := z.reconcileDiscoveryConfigMap(ctx, cluster, znodeInfo, zkv1alpha1.ClusterInternal); err != nil {
		return ctrl.Result{}, "", err
	}

	// Create external-unstable discovery ConfigMap if needed
	if cluster.Spec.ClusterConfig != nil &&
		zkv1alpha1.ListenerClass(cluster.Spec.ClusterConfig.ListenerClass) == zkv1alpha1.ExternalUnstable {
		if err := z.reconcileDiscoveryConfigMap(ctx, cluster, znodeInfo, zkv1alpha1.ExternalUnstable); err != nil {
			return ctrl.Result{}, "", err
		}
	}

	// 3. Update znode status
	if res, err := z.updateZnodeStatus(ctx, znodePath); err != nil {
		return ctrl.Result{}, "", err
	} else if !res.IsZero() {
		return res, znodePath, nil
	}

	znodeLogger.V(1).Info("znode reconciled successfully", "namespace", z.instance.Namespace,
		"name", z.instance.Name, "znode path", znodePath)
	return ctrl.Result{}, znodePath, nil
}

func (z *ZNodeReconciler) reconcileDiscoveryConfigMap(
	ctx context.Context,
	cluster *zkv1alpha1.ZookeeperCluster,
	znodeInfo *common.ZNodeInfo,
	listenerClass zkv1alpha1.ListenerClass,
) error {
	cm, err := common.CreateDiscoveryConfigMap(ctx, z.client, z.instance, cluster, z.zkSecurity, znodeInfo, listenerClass)
	if err != nil {
		return fmt.Errorf("failed to create discovery configmap: %w", err)
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(z.instance, cm, z.scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update
	existing := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: cm.Namespace, Name: cm.Name}
	if err := z.client.Get(ctx, key, existing); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get configmap %s: %w", cm.Name, err)
		}
		if err := z.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("failed to create configmap %s: %w", cm.Name, err)
		}
		znodeLogger.Info("created discovery configmap", "name", cm.Name, "namespace", cm.Namespace)
	} else {
		existing.Data = cm.Data
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		for k, v := range cm.Labels {
			existing.Labels[k] = v
		}
		if err := z.client.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update configmap %s: %w", cm.Name, err)
		}
	}
	return nil
}

func (z *ZNodeReconciler) createZnodePath() string {
	return fmt.Sprintf("/znode-%s", z.instance.GetUID())
}

func (z *ZNodeReconciler) createZookeeperZnode(path string, cluster *zkv1alpha1.ZookeeperCluster) error {
	svcDns := getClusterSvcUrl(cluster, int32(z.zkSecurity.ClientPort()))
	znodeLogger.V(1).Info("zookeeper cluster service client dns url", "dns", svcDns)

	zkCli, err := NewZkClient(svcDns)
	if err != nil {
		return err
	}
	defer zkCli.Close()

	exists, err := zkCli.Exists(path)
	if err != nil {
		znodeLogger.Error(err, "failed to check if znode exists", "path", path)
		return err
	}
	if exists {
		znodeLogger.V(1).Info("znode already exists", "path", path)
		return nil
	}

	znodeLogger.Info("create new znode in zookeeper cluster", "path", path)
	err = zkCli.Create(path, []byte{})
	if err != nil {
		znodeLogger.Error(err, "failed to create znode", "path", path)
		return err
	}
	return nil
}

func (z *ZNodeReconciler) updateZnodeStatus(ctx context.Context, znodePath string) (ctrl.Result, error) {
	if z.instance.Status.ZnodePath == znodePath {
		return ctrl.Result{}, nil
	}

	z.instance.Status.ZnodePath = znodePath
	if err := z.client.Status().Update(ctx, z.instance); err != nil {
		znodeLogger.Error(err, "failed to update znode status", "znodePath", znodePath)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func getClusterSvcUrl(cluster *zkv1alpha1.ZookeeperCluster, clientPort int32) string {
	svcHost := common.ClusterServiceName(cluster.Name)
	dns := util.CreateDnsAccess(svcHost, cluster.Namespace)
	return fmt.Sprintf("%s:%d", dns, clientPort)
}

const ZNodeDeleteFinalizer = "znode.kubedoop.dev/delete-znode"

type ZnodeDeleteFinalizer struct {
	clientPort int32
	Chroot     string
	ZkCluster  *zkv1alpha1.ZookeeperCluster
}

func (z ZnodeDeleteFinalizer) Finalize(context.Context, client.Object) (finalizer.Result, error) {
	zkAddress := getClusterSvcUrl(z.ZkCluster, z.clientPort)
	zkCli, err := NewZkClient(zkAddress)
	if err != nil {
		return finalizer.Result{}, err
	}
	defer zkCli.Close()

	znodeLogger.Info("delete znode from zookeeper", "znode path", z.Chroot)
	err = zkCli.Delete(z.Chroot)
	if err != nil {
		znodeLogger.Error(err, "delete znode from zookeeper error", "znode path", z.Chroot)
		return finalizer.Result{}, err
	}
	znodeLogger.Info("delete znode from zookeeper success", "znode path", z.Chroot)
	return finalizer.Result{}, nil
}
