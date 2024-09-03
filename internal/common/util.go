package common

import (
	"fmt"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
)

const (
	ZkServerContainerName = "zookeeper"
)

func ClusterServiceName(instanceName string) string {
	return instanceName
}

func StatefulsetName(roleGroupInfo *reconciler.RoleGroupInfo) string {
	return roleGroupInfo.GetFullName()
}

func RoleGroupConfigMapName(roleGroupInfo *reconciler.RoleGroupInfo) string {
	return roleGroupInfo.GetFullName()
}

func RoleGroupServiceName(roleGroupInfo *reconciler.RoleGroupInfo) string {
	return roleGroupInfo.GetFullName()
}

func PodFQDN(podName, svcName, namespace string) string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, svcName, namespace)
}

func CreateClientConnectionString(statefulSetName string, replicates, clientPort int32, svcName string, ns string) string {
	var clientCollections []string
	for i := int32(0); i < replicates; i++ {
		podName := fmt.Sprintf("%s-%d", statefulSetName, i)
		podFQDN := PodFQDN(podName, svcName, ns)
		clientCollections = append(clientCollections, fmt.Sprintf("%s:%d", podFQDN, clientPort))
	}
	return strings.Join(clientCollections, ",")
}
