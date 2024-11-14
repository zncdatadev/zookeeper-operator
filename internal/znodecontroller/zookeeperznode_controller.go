/*
Copyright 2024 zncdatadev.

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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/zncdatadev/operator-go/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
)

var ErrZookeeperCluster = errors.New("zookeeper cluster get failed")

// ZookeeperZnodeReconciler reconciles a ZookeeperZnode object
type ZookeeperZnodeReconciler struct {
	ctrlclient.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperznodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ZookeeperZnodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling zookeeper znode instance")

	znode := &zkv1alpha1.ZookeeperZnode{}

	if err := r.Get(ctx, req.NamespacedName, znode); err != nil {
		if ctrlclient.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "unable to fetch ZookeeperZNode")
			return ctrl.Result{}, err
		}
		r.Log.Info("Zookeeper-zNode resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}
	r.Log.Info("zookeeper-znode resource found", "Name", znode.Name)
	zkCluster, err := r.getClusterInstance(znode, ctx)
	if err != nil {
		if errors.Is(err, ErrZookeeperCluster) {
			return ctrl.Result{RequeueAfter: time.Millisecond * 10000}, nil
		}
		return ctrl.Result{}, err
	}
	zkSecurity, err := security.NewZookeeperSecurity(zkCluster.Spec.ClusterConfig)
	if err != nil {
		return ctrl.Result{}, err
	}
	// reconcile order by "cluster -> role -> role-group -> resource"
	result, chroot, err := NewZNodeReconciler(r.Scheme, znode, r.Client, zkSecurity).reconcile(ctx, zkCluster)

	//setup finalizer
	if err := r.setupFinalizer(znode, zkCluster, ctx, chroot, int32(zkSecurity.ClientPort())); err != nil {
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
	resourceClient := client.NewClient(r.Client, clusterInstance)
	err := resourceClient.GetWithObject(ctx, clusterInstance)
	if err != nil {
		return nil, ErrZookeeperCluster
	}
	return clusterInstance, nil
}

func (r *ZookeeperZnodeReconciler) setupFinalizer(cr *zkv1alpha1.ZookeeperZnode, zkCluster *zkv1alpha1.ZookeeperCluster,
	ctx context.Context, chroot string, clientPort int32) error {
	finalizers := finalizer.NewFinalizers()
	err := finalizers.Register(ZNodeDeleteFinalizer, ZnodeDeleteFinalizer{Chroot: chroot, ZkCluster: zkCluster, clientPort: clientPort})
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
