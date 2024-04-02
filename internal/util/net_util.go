package util

import "fmt"

func CreateDnsAccess(podName string, namespace string, clusterDomain string) string {
	return fmt.Sprintf("%s.%s.svc.%s", podName, namespace, clusterDomain)
}
