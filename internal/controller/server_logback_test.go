package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/config"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
)

// These tests exercise the ZooKeeper logback declaration through the framework logging
// pipeline (reconciler.RenderContainerLogging) exactly as server_configmap.buildConfigMap
// does. The generic conversion/merge is covered by operator-go's own tests; here we lock in
// the ZooKeeper-specific bits: container key, encoder pattern (myid MDC) and the Vector
// output file.
var _ = Describe("ServerLogback wiring", func() {
	render := func(logging *commonsv1alpha1.LoggingSpec) (string, string, error) {
		buildCtx := &reconciler.RoleGroupBuildContext{
			MergedConfig: &config.MergedConfig{Logging: logging},
		}
		return reconciler.RenderContainerLogging(buildCtx, productlogging.ContainerLogging{
			Container:  common.ZkServerContainerName,
			Framework:  productlogging.LoggingFrameworkLogback,
			Pattern:    "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n",
			OutputFile: "zookeeper.stdout.log",
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
		Expect(xml).To(ContainSubstring("<file>/kubedoop/log/zookeeper.stdout.log</file>"))
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
})
