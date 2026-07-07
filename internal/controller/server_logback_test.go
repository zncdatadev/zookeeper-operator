package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/config"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"k8s.io/utils/ptr"
)

// These tests exercise the ZooKeeper logback declaration through the framework logging
// pipeline (reconciler.RenderContainerLogging) exactly as server_configmap.buildConfigMap
// does. The generic conversion/merge is covered by operator-go's own tests; here we lock in
// the ZooKeeper-specific bits: container key, encoder pattern (myid MDC), and the
// framework-derived, Vector-gated rolling file appender ("zookeeper.stdout.log").
var _ = Describe("ServerLogback wiring", func() {
	// render with the Vector agent enabled: the file appender is Vector-coupled, so these tests
	// (which assert on the file appender) enable it.
	render := func(logging *commonsv1alpha1.LoggingSpec) (string, string, error) {
		if logging == nil {
			logging = &commonsv1alpha1.LoggingSpec{}
		}
		logging.EnableVectorAgent = ptr.To(true)
		buildCtx := &reconciler.RoleGroupBuildContext{
			MergedConfig: &config.MergedConfig{Logging: logging},
		}
		return reconciler.RenderContainerLogging(buildCtx, productlogging.ContainerLogging{
			Container: common.ZkServerContainerName,
			Framework: productlogging.LoggingFrameworkLogback,
			Pattern:   "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n",
		})
	}

	serverLogging := func(loggers map[string]*commonsv1alpha1.LogLevelSpec) *commonsv1alpha1.LoggingSpec {
		return &commonsv1alpha1.LoggingSpec{
			Containers: map[string]commonsv1alpha1.LoggingConfigSpec{
				common.ZkServerContainerName: {Loggers: loggers},
			},
		}
	}

	It("renders defaults (root INFO + file appender + myid pattern) when no logging is set", func() {
		file, xml, err := render(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(file).To(Equal("logback.xml"))
		Expect(xml).To(ContainSubstring(`<root level="INFO">`))
		Expect(xml).To(ContainSubstring("<file>/kubedoop/log/zookeeper/zookeeper.log4j.xml</file>"))
		Expect(xml).To(ContainSubstring("[myid:%X{myid}]"))
	})

	It("applies the user's root level and named loggers", func() {
		_, xml, err := render(serverLogging(map[string]*commonsv1alpha1.LogLevelSpec{
			"ROOT":                 {Level: "WARN"},
			"org.apache.zookeeper": {Level: "DEBUG"},
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(xml).To(ContainSubstring(`<root level="WARN">`))
		Expect(xml).To(ContainSubstring(`<logger name="org.apache.zookeeper" level="DEBUG" />`))
	})

	It("applies console/file appender thresholds (now supported by the framework)", func() {
		logging := serverLogging(nil)
		c := logging.Containers[common.ZkServerContainerName]
		c.Console = &commonsv1alpha1.LogLevelSpec{Level: "INFO"}
		c.File = &commonsv1alpha1.LogLevelSpec{Level: "ERROR"}
		logging.Containers[common.ZkServerContainerName] = c

		_, xml, err := render(logging)
		Expect(err).NotTo(HaveOccurred())
		Expect(xml).To(ContainSubstring("ThresholdFilter"))
		Expect(xml).To(ContainSubstring("<level>INFO</level>"))
		Expect(xml).To(ContainSubstring("<level>ERROR</level>"))
	})

	It("omits the file appender (console-only) when the Vector agent is disabled", func() {
		buildCtx := &reconciler.RoleGroupBuildContext{
			MergedConfig: &config.MergedConfig{Logging: &commonsv1alpha1.LoggingSpec{}}, // no EnableVectorAgent
		}
		_, xml, err := reconciler.RenderContainerLogging(buildCtx, productlogging.ContainerLogging{
			Container: common.ZkServerContainerName,
			Framework: productlogging.LoggingFrameworkLogback,
			Pattern:   "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(xml).NotTo(ContainSubstring("zookeeper.stdout.log"))
		Expect(xml).NotTo(ContainSubstring("RollingFileAppender"))
		Expect(xml).To(ContainSubstring("[myid:%X{myid}]")) // console appender still uses the pattern
	})
})
