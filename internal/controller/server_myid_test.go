package controller

import (
	"fmt"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("myid / server list consistency", func() {
	crWith := func(clusterConfig *zkv1alpha1.ClusterConfigSpec) *zkv1alpha1.ZookeeperCluster {
		return &zkv1alpha1.ZookeeperCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-zk", Namespace: "ns"},
			Spec:       zkv1alpha1.ZookeeperClusterSpec{ClusterConfig: clusterConfig},
		}
	}

	Describe("resolveMinServerID", func() {
		It("defaults to 1 when clusterConfig is nil", func() {
			Expect(resolveMinServerID(crWith(nil))).To(Equal(int32(1)))
		})
		It("defaults to 1 when minServerId is unset (zero value)", func() {
			Expect(resolveMinServerID(crWith(&zkv1alpha1.ClusterConfigSpec{}))).To(Equal(int32(1)))
		})
		It("honors an explicit minServerId", func() {
			Expect(resolveMinServerID(crWith(&zkv1alpha1.ClusterConfigSpec{MinServerId: 5}))).To(Equal(int32(5)))
		})
		It("clamps a below-1 minServerId to 1 (ZooKeeper myid must be >= 1; server.0 is invalid)", func() {
			Expect(resolveMinServerID(crWith(&zkv1alpha1.ClusterConfigSpec{MinServerId: 0}))).To(Equal(int32(1)))
			Expect(resolveMinServerID(crWith(&zkv1alpha1.ClusterConfigSpec{MinServerId: -3}))).To(Equal(int32(1)))
		})
	})

	Describe("generateServerList keys vs myid offset", func() {
		h := &ZkRoleGroupHandler{}
		zkSecurity := newTestZookeeperSecurity()

		serverIDs := func(cr *zkv1alpha1.ZookeeperCluster, replicas int32) []int {
			buildCtx := &reconciler.RoleGroupBuildContext{ResourceName: "test-zk-server-default", ClusterNamespace: "ns"}
			servers := h.generateServerList(cr, buildCtx, replicas, zkSecurity)
			ids := make([]int, 0, len(servers))
			for k := range servers {
				var id int
				_, err := fmt.Sscanf(k, "server.%d", &id)
				Expect(err).NotTo(HaveOccurred())
				ids = append(ids, id)
			}
			sort.Ints(ids)
			return ids
		}

		It("numbers server.N from minServerId with the default offset (1)", func() {
			Expect(serverIDs(crWith(nil), 3)).To(Equal([]int{1, 2, 3}))
		})

		It("numbers server.N from a custom minServerId", func() {
			Expect(serverIDs(crWith(&zkv1alpha1.ClusterConfigSpec{MinServerId: 5}), 3)).To(Equal([]int{5, 6, 7}))
		})

		// The core regression guard: the myid the prepare container writes for pod ordinal 0
		// (MYID_OFFSET + 0) must equal the lowest server.N id in zoo.cfg, for every minServerId.
		DescribeTable("myid offset equals the lowest server.N id",
			func(minServerID int32, wantOffset int32) {
				cr := crWith(&zkv1alpha1.ClusterConfigSpec{MinServerId: minServerID})
				prepare := h.buildPrepareContainer("img", resolveMinServerID(cr))
				var offset string
				for _, e := range prepare.Env {
					if e.Name == "MYID_OFFSET" {
						offset = e.Value
					}
				}
				Expect(offset).To(Equal(fmt.Sprintf("%d", wantOffset)), "MYID_OFFSET env")

				ids := serverIDs(cr, 3)
				Expect(int32(ids[0])).To(Equal(wantOffset), "lowest server.N id must equal MYID_OFFSET")
			},
			Entry("default", int32(1), int32(1)),
			Entry("custom 5", int32(5), int32(5)),
			Entry("clamped 0 -> 1", int32(0), int32(1)),
		)
	})
})
