package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ClusterServiceExtension", func() {
	newScheme := func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(zkv1alpha1.AddToScheme(scheme)).To(Succeed())
		return scheme
	}

	It("creates a cluster Service named after the cluster with a cross-role-group selector", func() {
		scheme := newScheme()
		cr := &zkv1alpha1.ZookeeperCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "zk", Namespace: "ns"},
		}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

		ext := NewClusterServiceExtension(scheme)
		Expect(ext.PreReconcile(context.Background(), k8sClient, cr)).To(Succeed())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "zk", Namespace: "ns"}, svc)).To(Succeed())

		// Selector uses the product-owned identity labels (cluster + role) so it matches all
		// server pods of this cluster without selecting another product's "server" pods.
		Expect(svc.Spec.Selector).To(HaveKeyWithValue("zookeeper.kubedoop.dev/cluster", "zk"))
		Expect(svc.Spec.Selector).To(HaveKeyWithValue("zookeeper.kubedoop.dev/role", "server"))
		// Default listenerClass → ClusterIP, with the client port exposed.
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Name).To(Equal(zkv1alpha1.ClientPortName))
		// Owned by the cluster so it is garbage-collected with it.
		Expect(svc.OwnerReferences).NotTo(BeEmpty())
	})

	It("uses a NodePort Service for external-unstable listenerClass (required by discovery)", func() {
		scheme := newScheme()
		cr := &zkv1alpha1.ZookeeperCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "zk", Namespace: "ns"},
			Spec: zkv1alpha1.ZookeeperClusterSpec{
				ClusterConfig: &zkv1alpha1.ClusterConfigSpec{
					ListenerClass: zkv1alpha1.ExternalUnstable,
				},
			},
		}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

		ext := NewClusterServiceExtension(scheme)
		Expect(ext.PreReconcile(context.Background(), k8sClient, cr)).To(Succeed())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "zk", Namespace: "ns"}, svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
	})

	It("is idempotent across repeated reconciles", func() {
		scheme := newScheme()
		cr := &zkv1alpha1.ZookeeperCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "zk", Namespace: "ns"},
		}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

		ext := NewClusterServiceExtension(scheme)
		Expect(ext.PreReconcile(context.Background(), k8sClient, cr)).To(Succeed())
		Expect(ext.PreReconcile(context.Background(), k8sClient, cr)).To(Succeed())

		svcList := &corev1.ServiceList{}
		Expect(k8sClient.List(context.Background(), svcList, client.InNamespace("ns"))).To(Succeed())
		Expect(svcList.Items).To(HaveLen(1))
	})
})
