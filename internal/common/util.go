package common

import (
	"fmt"
	"strings"
)

const (
	// ZkServerContainerName is the name of the main Zookeeper container. It is kept as
	// "zookeeper" for backward compatibility with the pre-framework layout (the role name
	// is "server" and is used for the app.kubernetes.io/component label, but the container
	// name intentionally stays "zookeeper").
	ZkServerContainerName = "zookeeper"
)

func ClusterServiceName(instanceName string) string {
	return instanceName
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
