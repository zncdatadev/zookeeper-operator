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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
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

	Describe("getStartupProbe", func() {
		It("guards a slow first start with a generous budget so liveness cannot trip early", func() {
			h := &ZkRoleGroupHandler{}
			zkSecurity := newTestZookeeperSecurity()
			probe := h.getStartupProbe(zkSecurity)
			Expect(probe).NotTo(BeNil())
			Expect(probe.Exec).NotTo(BeNil())
			Expect(probe.Exec.Command[2]).To(ContainSubstring("ruok"))
			Expect(probe.Exec.Command[2]).To(ContainSubstring("imok"))
			// FailureThreshold*PeriodSeconds must comfortably exceed ZooKeeper's ~25s bind time so a
			// slow start under a constrained CPU limit is not killed into a CrashLoop.
			Expect(probe.PeriodSeconds).To(BeEquivalentTo(10))
			Expect(probe.FailureThreshold).To(BeEquivalentTo(30))
			Expect(int(probe.PeriodSeconds * probe.FailureThreshold)).To(BeNumerically(">=", 120))
		})
	})

	Describe("ensureServerConfigDefaults", func() {
		It("ensures a data PVC exists (non-nil storage) and defers its capacity to the framework", func() {
			h := &ZkRoleGroupHandler{}
			// RoleGroupSpec.Config nil → previously produced a dangling data mount with no PVC.
			buildCtx := &reconciler.RoleGroupBuildContext{}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Resources).NotTo(BeNil())
			// Storage must be non-nil so the framework builds a data PVC; its capacity is left unset
			// and the framework's GetCapacity() applies DefaultStorageCapacity (10Gi).
			Expect(cfg.Resources.Storage).NotTo(BeNil())
			Expect(cfg.Resources.Storage.Capacity).To(BeNil())
			capacity := cfg.Resources.Storage.GetCapacity()
			Expect(capacity.String()).To(Equal("10Gi"))
		})

		It("keeps a user-specified storage capacity", func() {
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							Storage: &commonsv1alpha1.StorageResource{Capacity: ptr.To(resource.MustParse("5Gi"))},
						},
					},
				},
			}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())
			Expect(buildCtx.RoleGroupSpec.Config.Resources.Storage.Capacity.String()).To(Equal("5Gi"))
		})

		It("defaults CPU, memory, anti-affinity and graceful shutdown for a minimal cluster", func() {
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{ClusterName: "test-zk", RoleName: serverRoleName}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("100m"))
			Expect(cfg.Resources.CPU.Max.String()).To(Equal("200m"))
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("512Mi"))
			Expect(*cfg.GracefulShutdownTimeout).To(Equal("120s"))
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
			buildCtx := &reconciler.RoleGroupBuildContext{
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							CPU:    &commonsv1alpha1.CPUResource{Min: ptr.To(resource.MustParse("500m")), Max: ptr.To(resource.MustParse("1"))},
							Memory: &commonsv1alpha1.MemoryResource{Limit: ptr.To(resource.MustParse("2Gi"))},
						},
						GracefulShutdownTimeout: ptr.To("45s"),
					},
				},
			}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())

			cfg := buildCtx.RoleGroupSpec.Config
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("500m"))
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("2Gi"))
			Expect(*cfg.GracefulShutdownTimeout).To(Equal("45s"))
		})

		It("drives ZK_SERVER_HEAP from the defaulted memory for a minimal cluster", func() {
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())

			var heap string
			for _, e := range h.getEnvVars(buildCtx.RoleGroupSpec.GetConfig()) {
				if e.Name == "ZK_SERVER_HEAP" {
					heap = e.Value
				}
			}
			// 512Mi * 0.8 = ~410 MiB. Before defaulting memory, no heap env was emitted at all.
			Expect(heap).To(Equal("410"))
		})

		It("applies the 120s product default only when graceful shutdown is unset (nil)", func() {
			// gracefulShutdownTimeout is a *string, so "unset" is a nil pointer and the CRD does not
			// default it. A minimal cluster leaves it nil at every level, so ZooKeeper's longer 120s
			// product default applies.
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{ClusterName: "test-zk", RoleName: serverRoleName}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())
			Expect(*buildCtx.RoleGroupSpec.Config.GracefulShutdownTimeout).To(Equal("120s"))
		})

		// The next two cases feed the config through RoleGroupSpec.Config because that is what a
		// handler is handed: GenericReconciler folds the role's config beneath the role group's
		// (MergeRoleGroupConfig) before building the context, so a value the user wrote at either
		// level arrives here already merged. These therefore cover the role level too.
		It("honors an explicit graceful shutdown equal to the platform default", func() {
			// An explicit "30s" used to be indistinguishable from the CRD's auto-injected default and
			// was overridden to 120s. With nil meaning "unset", it is a real user choice and stands.
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{
				ClusterName: "test-zk", RoleName: serverRoleName,
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{GracefulShutdownTimeout: ptr.To("30s")},
				},
			}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())
			Expect(*buildCtx.RoleGroupSpec.Config.GracefulShutdownTimeout).To(Equal("30s"))
		})

		It("keeps a merged-in value and still defaults the fields it does not cover", func() {
			h := &ZkRoleGroupHandler{}
			buildCtx := &reconciler.RoleGroupBuildContext{
				ClusterName: "test-zk", RoleName: serverRoleName,
				RoleGroupSpec: commonsv1alpha1.RoleGroupSpec{
					Config: &commonsv1alpha1.RoleGroupConfigSpec{
						Resources: &commonsv1alpha1.ResourcesSpec{
							Memory: &commonsv1alpha1.MemoryResource{Limit: ptr.To(resource.MustParse("1Gi"))},
						},
						GracefulShutdownTimeout: ptr.To("90s"),
					},
				},
			}
			Expect(h.ensureServerConfigDefaults(buildCtx)).To(Succeed())

			cfg := buildCtx.RoleGroupSpec.Config
			// What the user stated survives...
			Expect(cfg.Resources.Memory.Limit.String()).To(Equal("1Gi"))
			Expect(*cfg.GracefulShutdownTimeout).To(Equal("90s"))
			// ...and what nobody stated still gets the product default.
			Expect(cfg.Resources.CPU.Min.String()).To(Equal("100m"))
			Expect(cfg.Affinity).NotTo(BeNil())
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
