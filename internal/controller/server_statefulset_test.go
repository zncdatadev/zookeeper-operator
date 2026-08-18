package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
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

	Describe("serverConfigDefaults", func() {
		It("declares storage, CPU, memory, anti-affinity and graceful shutdown", func() {
			cfg, err := serverConfigDefaults("test-zk")
			Expect(err).NotTo(HaveOccurred())

			// Storage is declared non-nil so the framework builds the data PVC; the capacity is
			// left to StorageResource.GetCapacity(), which applies the 10Gi default.
			Expect(cfg.Resources.Storage).NotTo(BeNil())
			Expect(cfg.Resources.Storage.Capacity).To(BeNil())
			capacity := cfg.Resources.Storage.GetCapacity()
			Expect(capacity.String()).To(Equal("10Gi"))

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
	})

	Describe("serverHeapEnv", func() {
		It("derives the heap from the folded memory limit (512Mi * 0.8)", func() {
			Expect(serverHeapEnv(&commonsv1alpha1.RoleGroupConfigSpec{
				Resources: &commonsv1alpha1.ResourcesSpec{
					Memory: &commonsv1alpha1.MemoryResource{Limit: ptr.To(resource.MustParse("512Mi"))},
				},
			})).To(Equal("410"))
		})

		It("leaves ZooKeeper on its built-in default when no limit survived the fold", func() {
			Expect(serverHeapEnv(nil)).To(BeEmpty())
			Expect(serverHeapEnv(&commonsv1alpha1.RoleGroupConfigSpec{})).To(BeEmpty())
			Expect(serverHeapEnv(&commonsv1alpha1.RoleGroupConfigSpec{
				Resources: &commonsv1alpha1.ResourcesSpec{Memory: &commonsv1alpha1.MemoryResource{}},
			})).To(BeEmpty())
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
