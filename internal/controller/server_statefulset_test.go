package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
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
			Expect(probe.ProbeHandler.Exec).NotTo(BeNil())
			cmd := probe.ProbeHandler.Exec.Command
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
})

// newTestZookeeperSecurity creates a ZookeeperSecurity for testing (no TLS).
func newTestZookeeperSecurity() *security.ZookeeperSecurity {
	scheme := runtime.NewScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	zkSecurity, err := security.NewZookeeperSecurity(context.Background(), k8sClient, nil)
	Expect(err).NotTo(HaveOccurred())
	return zkSecurity
}
