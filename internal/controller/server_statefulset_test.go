package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"k8s.io/apimachinery/pkg/api/resource"
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

	Describe("ensureStorageDefault", func() {
		It("defaults storage to 10Gi when resources.storage is unset (minimal CR)", func() {
			h := &ZkRoleGroupHandler{}
			// RoleGroupSpec.Config nil → previously produced a dangling data mount with no PVC.
			buildCtx := &reconciler.RoleGroupBuildContext{}
			h.ensureStorageDefault(buildCtx)

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
			h.ensureStorageDefault(buildCtx)
			Expect(buildCtx.RoleGroupSpec.Config.Resources.Storage.Capacity.String()).To(Equal("5Gi"))
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
