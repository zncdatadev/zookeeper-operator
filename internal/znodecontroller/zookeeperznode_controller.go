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
	// reconcile order by "cluster -> role -> role-group -> resource"
	result, err := NewZNodeReconciler(r.Scheme, znode, r.Client).reconcile(ctx)
	if err != nil {
		return ctrl.Result{}, err
	} else if result.RequeueAfter > 0 {
		return result, nil
	}
	r.Log.Info("Reconcile successfully ", "Name", znode.Name)
	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *ZookeeperZnodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zkv1alpha1.ZookeeperZnode{}).
		Complete(r)
}
