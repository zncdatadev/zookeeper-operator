package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/operator-go/pkg/sidecar"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ServerLogging", func() {
	Describe("buildVectorConfigMapData", func() {
		It("should return nil when SidecarManager is nil", func() {
			h := &ZkRoleGroupHandler{}
			scheme := runtime.NewScheme()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			cr := &zkv1alpha1.ZookeeperCluster{}
			buildCtx := &reconciler.RoleGroupBuildContext{}

			data, err := h.buildVectorConfigMapData(context.Background(), k8sClient, cr, buildCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(BeNil())
		})

		It("should return error when SidecarManager is set but VectorAggregatorConfigMapName is nil", func() {
			h := &ZkRoleGroupHandler{}
			scheme := runtime.NewScheme()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			cr := &zkv1alpha1.ZookeeperCluster{
				Spec: zkv1alpha1.ZookeeperClusterSpec{
					ClusterConfig: &zkv1alpha1.ClusterConfigSpec{},
				},
			}
			buildCtx := &reconciler.RoleGroupBuildContext{
				SidecarManager: sidecar.NewSidecarManager(),
			}

			data, err := h.buildVectorConfigMapData(context.Background(), k8sClient, cr, buildCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("vectorAggregatorConfigMapName"))
			Expect(data).To(BeNil())
		})

		It("should return error when ClusterConfig is nil", func() {
			h := &ZkRoleGroupHandler{}
			scheme := runtime.NewScheme()
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			cr := &zkv1alpha1.ZookeeperCluster{}
			buildCtx := &reconciler.RoleGroupBuildContext{
				SidecarManager: sidecar.NewSidecarManager(),
			}

			data, err := h.buildVectorConfigMapData(context.Background(), k8sClient, cr, buildCtx)
			Expect(err).To(HaveOccurred())
			Expect(data).To(BeNil())
		})

		It("should return config data when aggregator ConfigMap exists", func() {
			h := &ZkRoleGroupHandler{}
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			aggregatorCMName := "test-vector-aggregator"
			namespace := "test-ns"
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: aggregatorCMName, Namespace: namespace},
					Data:       map[string]string{"ADDRESS": "vector-aggregator.test-ns.svc:9000"},
				},
			).Build()

			cr := &zkv1alpha1.ZookeeperCluster{
				Spec: zkv1alpha1.ZookeeperClusterSpec{
					ClusterConfig: &zkv1alpha1.ClusterConfigSpec{
						VectorAggregatorConfigMapName: &aggregatorCMName,
					},
				},
			}
			buildCtx := &reconciler.RoleGroupBuildContext{
				SidecarManager:  sidecar.NewSidecarManager(),
				ClusterNamespace: namespace,
				ClusterName:      "test-cluster",
				RoleName:         "server",
				RoleGroupName:    "default",
			}

			data, err := h.buildVectorConfigMapData(context.Background(), k8sClient, cr, buildCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).NotTo(BeNil())
			Expect(data).To(HaveKey("vector.yaml"))
			Expect(data["vector.yaml"]).To(ContainSubstring("vector-aggregator.test-ns.svc:9000"))
		})
	})
})
