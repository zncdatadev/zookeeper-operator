/*
Copyright 2024 zncdatadev.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ZooCfgFileName   = "zoo.cfg"
	SecurityFileName = "security.properties"
	LogbackFileName  = "logback.xml"
	JavaEnvFileName  = "java.env"
)

const (
	MaxZKLogFileSize         = "10Mi"
	ConsoleConversionPattern = "%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n"
)

const (
	ClientPortName     = "client"
	SecurityClientName = "secureClient"
	LeaderPortName     = "leader"
	ElectionPortName   = "election"
	MetricsPortName    = "metrics"

	ClientPort       = 2181
	SecureClientPort = 2282
	LeaderPort       = 2888
	ElectionPort     = 3888
	MetricsPort      = 9505

	AdminPort = 8080
)

// volume name
const (
	DataDirName      = "data"
	LogDirName       = "log"
	ConfigDirName    = "config"
	LogConfigDirName = "log-config"
)

type ListenerClass string

const (
	ClusterInternal  ListenerClass = "cluster-internal"
	ExternalUnstable ListenerClass = "external-unstable"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=zookeeperclusters,scope=Namespaced,shortName=zk;zks,singular=zookeepercluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ZookeeperCluster is the Schema for the zookeeperclusters API
type ZookeeperCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZookeeperClusterSpec   `json:"spec,omitempty"`
	Status ZookeeperClusterStatus `json:"status,omitempty"`
}

type ZookeeperClusterStatus struct {
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Optional
	ClientConnections map[string]string `json:"clientConnections"`
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true

// ZookeeperClusterList contains a list of ZookeeperCluster
type ZookeeperClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZookeeperCluster `json:"items"`
}

// ZookeeperClusterSpec defines the desired state of ZookeeperCluster
type ZookeeperClusterSpec struct {
	// +kubebuilder:validation:Optional
	Image *ImageSpec `json:"image"`
	// +kubebuilder:validation:Optional
	ClusterOperationSpec *commonsv1alpha1.ClusterOperationSpec `json:"clusterOperation,omitempty"`
	// +kubebuilder:validation:Optional
	ClusterConfig *ClusterConfigSpec `json:"clusterConfig,omitempty"`
	// +kubebuilder:validation:Required
	Servers *ServerSpec `json:"servers"`
}

type ClusterConfigSpec struct {
	// +kubebuilder:validation:optional
	// +kubebuilder:validation:Enum="cluster-internal";"external-unstable"
	// +kubebuilder:default="cluster-internal"
	ListenerClass constants.ListenerClass `json:"listenerClass"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="cluster.local"
	ClusterDomain string `json:"clusterDomain,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=1
	MinServerId int32 `json:"minServerId,omitempty"`

	// +kubebuilder:validation:Optional
	Authentication *AuthenticationSpec `json:"authentication,omitempty"`

	// +kubebuilder:validation:Optional
	Tls *ZookeeperTls `json:"tls,omitempty"`

	// Name of the Vector aggregator [discovery ConfigMap].
	// It must contain the key `ADDRESS` with the address of the Vector aggregator.
	// Follow the [logging tutorial](DOCS_BASE_URL_PLACEHOLDER/tutorials/logging-vector-aggregator)
	// to learn how to configure log aggregation with Vector.

	// +kubebuilder:validation:Optional
	VectorAggregatorConfigMapName *string `json:"vectorAggregatorConfigMapName,omitempty"`
}

type AuthenticationSpec struct {
	//
	// ## mTLS
	//
	// Only affects client connections. This setting controls:
	// - If clients need to authenticate themselves against the server via TLS
	// - Which ca.crt to use when validating the provided client certs
	// This will override the server TLS settings (if set) in `spec.clusterConfig.tls.serverSecretClass`.
	AuthenticationClass []string `json:"authenticationClass,omitempty"`
}

// ZookeeperTls defines the tls setting for zookeeper cluster
type ZookeeperTls struct {
	// QuorumSecretClass is the secret class for internal quorum communication.
	// Use mutual verification between Zookeeper Nodes
	// (mandatory). This setting controls:
	// - Which cert the servers should use to authenticate themselves against other servers
	// - Which ca.crt to use when validating the other server
	// Defaults to `tls`

	// +kubebuilder:validation:Required
	// +kubebuilder:default=tls
	QuorumSecretClass string `json:"quorumSecretClass,omitempty"`

	// ServerSecretClass is the secret class for client connections.
	// This setting controls:
	// - If TLS encryption is used at all
	// - Which cert the servers should use to authenticate themselves against the client
	// Defaults to `tls`.

	// +kubebuilder:validation:Optional
	ServerSecretClass string `json:"serverSecretClass,omitempty"`

	// todo: use secret resource
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="changeit"
	SSLStorePassword string `json:"sslStorePassword,omitempty"`
}

type ServerSpec struct {
	// +kubebuilder:validation:Optional
	Config *ConfigSpec `json:"config,omitempty"`

	// +kubebuilder:validation:Optional
	RoleGroups map[string]RoleGroupSpec `json:"roleGroups,omitempty"`

	// +kubebuilder:validation:Optional
	RoleConfig *commonsv1alpha1.RoleConfigSpec `json:"roleConfig,omitempty"`

	*commonsv1alpha1.OverridesSpec `json:",inline"`
}

type RoleGroupSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=1
	Replicas int32 `json:"replicas,omitempty"`

	// +kubebuilder:validation:Optional
	Config *ConfigSpec `json:"config,omitempty"`

	*commonsv1alpha1.OverridesSpec `json:",inline"`
}

type ConfigSpec struct {
	*commonsv1alpha1.RoleGroupConfigSpec `json:",inline"`

	// +kubebuilder:validation:Optional
	ExtraEnv map[string]string `json:"extraEnv,omitempty"`

	// +kubebuilder:validation:Optional
	ExtraSecret map[string]string `json:"extraSecret,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ZookeeperCluster{}, &ZookeeperClusterList{})
}
