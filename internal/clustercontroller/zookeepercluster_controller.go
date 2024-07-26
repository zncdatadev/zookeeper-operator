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

package clustercontroller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

// ZookeeperClusterReconciler reconciles a ZookeeperCluster object
type ZookeeperClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ZookeeperClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling zookeeper cluster instance")

	zookeeper := &zkv1alpha1.ZookeeperCluster{}

	if err := r.Get(ctx, req.NamespacedName, zookeeper); err != nil {
		if client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "unable to fetch ZookeeperCluster")
			return ctrl.Result{}, err
		}
		r.Log.Info("Zookeeper-cluster resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	r.Log.Info("ZookeeperCluster found", "Name", zookeeper.Name)
	// reconcile order by "cluster -> role -> role-group -> resource"
	result, err := NewClusterReconciler(r.Client, r.Scheme, zookeeper).ReconcileCluster(ctx)
	if err != nil {
		return ctrl.Result{}, err
	} else if result.RequeueAfter > 0 {
		return result, nil
	}
	r.Log.Info("Reconcile successfully ", "Name", zookeeper.Name)
	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *ZookeeperClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zkv1alpha1.ZookeeperCluster{}).
		Complete(r)
}
