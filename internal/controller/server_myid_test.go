package controller

import (
	"fmt"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// crWithGroups builds a ZookeeperCluster with the given server role groups (name -> replicas) and
// an optional minServerId (0 means "leave clusterConfig unset").
func crWithGroups(minServerID int32, groups map[string]int32) *zkv1alpha1.ZookeeperCluster {
	rgs := make(map[string]zkv1alpha1.RoleGroupSpec, len(groups))
	for name, replicas := range groups {
		rgs[name] = zkv1alpha1.RoleGroupSpec{Replicas: replicas}
	}
	cr := &zkv1alpha1.ZookeeperCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-zk", Namespace: "ns"},
		Spec:       zkv1alpha1.ZookeeperClusterSpec{Servers: &zkv1alpha1.ServerSpec{RoleGroups: rgs}},
	}
	if minServerID != 0 {
		cr.Spec.ClusterConfig = &zkv1alpha1.ClusterConfigSpec{MinServerId: minServerID}
	}
	return cr
}

// serverIDs returns the sorted server.N ids from a generated server list.
func serverIDs(servers map[string]string) []int {
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

var _ = Describe("myid / server list consistency", func() {
	h := &ZkRoleGroupHandler{}
	zkSecurity := newTestZookeeperSecurity()

	Describe("resolveMinServerID", func() {
		clusterCfg := func(cc *zkv1alpha1.ClusterConfigSpec) *zkv1alpha1.ZookeeperCluster {
			return &zkv1alpha1.ZookeeperCluster{Spec: zkv1alpha1.ZookeeperClusterSpec{ClusterConfig: cc}}
		}
		It("defaults to 1 when clusterConfig is nil", func() {
			Expect(resolveMinServerID(clusterCfg(nil))).To(Equal(int32(1)))
		})
		It("defaults to 1 when minServerId is unset (zero value)", func() {
			Expect(resolveMinServerID(clusterCfg(&zkv1alpha1.ClusterConfigSpec{}))).To(Equal(int32(1)))
		})
		It("honors an explicit minServerId", func() {
			Expect(resolveMinServerID(clusterCfg(&zkv1alpha1.ClusterConfigSpec{MinServerId: 5}))).To(Equal(int32(5)))
		})
		It("clamps a below-1 minServerId to 1 (server.0 is an invalid ZooKeeper myid)", func() {
			Expect(resolveMinServerID(clusterCfg(&zkv1alpha1.ClusterConfigSpec{MinServerId: 0}))).To(Equal(int32(1)))
			Expect(resolveMinServerID(clusterCfg(&zkv1alpha1.ClusterConfigSpec{MinServerId: -3}))).To(Equal(int32(1)))
		})
	})

	Describe("single role group", func() {
		It("numbers server.N from minServerId with the default offset (1)", func() {
			cr := crWithGroups(0, map[string]int32{"default": 3})
			Expect(serverIDs(h.generateServerList(cr, zkSecurity))).To(Equal([]int{1, 2, 3}))
		})
		It("numbers server.N from a custom minServerId", func() {
			cr := crWithGroups(5, map[string]int32{"default": 3})
			Expect(serverIDs(h.generateServerList(cr, zkSecurity))).To(Equal([]int{5, 6, 7}))
		})
	})

	Describe("multiple role groups form one ensemble", func() {
		// default(2) + secondary(1), minServerId=1: ranges [1,2] and [3].
		cr := crWithGroups(1, map[string]int32{"default": 2, "secondary": 1})

		It("assigns non-overlapping myid ranges ordered by group name", func() {
			bases := serverGroupBaseIDs(cr)
			Expect(bases["default"]).To(Equal(int32(1)))
			Expect(bases["secondary"]).To(Equal(int32(3)))
		})

		It("spans all groups in a single server.N list with unique ids", func() {
			servers := h.generateServerList(cr, zkSecurity)
			Expect(serverIDs(servers)).To(Equal([]int{1, 2, 3}))
			// Cross-group entries reference each group's own headless Service.
			Expect(servers["server.1"]).To(ContainSubstring("test-zk-server-default-0.test-zk-server-default-headless.ns"))
			Expect(servers["server.3"]).To(ContainSubstring("test-zk-server-secondary-0.test-zk-server-secondary-headless.ns"))
		})

		It("reports the total ensemble size across groups", func() {
			Expect(serverEnsembleSize(cr)).To(Equal(int32(3)))
		})
	})

	// The core regression guard: for each group, the myid the prepare container writes for ordinal
	// 0 (its base) must equal the lowest server.N id that group contributes to zoo.cfg.
	DescribeTable("prepare-container myid base equals the group's lowest server.N id",
		func(minServerID int32, groups map[string]int32, group string, wantBase int32) {
			cr := crWithGroups(minServerID, groups)
			base := serverGroupBaseIDs(cr)[group]
			Expect(base).To(Equal(wantBase), "group base")

			prepare := h.buildPrepareContainer("img", base)
			var offset string
			for _, e := range prepare.Env {
				if e.Name == "MYID_OFFSET" {
					offset = e.Value
				}
			}
			Expect(offset).To(Equal(fmt.Sprintf("%d", wantBase)), "MYID_OFFSET env")

			// The group's own entries start at its base.
			resourceName := fmt.Sprintf("test-zk-server-%s", group)
			servers := h.generateServerList(cr, zkSecurity)
			lowest := int32(1 << 30)
			for k, v := range servers {
				if !strings.Contains(v, resourceName+"-headless") {
					continue
				}
				var id int32
				_, err := fmt.Sscanf(k, "server.%d", &id)
				Expect(err).NotTo(HaveOccurred())
				if id < lowest {
					lowest = id
				}
			}
			Expect(lowest).To(Equal(wantBase), "lowest server.N id for the group")
		},
		Entry("single default group, minServerId 1", int32(1), map[string]int32{"default": 3}, "default", int32(1)),
		Entry("single default group, custom minServerId 5", int32(5), map[string]int32{"default": 3}, "default", int32(5)),
		Entry("multi-group: default base", int32(1), map[string]int32{"default": 2, "secondary": 1}, "default", int32(1)),
		Entry("multi-group: secondary base after default's 2 replicas", int32(1), map[string]int32{"default": 2, "secondary": 1}, "secondary", int32(3)),
	)
})
