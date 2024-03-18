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
	"k8s.io/client-go/util/retry"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
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
	}

	r.Log.Info("Successfully reconciled ZookeeperCluster")
	return result, nil
}

// UpdateStatus updates the status of the ZookeeperCluster resource
// https://stackoverflow.com/questions/76388004/k8s-controller-update-status-and-condition
func (r *ZookeeperClusterReconciler) UpdateStatus(ctx context.Context, instance *zkv1alpha1.ZookeeperCluster) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.Status().Update(ctx, instance)
		//return r.Status().Patch(ctx, instance, client.MergeFrom(instance))
	})

	if retryErr != nil {
		r.Log.Error(retryErr, "Failed to update vfm status after retries")
		return retryErr
	}

	r.Log.V(1).Info("Successfully patched object status")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ZookeeperClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zkv1alpha1.ZookeeperCluster{}).
		Complete(r)
}
