package common

import (
	"fmt"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

type ClusterConfiguration interface {
	Pods() []ZKPodRef
}

var _ ClusterConfiguration = &ZkClusterConfiguration{}

type ZkClusterConfiguration struct {
	mergedCfg *zkv1alpha1.RoleGroupSpec
}

// Pods implements ClusterConfiguration.
func (z *ZkClusterConfiguration) Pods() []ZKPodRef {

	return nil
}

type ZKPodRef struct {
	Namespace        string
	PodName          string
	GroupServiceName string
	ZkMyId           int
}

// fqdn
func (z *ZKPodRef) FQDN() string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", z.PodName, z.GroupServiceName, z.Namespace)
}
