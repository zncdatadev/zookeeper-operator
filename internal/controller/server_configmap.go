package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// buildConfigMap creates the ConfigMap for a server role group.
func (h *ZkRoleGroupHandler) buildConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
	labels map[string]string,
	zkSecurity *security.ZookeeperSecurity,
	secretProvisioner *opgosecurity.SecretProvisioner,
	replicas int32,
) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	// 1. Generate zoo.cfg
	data[zkv1alpha1.ZooCfgFileName] = h.generateZooCfg(cr, buildCtx, zkSecurity, secretProvisioner, replicas)

	// 2. Generate security.properties
	data[zkv1alpha1.SecurityFileName] = h.generateSecurityProps(buildCtx)

	// 3. Generate logback.xml
	data["logback.xml"] = generateLogbackXml()

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildCtx.ResourceName,
			Namespace: buildCtx.ClusterNamespace,
			Labels:    labels,
		},
		Data: data,
	}, nil
}

// generateZooCfg generates the zoo.cfg content.
func (h *ZkRoleGroupHandler) generateZooCfg(
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
	zkSecurity *security.ZookeeperSecurity,
	secretProvisioner *opgosecurity.SecretProvisioner,
	replicas int32,
) string {
	zooCfg := make(map[string]string)

	// Default properties
	zooCfg["admin.serverPort"] = fmt.Sprintf("%d", zkv1alpha1.AdminPort)
	zooCfg["4lw.commands.whitelist"] = "srvr, mntr, conf, ruok"
	zooCfg["metricsProvider.className"] = "org.apache.zookeeper.metrics.prometheus.PrometheusMetricsProvider"
	zooCfg["metricsProvider.httpPort"] = fmt.Sprintf("%d", zkv1alpha1.NativeMetricsProviderPort)

	// Default server config
	zooCfg["initLimit"] = "5"
	zooCfg["syncLimit"] = "2"
	zooCfg["tickTime"] = "3000"
	zooCfg["dataDir"] = constant.KubedoopDataDir

	// Server list for multi-replica
	if replicas > 1 {
		for k, v := range h.generateServerList(cr, buildCtx, replicas, zkSecurity) {
			zooCfg[k] = v
		}
	}

	// Security config (TLS)
	for k, v := range zkSecurity.ConfigSettings(secretProvisioner) {
		zooCfg[k] = v
	}

	// Apply config overrides from merged config
	if buildCtx.MergedConfig != nil {
		if zooOverrides, ok := buildCtx.MergedConfig.ConfigFiles[zkv1alpha1.ZooCfgFileName]; ok {
			for k, v := range zooOverrides {
				zooCfg[k] = v
			}
		}
	}

	return toProperties(zooCfg)
}

// generateServerList generates the server.N entries for zoo.cfg.
func (h *ZkRoleGroupHandler) generateServerList(
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
	replicas int32,
	zkSecurity *security.ZookeeperSecurity,
) map[string]string {
	servers := make(map[string]string)
	minServerId := int32(1)
	if cr.Spec.ClusterConfig != nil {
		minServerId = cr.Spec.ClusterConfig.MinServerId
	}

	for i := int32(0); i < replicas; i++ {
		zkMyId := i + minServerId
		serverKey := fmt.Sprintf("server.%d", zkMyId)
		podName := fmt.Sprintf("%s-%d", buildCtx.ResourceName, i)
		podFQDN := common.PodFQDN(podName, buildCtx.ResourceName, buildCtx.ClusterNamespace)
		server := fmt.Sprintf("%s:2888:3888;%d", podFQDN, zkSecurity.ClientPort())
		servers[serverKey] = server
	}
	return servers
}

// generateSecurityProps generates security.properties content.
func (h *ZkRoleGroupHandler) generateSecurityProps(
	buildCtx *reconciler.RoleGroupBuildContext,
) string {
	// Check for overrides
	if buildCtx.MergedConfig != nil {
		if secOverrides, ok := buildCtx.MergedConfig.ConfigFiles[zkv1alpha1.SecurityFileName]; ok {
			return toProperties(secOverrides)
		}
	}
	// Default security properties
	return toProperties(map[string]string{
		"networkaddress.cache.ttl":          "5",
		"networkaddress.cache.negative.ttl": "0",
	})
}

// generateLogbackXml generates a simple logback.xml configuration.
func generateLogbackXml() string {
	consolePattern := "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n"
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<configuration>
  <appender name="CONSOLE" class="ch.qos.logback.core.ConsoleAppender">
    <encoder>
      <pattern>%s</pattern>
    </encoder>
  </appender>
  <root level="INFO">
    <appender-ref ref="CONSOLE"/>
  </root>
</configuration>`, consolePattern)
}

// toProperties converts a map to Java properties format.
func toProperties(data map[string]string) string {
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
