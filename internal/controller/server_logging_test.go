package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"k8s.io/utils/ptr"
)

// The Vector log pipeline (shared volume + vector.yaml) is now owned by the operator-go
// framework: it resolves the aggregator address from the CR's VectorAggregatorConfigMapName and
// generates vector.yaml. Here we lock in ZooKeeper's side of that seam — the CR exposes the
// aggregator ConfigMap name from spec.clusterConfig (reconciler.VectorAggregatorProvider).
var _ = Describe("ZookeeperCluster VectorAggregatorConfigMapName", func() {
	It("returns the configured aggregator ConfigMap name", func() {
		name := "test-vector-aggregator"
		cr := &zkv1alpha1.ZookeeperCluster{
			Spec: zkv1alpha1.ZookeeperClusterSpec{
				ClusterConfig: &zkv1alpha1.ClusterConfigSpec{VectorAggregatorConfigMapName: ptr.To(name)},
			},
		}
		Expect(cr.VectorAggregatorConfigMapName()).To(Equal(name))
	})

	It("returns empty when ClusterConfig is nil", func() {
		cr := &zkv1alpha1.ZookeeperCluster{}
		Expect(cr.VectorAggregatorConfigMapName()).To(BeEmpty())
	})

	It("returns empty when the aggregator name is unset", func() {
		cr := &zkv1alpha1.ZookeeperCluster{
			Spec: zkv1alpha1.ZookeeperClusterSpec{ClusterConfig: &zkv1alpha1.ClusterConfigSpec{}},
		}
		Expect(cr.VectorAggregatorConfigMapName()).To(BeEmpty())
	})
})
