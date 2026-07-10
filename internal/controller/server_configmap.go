package controller

import (
	"fmt"
	"maps"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/constant"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildConfigMap creates the ConfigMap for a server role group.
func (h *ZkRoleGroupHandler) buildConfigMap(
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

	// 3. Framework-owned logging config: logback.xml (from the deep-merged CRD logging spec, with
	// the file appender gated on Vector) and, when Vector is enabled and the CR exposes the
	// aggregator ConfigMap (VectorAggregatorConfigMapName), vector.yaml. h.LoggingContainers is
	// the single declaration that also drives the shared log volume, so config and volume stay in
	// lockstep.
	loggingData, err := reconciler.RenderLoggingConfigMapData(buildCtx, h.LoggingContainers)
	if err != nil {
		return nil, fmt.Errorf("failed to render logging config: %w", err)
	}
	maps.Copy(data, loggingData)

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

	return util.ToProperties(zooCfg)
}

// generateServerList generates the server.N entries for zoo.cfg.
func (h *ZkRoleGroupHandler) generateServerList(
	cr *zkv1alpha1.ZookeeperCluster,
	buildCtx *reconciler.RoleGroupBuildContext,
	replicas int32,
	zkSecurity *security.ZookeeperSecurity,
) map[string]string {
	servers := make(map[string]string)
	minServerID := resolveMinServerID(cr)

	for i := int32(0); i < replicas; i++ {
		zkMyId := i + minServerID
		serverKey := fmt.Sprintf("server.%d", zkMyId)
		podName := fmt.Sprintf("%s-%d", buildCtx.ResourceName, i)
		// The StatefulSet is governed by the framework's headless service, named
		// "<resource>-headless"; pod DNS resolves under that service.
		podFQDN := common.PodFQDN(podName, buildCtx.ResourceName+"-headless", buildCtx.ClusterNamespace)
		server := fmt.Sprintf("%s:2888:3888;%d", podFQDN, zkSecurity.ClientPort())
		servers[serverKey] = server
	}
	return servers
}

// resolveMinServerID returns the myid assigned to the first server (pod ordinal 0) of the
// ensemble. It is the single source of truth for the myid numbering: generateServerList keys the
// zoo.cfg "server.N" entries off it, and buildPrepareContainer writes each pod's myid file as
// resolveMinServerID + ordinal, so the two always line up. ZooKeeper requires myid in [1,255], so
// a value below 1 (e.g. an explicit minServerId: 0, which would otherwise produce the invalid
// server.0) is clamped to 1.
func resolveMinServerID(cr *zkv1alpha1.ZookeeperCluster) int32 {
	if cr.Spec.ClusterConfig != nil && cr.Spec.ClusterConfig.MinServerId > 0 {
		return cr.Spec.ClusterConfig.MinServerId
	}
	return 1
}

// generateSecurityProps generates security.properties content.
func (h *ZkRoleGroupHandler) generateSecurityProps(
	buildCtx *reconciler.RoleGroupBuildContext,
) string {
	// Check for overrides
	if buildCtx.MergedConfig != nil {
		if secOverrides, ok := buildCtx.MergedConfig.ConfigFiles[zkv1alpha1.SecurityFileName]; ok {
			return util.ToProperties(secOverrides)
		}
	}
	// Default security properties
	return util.ToProperties(map[string]string{
		"networkaddress.cache.ttl":          "5",
		"networkaddress.cache.negative.ttl": "0",
	})
}
