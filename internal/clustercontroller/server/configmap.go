package server

import (
	"context"
	"fmt"
	"strconv"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/productlogging"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	"golang.org/x/exp/maps"
	"k8s.io/utils/ptr"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

const (
	ConsoleConversionPattern = "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n"
	LogbackConfigFileName    = "logback.xml"
)

func NewConfigMapReconciler(
	ctx context.Context,
	client *client.Client,
	repilicates *int32,
	options *reconciler.RoleGroupInfo,
	overrides *commonsv1alpha1.OverridesSpec,
	roleGroupSpec *commonsv1alpha1.RoleGroupConfigSpec,
	zkSecurity *security.ZookeeperSecurity,
) reconciler.ResourceReconciler[*builder.ConfigMapBuilder] {

	myidOffSet := 1
	var zooCfgOverride, securityPropsOverride map[string]string
	if overrides != nil {
		configOverride := overrides.ConfigOverrides
		if configOverride != nil {
			if zooCfg, ok := configOverride[zkv1alpha1.ZooCfgFileName]; ok {
				zooCfgOverride = zooCfg
			}
			if securityProps, ok := configOverride[zkv1alpha1.SecurityFileName]; ok {
				securityPropsOverride = securityProps
			}
		}
	}

	namespace := client.GetOwnerNamespace()
	cmBuilder := NewConfigMapBuilder(
		ctx,
		options,
		*client,
		namespace,
		*repilicates,
		uint16(myidOffSet),
		zooCfgOverride,
		securityPropsOverride,
		zkSecurity,
		roleGroupSpec.Logging,
	)
	return reconciler.NewGenericResourceReconciler(client, cmBuilder)
}

func NewConfigMapBuilder(
	ctx context.Context,
	roleGroupInfo *reconciler.RoleGroupInfo,
	client client.Client,
	namespace string,
	replicates int32,
	myidOffset uint16,
	zooCfgOverride map[string]string,
	securityPropsOverride map[string]string,
	zkSecurity *security.ZookeeperSecurity,
	loggingSpec *commonsv1alpha1.LoggingSpec,
) *builder.ConfigMapBuilder {
	configGenerator := &ConfigGenerator{
		RoleGroupInfo:         roleGroupInfo,
		namespace:             namespace,
		replicates:            replicates,
		myidOffset:            myidOffset,
		zooCfgOverride:        zooCfgOverride,
		securityPropsOverride: securityPropsOverride,
		zkSecurity:            zkSecurity,
	}
	buider := builder.NewConfigMapBuilder(
		&client,
		common.RoleGroupConfigMapName(roleGroupInfo),
		func(o *builder.Options) {
			o.Labels = roleGroupInfo.GetLabels()
			o.Annotations = roleGroupInfo.GetAnnotations()
		},
	)
	buider.AddData(map[string]string{zkv1alpha1.ZooCfgFileName: configGenerator.createZooCfgData()})
	buider.AddData(map[string]string{zkv1alpha1.SecurityFileName: configGenerator.createSecurityPropertiesData()})
	buider.AddData(map[string]string{LogbackConfigFileName: createLogbackXmlConfig(loggingSpec)})
	data := buider.GetData()
	if IsVectorEnable(loggingSpec) {
		cr := client.GetOwnerReference()
		cluster := cr.(*zkv1alpha1.ZookeeperCluster)
		ExtendConfigMapByVector(ctx, VectorConfigParams{
			Client:        client.GetCtrlClient(),
			ClusterConfig: cluster.Spec.ClusterConfig,
			Namespace:     namespace,
			InstanceName:  cr.GetName(),
			Role:          string(common.Server),
			GroupName:     roleGroupInfo.GetGroupName(),
		}, data)
		buider.SetData(data)
	}
	return buider
}

type ConfigGenerator struct {
	*reconciler.RoleGroupInfo
	namespace             string
	replicates            int32
	myidOffset            uint16
	zooCfgOverride        map[string]string
	securityPropsOverride map[string]string

	zkSecurity *security.ZookeeperSecurity
}

// create zoo.cfg
func (c *ConfigGenerator) createZooCfgData() string {
	var zooCfg = make(map[string]string)
	// default properties
	maps.Copy(zooCfg, map[string]string{
		"admin.serverPort":          strconv.Itoa(zkv1alpha1.AdminPort),
		"4lw.commands.whitelist":    "srvr, mntr, conf, ruok",
		"metricsProvider.className": "org.apache.zookeeper.metrics.prometheus.PrometheusMetricsProvider",
		"metricsProvider.httpPort":  "7000",
	})
	if c.replicates > 1 {
		maps.Copy(zooCfg, c.createZooServers())
	}

	maps.Copy(zooCfg, c.zkSecurity.ConfigSettings())
	zooCfg = c.configOverrides(zooCfg)

	return util.ToProperties(zooCfg)
}

// create security.properties
func (c *ConfigGenerator) createSecurityPropertiesData() string {
	if c.securityPropsOverride == nil {
		return util.ToProperties(common.DefautlSercurityProperties())
	}
	return util.ToProperties(c.securityPropsOverride)
}

func (c *ConfigGenerator) createZooServers() map[string]string {
	var servers = make(map[string]string)
	// range repilicates
	for i := 0; i < int(c.replicates); i++ {
		zkMyId := i + int(c.myidOffset)
		serverKey := fmt.Sprintf("server.%d", zkMyId)
		podName := fmt.Sprintf("%s-%d", common.StatefulsetName(c.RoleGroupInfo), i)
		podFQDN := common.PodFQDN(podName, common.RoleGroupServiceName(c.RoleGroupInfo), c.namespace)
		server := fmt.Sprintf("%s:2888:3888;%d", podFQDN, c.zkSecurity.ClientPort())
		maps.Copy(servers, map[string]string{
			serverKey: server,
		})
	}
	return servers
}

// configOverrides need to go last
func (c *ConfigGenerator) configOverrides(zooCfg map[string]string) map[string]string {
	if c.zooCfgOverride == nil {
		return zooCfg
	}

	maps.Copy(zooCfg, c.zooCfgOverride)
	return zooCfg
}

// create logback.xml
func createLogbackXmlConfig(loggingSpec *commonsv1alpha1.LoggingSpec) string {
	var zkServerLoggingConfigSpec *commonsv1alpha1.LoggingConfigSpec
	if loggingSpec != nil {
		containersLoggingSpec := loggingSpec.Containers
		if zkLoggingConfig, ok := containersLoggingSpec[common.ZkServerContainerName]; ok {
			zkServerLoggingConfigSpec = &zkLoggingConfig
		}
	}
	logfileName := fmt.Sprintf("%s.log4j.xml", common.ZkServerContainerName)
	opts := func(opt *productlogging.ConfigGeneratorOption) {
		opt.ConsoleHandlerFormatter = ptr.To(ConsoleConversionPattern)
	}
	logbackGenerator, _ := productlogging.NewConfigGenerator(
		zkServerLoggingConfigSpec,
		common.ZkServerContainerName,
		logfileName,
		productlogging.LogTypeLogback, opts,
	)
	xml, err := logbackGenerator.Content()
	if err != nil {
		panic(err)
	}
	return xml
}
