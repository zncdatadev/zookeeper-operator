package util

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/constants"
	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
)

func QuantityToMB(quantity resource.Quantity) float64 {
	return (float64(quantity.Value() / (1024 * 1024)))
}

// SERVER_JVMFLAGS:  -javaagent:/stackable/jmx/jmx_prometheus_javaagent.jar=9505:/stackable/jmx/server.yaml -Dlogback.configurationFile=/stackable/log_config/logback.xml -Djava.security.properties=/stackable/config/secur
func JvmJmxOpts(metricsPort int) string {
	// SERVER_JVMFLAGS:  -javaagent:/stackable/jmx/jmx_prometheus_javaagent.jar=9505:/stackable/jmx/server.yaml -Dlogback.configurationFile=/stackable/log_config/logback.xml -Djava.security.properties=/stackable/config/secur
	var jvmOpts = make([]string, 0)
	jmxDir := path.Join(constants.KubedoopRoot, "jmx")
	jmxConfig := fmt.Sprintf("-javaagent:%s=%d:%s", path.Join(jmxDir, "jmx_prometheus_javaagent.jar"), metricsPort, path.Join(jmxDir, "config.yaml"))
	jvmOpts = append(jvmOpts, jmxConfig)

	logbackConfig := fmt.Sprintf("-Dlogback.configurationFile=%s", path.Join(constants.KubedoopConfigDir, "logback.xml"))
	jvmOpts = append(jvmOpts, logbackConfig)

	securityConfig := fmt.Sprintf("-Djava.security.properties=%s", path.Join(constants.KubedoopConfigDir, "security.properties"))
	jvmOpts = append(jvmOpts, securityConfig)
	return strings.Join(jvmOpts, " ")
}

func ToProperties(data map[string]string) string {
	keys := maps.Keys(data)
	sort.Strings(keys)
	var buf bytes.Buffer
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
