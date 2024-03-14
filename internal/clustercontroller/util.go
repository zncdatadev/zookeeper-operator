package clustercontroller

import (
	"fmt"
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"github.com/zncdata-labs/zookeeper-operator/internal/common"
)

// "ZOO_SERVERS":
// pattern: {instanceName}-{index}.{svc-headless}.{namespace}.svc.{clusterDomain}:{followerPort}:{electionPort}::{index+minId}
//
//	zk0391-3-zookeeper-0.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::1,
//	zk0391-3-zookeeper-1.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::2,
//	zk0391-3-zookeeper-2.zk0391-3-zookeeper-headless.default.svc.cluster.local:2888:3888::3
func createZooServerNetworkName(instanceName string, replicates int32, minServerId int32, svcName string, ns string,
	clusterDomain string) string {
	var zooServers string
	for i := int32(0); i < replicates; i++ {
		zooServers += fmt.Sprintf("%s-%d.%s.%s.svc.%s:%d:%d::%d,", instanceName, i, svcName, ns, clusterDomain,
			zkv1alpha1.FollowerPort, zkv1alpha1.ElectionPort, i+minServerId)
	}
	return zooServers
}

func createHeadlessServiceName(instanceName string, groupName string) string {
	return common.NewResourceNameGeneratorOneRole(instanceName, groupName).GenerateResourceName("headless")
}

func createClusterConfigName(instanceName string) string {
	return fmt.Sprintf("%s-config", instanceName)
}

func createScriptConfigName(instanceName string, groupName string) string {
	return common.NewResourceNameGeneratorOneRole(instanceName, groupName).GenerateResourceName("script")
}

func createServiceAccountName(instanceName string, groupName string) string {
	return common.NewResourceNameGeneratorOneRole(instanceName, groupName).GenerateResourceName("sa")
}

func createStatefulSetName(instanceName string, groupName string) string {
	return common.NewResourceNameGeneratorOneRole(instanceName, groupName).GenerateResourceName("")
}

func createDataPvcName(instanceName string, groupName string) string {
	return common.NewResourceNameGeneratorOneRole(instanceName, groupName).GenerateResourceName("data")
}
