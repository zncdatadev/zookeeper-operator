package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ServerStatefulSet", func() {
	Describe("getLivenessProbe", func() {
		It("should return a liveness probe with ruok command on default port", func() {
			h := &ZkRoleGroupHandler{}
			zkSecurity := newTestZookeeperSecurity()
			probe := h.getLivenessProbe(zkSecurity)
			Expect(probe).NotTo(BeNil())
			Expect(probe.Exec).NotTo(BeNil())
			cmd := probe.Exec.Command
			Expect(cmd).To(HaveLen(3))
			Expect(cmd[0]).To(Equal("bash"))
			Expect(cmd[1]).To(Equal("-c"))
			Expect(cmd[2]).To(ContainSubstring("ruok"))
			Expect(cmd[2]).To(ContainSubstring("imok"))
			Expect(cmd[2]).To(ContainSubstring("127.0.0.1/2181"))
		})

		It("should have correct probe timing", func() {
			h := &ZkRoleGroupHandler{}
			zkSecurity := newTestZookeeperSecurity()
			probe := h.getLivenessProbe(zkSecurity)
			Expect(probe.InitialDelaySeconds).To(BeEquivalentTo(10))
			Expect(probe.PeriodSeconds).To(BeEquivalentTo(10))
			Expect(probe.FailureThreshold).To(BeEquivalentTo(3))
			Expect(probe.SuccessThreshold).To(BeEquivalentTo(1))
			Expect(probe.TimeoutSeconds).To(BeEquivalentTo(5))
		})

		It("should use ClientPort from security config", func() {
			zkSecurity := newTestZookeeperSecurity()
			Expect(zkSecurity.ClientPort()).To(BeEquivalentTo(zkv1alpha1.ClientPort))
		})
	})

	Describe("ensureServerConfigDefaults", func() {
		minimalCR := &zkv1alpha1.ZookeeperCluster{}

		It("defaults storage to 10Gi when resources.storage is unset (minimal CR)", func() {
			h := &ZkRoleGroupHandler{}
			// RoleGroupSpec.Config nil → previously produced a dangling data mount with no PVC.
			buildCtx := &reconciler.RoleGroupBuildContext{}
			h.ensureServerConfigDefaults(minimalCR, buildCtx)

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Resources).NotTo(BeNil())
			Expect(cfg.Resources.Storage).NotTo(BeNil())
			Expect(cfg.Resources.Storage.Capacity.String()).To(Equal("10Gi"))
		})

		It("keeps a user-specified storage capacity", func() {
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							Storage: &commonsv1alpha1.StorageResource{Capacity: resource.MustParse("5Gi")},
						},
					},
				},
			}
			h.ensureServerConfigDefaults(minimalCR, buildCtx)
			Expect(buildCtx.RoleGroupSpec.Config.Resources.Storage.Capacity.String()).To(Equal("5Gi"))
		})

		It("defaults CPU, memory, anti-affinity and graceful shutdown for a minimal cluster", func() {
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{}
			h.ensureServerConfigDefaults(cr, buildCtx)

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("100m"))
			Expect(cfg.Resources.CPU.Max.String()).To(Equal("200m"))
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("512Mi"))
			Expect(cfg.GracefulShutdownTimeout).To(Equal("120s"))
			Expect(cfg.Affinity).NotTo(BeNil())
			affinity := &corev1.Affinity{}
			Expect(json.Unmarshal(cfg.Affinity.Raw, affinity)).To(Succeed())
			terms := affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(terms).To(HaveLen(1))
			Expect(terms[0].Weight).To(BeEquivalentTo(70))
			Expect(terms[0].PodAffinityTerm.LabelSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/component", "server"))
			Expect(terms[0].PodAffinityTerm.LabelSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/instance", "test-zk"))
			Expect(terms[0].PodAffinityTerm.TopologyKey).To(Equal(corev1.LabelHostname))
		})

		It("preserves user-set CPU/memory/affinity/graceful-shutdown at the group level", func() {
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							CPU:    &commonsv1alpha1.CPUResource{Min: resource.MustParse("500m"), Max: resource.MustParse("1")},
							Memory: &commonsv1alpha1.MemoryResource{Limit: resource.MustParse("2Gi")},
						},
						GracefulShutdownTimeout: "45s",
					},
				},
			}
			h.ensureServerConfigDefaults(cr, buildCtx)

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("500m"))
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("2Gi"))
			Expect(cfg.GracefulShutdownTimeout).To(Equal("45s"))
		})

		It("drives ZK_SERVER_HEAP from the defaulted memory for a minimal cluster", func() {
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{}
			h.ensureServerConfigDefaults(cr, buildCtx)

			var heap string
			for _, e := range h.getEnvVars(buildCtx.RoleGroupSpec.GetConfig()) {
				if e.Name == "ZK_SERVER_HEAP" {
					heap = e.Value
				}
			}
			// 512Mi * 0.8 = ~410 MiB. Before defaulting memory, no heap env was emitted at all.
			Expect(heap).To(Equal("410"))
		})

		It("applies the 120s product default even when the CRD injected the platform grace at role level", func() {
			// The base-operator-go CRD auto-injects gracefulShutdownTimeout="30s" into
			// servers.config; that platform default must not suppress ZooKeeper's 120s default.
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleSpec: &commonsv1alpha1.RoleSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{GracefulShutdownTimeout: "30s"},
				},
			}
			h.ensureServerConfigDefaults(cr, buildCtx)
			Expect(buildCtx.RoleGroupSpec.Config.GracefulShutdownTimeout).To(Equal("120s"))
		})

		It("honors an explicit non-default graceful shutdown at the role level", func() {
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleSpec: &commonsv1alpha1.RoleSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{GracefulShutdownTimeout: "90s"},
				},
			}
			h.ensureServerConfigDefaults(cr, buildCtx)
			Expect(buildCtx.RoleGroupSpec.Config.GracefulShutdownTimeout).To(Equal("90s"))
		})

		It("folds role-level resources into the group when the group omits them", func() {
			h := &ZkRoleGroupHandler{}
			cr := &zkv1alpha1.ZookeeperCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-zk"}}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleSpec: &commonsv1alpha1.RoleSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							Memory: &commonsv1alpha1.MemoryResource{Limit: resource.MustParse("1Gi")},
						},
						GracefulShutdownTimeout: "90s",
					},
				},
			}
			h.ensureServerConfigDefaults(cr, buildCtx)

			cfg := buildCtx.RoleGroupSpec.Config
			// Role-level values win over the built-in defaults...
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("1Gi"))
			Expect(cfg.GracefulShutdownTimeout).To(Equal("90s"))
			// ...and fields the role omits still fall back to the defaults.
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("100m"))
		})
	})
})

// newTestZookeeperSecurity creates a ZookeeperSecurity for testing (no TLS).
func newTestZookeeperSecurity() *security.ZookeeperSecurity {
	scheme := runtime.NewScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	zkSecurity, err := security.NewZookeeperSecurity(context.Background(), k8sClient, nil)
	Expect(err).NotTo(HaveOccurred())
	return zkSecurity
}
