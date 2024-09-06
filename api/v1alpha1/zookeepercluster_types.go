/*
Copyright 2024.

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
	corev1 "k8s.io/api/core/v1"
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
// +operator-sdk:csv:customresourcedefinitions:displayName="Zookeeper Cluster"
// This annotation provides a hint for OLM which resources are managed by ZookeeperCluster kind.
// It's not mandatory to list all resources.
// https://sdk.operatorframework.io/docs/olm-integration/generation/#csv-fields
// https://sdk.operatorframework.io/docs/building-operators/golang/references/markers/
// +operator-sdk:csv:customresourcedefinitions:resources={{Deployment,app/v1},{Service,v1},{Pod,v1},{ConfigMap,v1},{PersistentVolumeClaim,v1},{PersistentVolume,v1},{PodDisruptionBudget,v1}}

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

//+kubebuilder:object:root=true

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
	Server *ServerSpec `json:"server"`
}

type ClusterConfigSpec struct {
	// +kubebuilder:validation:optional
	// +kubebuilder:validation:Enum="cluster-internal";"external-unstable"
	// +kubebuilder:default="cluster-internal"
	ListenerClass string `json:"listenerClass"`

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
	PodDisruptionBudget *commonsv1alpha1.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`

	// +kubebuilder:validation:Optional
	CommandOverrides []string `json:"commandOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	ConfigOverrides *ConfigOverridesSpec `json:"configOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	EnvOverrides map[string]string `json:"envOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	PodOverrides *corev1.PodTemplateSpec `json:"podOverrides,omitempty"`
}

type RoleGroupSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=1
	Replicas int32 `json:"replicas,omitempty"`

	// +kubebuilder:validation:Required
	Config *ConfigSpec `json:"config,omitempty"`

	// +kubebuilder:validation:Optional
	PodDisruptionBudget *commonsv1alpha1.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`

	// +kubebuilder:validation:Optional
	CommandOverrides []string `json:"commandOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	ConfigOverrides *ConfigOverridesSpec `json:"configOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	EnvOverrides map[string]string `json:"envOverrides,omitempty"`

	// +kubebuilder:validation:Optional
	PodOverrides *corev1.PodTemplateSpec `json:"podOverrides,omitempty"`
}

type ConfigSpec struct {
	// +kubebuilder:validation:Optional
	Resources *commonsv1alpha1.ResourcesSpec `json:"resources,omitempty"`

	// +kubebuilder:validation:Optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext"`

	// +kubebuilder:validation:Optional
	Affinity *corev1.Affinity `json:"affinity"`

	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations"`

	// +kubebuilder:validation:Optional
	PodDisruptionBudget *PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`

	// Use time.ParseDuration to parse the string
	// +kubebuilder:validation:Optional
	GracefulShutdownTimeout *string `json:"gracefulShutdownTimeout,omitempty"`

	// +kubebuilder:validation:Optional
	ExtraEnv map[string]string `json:"extraEnv,omitempty"`

	// +kubebuilder:validation:Optional
	ExtraSecret map[string]string `json:"extraSecret,omitempty"`

	// +kubebuilder:validation:Optional
	Logging *ContainerLoggingSpec `json:"logging,omitempty"`
}

type ContainerLoggingSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EnableVectorAgent bool `json:"enableVectorAgent,omitempty"`
	// +kubebuilder:validation:Optional
	Zookeeper *commonsv1alpha1.LoggingConfigSpec `json:"zookeeperCluster,omitempty"`
}

type ConfigOverridesSpec struct {
	ZooCfg         map[string]string `json:"zoo.cfg,omitempty"`
	SercurityProps map[string]string `json:"security.properties,omitempty"`
}

type PodDisruptionBudgetSpec struct {
	// +kubebuilder:validation:Optional
	MinAvailable int32 `json:"minAvailable,omitempty"`

	// +kubebuilder:validation:Optional
	MaxUnavailable int32 `json:"maxUnavailable,omitempty"`
}

type ServiceSpec struct {
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:enum=ClusterIP;NodePort;LoadBalancer;ExternalName
	// +kubebuilder:default=ClusterIP
	Type corev1.ServiceType `json:"type,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=2181
	Port int32 `json:"port,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ZookeeperCluster{}, &ZookeeperClusterList{})
}
