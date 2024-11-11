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
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/clustercontroller/cluster"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	logger = log.Log.WithName("controller")
)

// ZookeeperClusterReconciler reconciles a ZookeeperCluster object
type ZookeeperClusterReconciler struct {
	ctrlclient.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=zookeeper.zncdata.dev,resources=zookeeperclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ZookeeperClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger.V(1).Info("Reconciling ZookeeperCluster")

	instance := &zkv1alpha1.ZookeeperCluster{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			logger.V(1).Info("ZookeeperCluster not found, may have been deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	logger.V(1).Info("ZookeeperCluster found", "namespace", instance.Namespace, "name", instance.Name)

	resourceClient := &client.Client{
		Client:         r.Client,
		OwnerReference: instance,
	}

	gvk := instance.GetObjectKind().GroupVersionKind()

	clusterReconciler := cluster.NewClusterReconciler(
		resourceClient,
		reconciler.ClusterInfo{
			GVK: &metav1.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
			ClusterName: instance.Name,
		},
		&instance.Spec,
	)

	if err := clusterReconciler.RegisterResources(ctx); err != nil {
		return ctrl.Result{}, err
	}

	if result, err := clusterReconciler.Reconcile(ctx); util.RequeueOrError(result, err) {
		return result, err
	}

	logger.Info("Cluster reconciled")

	if result, err := clusterReconciler.Ready(ctx); util.RequeueOrError(result, err) {
		return result, err
	}

	logger.V(1).Info("Reconcile finished")

	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *ZookeeperClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zkv1alpha1.ZookeeperCluster{}).
		Complete(r)
}
