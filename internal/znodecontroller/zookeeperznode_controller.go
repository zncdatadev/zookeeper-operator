/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package znodecontroller

import (
	"context"
	"fmt"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

// ZookeeperZnodeReconciler reconciles a ZookeeperZnode object
type ZookeeperZnodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ZookeeperZnode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ZookeeperZnodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling zookeeper znode instance")

	znode := &zkv1alpha1.ZookeeperZnode{}

	if err := r.Get(ctx, req.NamespacedName, znode); err != nil {
		if client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "unable to fetch ZookeeperZNode")
			return ctrl.Result{}, err
		}
		r.Log.Info("Zookeeper-zNode resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}
	r.Log.Info("zookeeper-znode resource found", "Name", znode.Name)
	zkCluster, err := r.getClusterInstance(znode, ctx)
	if err != nil {
		return ctrl.Result{RequeueAfter: time.Millisecond * 10000}, err
	}
	// reconcile order by "cluster -> role -> role-group -> resource"
	result, chroot, err := NewZNodeReconciler(r.Scheme, znode, r.Client).reconcile(ctx, zkCluster)

	//setup finalizer
	if err := r.setupFinalizer(znode, zkCluster, ctx, chroot); err != nil {
		return ctrl.Result{}, err
	}

	if err != nil {
		return ctrl.Result{}, err
	} else if result.RequeueAfter > 0 {
		return result, nil
	}
	r.Log.Info("Reconcile successfully ", "Name", znode.Name)
	return ctrl.Result{}, nil
}

// get cluster instance
func (r *ZookeeperZnodeReconciler) getClusterInstance(znode *zkv1alpha1.ZookeeperZnode, ctx context.Context) (*zkv1alpha1.ZookeeperCluster, error) {
	clusterRef := znode.Spec.ClusterRef
	if clusterRef == nil {
		return nil, fmt.Errorf("clusterRef is nil")
	}
	namespace := clusterRef.Namespace
	if namespace == "" {
		namespace = znode.Namespace
	}

	clusterInstance := &zkv1alpha1.ZookeeperCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRef.Name,
			Namespace: namespace,
		},
	}
	resourceClient := common.NewResourceClient(ctx, r.Client, namespace)
	err := resourceClient.Get(clusterInstance)
	if err != nil {
		return nil, err
	}
	return clusterInstance, nil
}

func (r *ZookeeperZnodeReconciler) setupFinalizer(cr *zkv1alpha1.ZookeeperZnode, zkCluster *zkv1alpha1.ZookeeperCluster,
	ctx context.Context, chroot string) error {
	finalizers := finalizer.NewFinalizers()
	err := finalizers.Register(ZNodeDeleteFinalizer, ZnodeDeleteFinalizer{Chroot: chroot, ZkCluster: zkCluster})
	if err != nil {
		return err
	}
	_, err = finalizers.Finalize(ctx, cr)
	if err != nil {
		return err
	}
	err = r.Update(ctx, cr)
	if err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ZookeeperZnodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zkv1alpha1.ZookeeperZnode{}).
		Complete(r)
}
