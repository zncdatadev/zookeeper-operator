package controller

import (
	"fmt"
	"maps"
	"sort"

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
) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	// 1. Generate zoo.cfg
	data[zkv1alpha1.ZooCfgFileName] = h.generateZooCfg(cr, buildCtx, zkSecurity, secretProvisioner)

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

	// Server list for a multi-node ensemble, spanning every role group (single quorum). A
	// single-member ensemble runs standalone with no server list.
	if serverEnsembleSize(cr) > 1 {
		for k, v := range h.generateServerList(cr, zkSecurity) {
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

// generateServerList generates the zoo.cfg "server.N" entries for the WHOLE ensemble — every pod
// of every server role group — so all role groups form a single quorum (the discovery ConfigMap
// and health check likewise aggregate across groups). Each group is written into every group's
// zoo.cfg identically; a pod's own myid (see buildPrepareContainer) selects its entry.
func (h *ZkRoleGroupHandler) generateServerList(
	cr *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
) map[string]string {
	servers := make(map[string]string)
	if cr.Spec.Servers == nil {
		return servers
	}
	bases := serverGroupBaseIDs(cr)
	for groupName, rg := range cr.Spec.Servers.RoleGroups {
		base := bases[groupName]
		resourceName := reconciler.RoleGroupResourceName(cr.Name, serverRoleName, groupName)
		for i := int32(0); i < groupReplicas(rg); i++ {
			zkMyId := base + i
			podName := fmt.Sprintf("%s-%d", resourceName, i)
			// Each group's pods resolve under that group's own headless Service
			// ("<resource>-headless"), so cross-group entries reference the right service.
			podFQDN := common.PodFQDN(podName, resourceName+"-headless", cr.Namespace)
			servers[fmt.Sprintf("server.%d", zkMyId)] = fmt.Sprintf("%s:2888:3888;%d", podFQDN, zkSecurity.ClientPort())
		}
	}
	return servers
}

// resolveMinServerID returns the myid base of the first server role group (pod ordinal 0 of the
// lowest-numbered group). ZooKeeper requires myid in [1,255], so a value below 1 (e.g. an explicit
// minServerId: 0, which would otherwise produce the invalid server.0) is clamped to 1.
func resolveMinServerID(cr *zkv1alpha1.ZookeeperCluster) int32 {
	if cr.Spec.ClusterConfig != nil && cr.Spec.ClusterConfig.MinServerId > 0 {
		return cr.Spec.ClusterConfig.MinServerId
	}
	return 1
}

// serverGroupBaseIDs assigns each server role group a non-overlapping myid range so every pod has
// a unique myid across the whole ensemble. Groups are ordered by name (deterministic); each
// group's base is minServerId plus the running total of the prior groups' replicas, and is the
// myid of that group's pod ordinal 0. generateServerList and buildPrepareContainer both derive
// their numbering from this, so the zoo.cfg server.N ids and the on-disk myid files always agree.
func serverGroupBaseIDs(cr *zkv1alpha1.ZookeeperCluster) map[string]int32 {
	bases := make(map[string]int32)
	if cr.Spec.Servers == nil {
		return bases
	}
	names := make([]string, 0, len(cr.Spec.Servers.RoleGroups))
	for name := range cr.Spec.Servers.RoleGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	next := resolveMinServerID(cr)
	for _, name := range names {
		bases[name] = next
		next += groupReplicas(cr.Spec.Servers.RoleGroups[name])
	}
	return bases
}

// groupReplicas is a role group's desired replica count (default 1 when unset). It is independent
// of any stopped/paused scaling: the ensemble topology in zoo.cfg must reflect the desired
// members so the quorum config is preserved while the StatefulSet is scaled to zero.
func groupReplicas(rg zkv1alpha1.RoleGroupSpec) int32 {
	if rg.Replicas > 0 {
		return rg.Replicas
	}
	return 1
}

// serverEnsembleSize is the total desired server count across all role groups. A single-member
// ensemble runs standalone (no server.N list), matching ZooKeeper's standalone mode.
func serverEnsembleSize(cr *zkv1alpha1.ZookeeperCluster) int32 {
	var total int32
	if cr.Spec.Servers == nil {
		return 0
	}
	for _, rg := range cr.Spec.Servers.RoleGroups {
		total += groupReplicas(rg)
	}
	return total
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
