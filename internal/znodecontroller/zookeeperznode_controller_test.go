package znodecontroller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

// TestReconcileDeletingZnodeWithMissingClusterClearsFinalizer covers the case where a
// ZookeeperZnode is being deleted but its referenced ZookeeperCluster is already gone (e.g. the
// cluster was deleted first). The reconciler must drop the delete finalizer so the object can be
// removed, instead of requeueing forever behind a finalizer that can never reach ZooKeeper.
func TestReconcileDeletingZnodeWithMissingClusterClearsFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := zkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	now := metav1.Now()
	znode := &zkv1alpha1.ZookeeperZnode{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "zn",
			Namespace:         "ns",
			Finalizers:        []string{ZNodeDeleteFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: zkv1alpha1.ZookeeperZnodeSpec{
			ClusterRef: &zkv1alpha1.ClusterRefSpec{Name: "missing", Namespace: "ns"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(znode).Build()
	r := &ZookeeperZnodeReconciler{Client: c, Scheme: scheme, Log: logr.Discard()}

	key := ctrlclient.ObjectKey{Name: "zn", Namespace: "ns"}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Errorf("expected no requeue once the finalizer is cleared, got RequeueAfter=%v", res.RequeueAfter)
	}

	// Removing the last finalizer on a deleting object lets the fake client delete it, so the
	// znode should now be gone — proof it is no longer stuck.
	got := &zkv1alpha1.ZookeeperZnode{}
	if err := c.Get(context.Background(), key, got); !apierrors.IsNotFound(err) {
		t.Errorf("expected znode to be deleted after finalizer removal, got err=%v, finalizers=%v", err, got.Finalizers)
	}
}
