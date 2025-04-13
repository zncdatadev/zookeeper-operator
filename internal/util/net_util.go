package util

import "fmt"

func CreateDnsAccess(podName string, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", podName, namespace)
}
