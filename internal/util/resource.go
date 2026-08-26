package util

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
)

func QuantityToMB(quantity resource.Quantity) float64 {
	return (float64(quantity.Value() / (1024 * 1024)))
}

func JvmJmxOpts(metricsPort int) string {
	jvmOpts := make([]string, 0, 3)
	jmxDir := path.Join(constant.KubedoopRoot, "jmx")
	jmxConfig := fmt.Sprintf("-javaagent:%s=%d:%s", path.Join(jmxDir, "jmx_prometheus_javaagent.jar"), metricsPort, path.Join(jmxDir, "config.yaml"))
	jvmOpts = append(jvmOpts, jmxConfig)

	logbackConfig := fmt.Sprintf("-Dlogback.configurationFile=%s", path.Join(constant.KubedoopConfigDir, "logback.xml"))
	jvmOpts = append(jvmOpts, logbackConfig)

	securityConfig := fmt.Sprintf("-Djava.security.properties=%s", path.Join(constant.KubedoopConfigDir, "security.properties"))
	jvmOpts = append(jvmOpts, securityConfig)
	return strings.Join(jvmOpts, " ")
}

func ToProperties(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString("=")
		buf.WriteString(data[k])
		buf.WriteString("\n")
	}
	return buf.String()
}

func RequeueOrError(res ctrl.Result, err error) bool {
	return !res.IsZero() || err != nil
}
