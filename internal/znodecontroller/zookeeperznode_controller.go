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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
)

// ZookeeperZnodeReconciler reconciles a ZookeeperZnode object
type ZookeeperZnodeReconciler struct {
	ctrlclient.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperznodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperznodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperznodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch

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
		if apierrors.IsNotFound(err) {
			// The referenced ZookeeperCluster does not exist. If the znode is being deleted, there
			// is no ensemble left to remove the znode from, so drop our finalizer and let the
			// object be deleted — otherwise it would requeue forever, stuck behind a finalizer that
			// can never reach ZooKeeper. If the cluster simply has not been created yet, wait.
			if !znode.DeletionTimestamp.IsZero() {
				return ctrl.Result{}, r.clearDeleteFinalizer(ctx, znode)
			}
			r.Log.Info("referenced ZookeeperCluster not found; waiting for it to appear",
				"clusterRef", znode.Spec.ClusterRef)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	zkSecurity, err := security.NewZookeeperSecurity(ctx, r.Client, zkCluster.Spec.ClusterConfig)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile znode
	result, chroot, err := NewZNodeReconciler(r.Scheme, znode, r.Client, zkSecurity).reconcile(ctx, zkCluster)

	// Setup finalizer
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

func (r *ZookeeperZnodeReconciler) getClusterInstance(znode *zkv1alpha1.ZookeeperZnode, ctx context.Context) (*zkv1alpha1.ZookeeperCluster, error) {
	clusterRef := znode.Spec.ClusterRef
	if clusterRef == nil {
		return nil, fmt.Errorf("clusterRef is nil")
	}
	namespace := clusterRef.Namespace
	if namespace == "" {
		namespace = znode.Namespace
	}

	clusterInstance := &zkv1alpha1.ZookeeperCluster{}
	key := ctrlclient.ObjectKey{Namespace: namespace, Name: clusterRef.Name}
	if err := r.Get(ctx, key, clusterInstance); err != nil {
		// Preserve the original error so the caller can distinguish "not found" (cluster gone or
		// not yet created) from a transient API error.
		return nil, err
	}
	return clusterInstance, nil
}

// clearDeleteFinalizer removes the znode delete finalizer without contacting ZooKeeper. Used when
// the referenced ZookeeperCluster no longer exists: the ensemble (and the znode within it) is
// already gone, so there is nothing to delete and the object must not stay stuck behind a
// finalizer that can never complete.
func (r *ZookeeperZnodeReconciler) clearDeleteFinalizer(ctx context.Context, znode *zkv1alpha1.ZookeeperZnode) error {
	if controllerutil.RemoveFinalizer(znode, ZNodeDeleteFinalizer) {
		if err := r.Update(ctx, znode); err != nil {
			return fmt.Errorf("failed to remove znode finalizer after cluster deletion: %w", err)
		}
		r.Log.Info("removed znode finalizer; referenced ZookeeperCluster is gone", "name", znode.Name)
	}
	return nil
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
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
