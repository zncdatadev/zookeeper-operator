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
	"github.com/zncdatadev/operator-go/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

	AdminPort                 = 8080
	NativeMetricsProviderPort = 7000
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

// ClusterInterface implementation

func (z *ZookeeperCluster) GetSpec() *commonsv1alpha1.GenericClusterSpec {
	return z.Spec.ToGenericSpec()
}

// GetStatus returns the cluster status.
func (z *ZookeeperCluster) GetStatus() *commonsv1alpha1.GenericClusterStatus {
	return &z.Status.GenericClusterStatus
}

// SetStatus updates the cluster status.
func (z *ZookeeperCluster) SetStatus(status *commonsv1alpha1.GenericClusterStatus) {
	z.Status.GenericClusterStatus = *status
}

// GetObjectMeta returns the object metadata.
func (z *ZookeeperCluster) GetObjectMeta() *metav1.ObjectMeta {
	return &z.ObjectMeta
}

// GetScheme returns the cached runtime scheme.
func (z *ZookeeperCluster) GetScheme() *runtime.Scheme {
	return cachedScheme
}

// DeepCopyCluster creates a deep copy of the cluster.
func (z *ZookeeperCluster) DeepCopyCluster() common.ClusterInterface {
	return z.DeepCopy()
}

// GetRuntimeObject returns the underlying runtime.Object.
func (z *ZookeeperCluster) GetRuntimeObject() runtime.Object {
	return z
}

// ToGenericSpec adapts ZookeeperClusterSpec to GenericClusterSpec.
func (s *ZookeeperClusterSpec) ToGenericSpec() *commonsv1alpha1.GenericClusterSpec {
	result := &commonsv1alpha1.GenericClusterSpec{
		ClusterOperation: s.ClusterOperationSpec,
	}

	// Image adapter
	if s.Image != nil {
		result.Image = &commonsv1alpha1.ImageSpec{
			Custom:          s.Image.Custom,
			Repo:            s.Image.Repo,
			ProductVersion:  s.Image.ProductVersion,
			KubedoopVersion: s.Image.KubedoopVersion,
		}
	}

	// Roles adapter: servers -> Roles["server"]
	if s.Servers != nil {
		roleSpec := commonsv1alpha1.RoleSpec{
			RoleConfig: s.Servers.RoleConfig,
		}

		// Config adapter: ConfigSpec embeds RoleGroupConfigSpec
		if s.Servers.Config != nil {
			roleSpec.Config = s.Servers.Config.RoleGroupConfigSpec
		}

		// Overrides from embedded OverridesSpec
		if s.Servers.OverridesSpec != nil {
			roleSpec.ConfigOverrides = s.Servers.ConfigOverrides
			roleSpec.EnvOverrides = s.Servers.EnvOverrides
			roleSpec.CliOverrides = s.Servers.CliOverrides
			roleSpec.PodOverrides = s.Servers.PodOverrides
		}

		// JVM args -> CliOverrides
		if s.Servers.JVMArgumentOverrides != nil {
			roleSpec.CliOverrides = append(roleSpec.CliOverrides, s.Servers.JVMArgumentOverrides.Add...)
		}

		// RoleGroups adapter: convert product RoleGroupSpec to generic
		roleGroups := make(map[string]commonsv1alpha1.RoleGroupSpec)
		for name, rg := range s.Servers.RoleGroups {
			adapted := commonsv1alpha1.RoleGroupSpec{}
			if rg.Replicas > 0 {
				r := rg.Replicas
				adapted.Replicas = &r
			}
			if rg.Config != nil {
				adapted.Config = rg.Config.RoleGroupConfigSpec
			}
			if rg.OverridesSpec != nil {
				adapted.ConfigOverrides = rg.ConfigOverrides
				adapted.EnvOverrides = rg.EnvOverrides
				adapted.CliOverrides = rg.CliOverrides
				adapted.PodOverrides = rg.PodOverrides
			}
			roleGroups[name] = adapted
		}
		roleSpec.RoleGroups = roleGroups

		result.Roles = map[string]commonsv1alpha1.RoleSpec{
			"server": roleSpec,
		}
	}

	return result
}

type ZookeeperClusterStatus struct {
	commonsv1alpha1.GenericClusterStatus `json:",inline"`
	// +kubebuilder:validation:Optional
	ClientConnections map[string]string `json:"clientConnections"`
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
	// +default:value={"repo": "quay.io/zncdatadev", "pullPolicy": "IfNotPresent"}
	Image *ImageSpec `json:"image"`
	// +kubebuilder:validation:Optional
	ClusterOperationSpec *commonsv1alpha1.ClusterOperationSpec `json:"clusterOperation,omitempty"`
	// +kubebuilder:validation:Optional
	// +default:value={"listenerClass": "cluster-internal"}
	ClusterConfig *ClusterConfigSpec `json:"clusterConfig,omitempty"`
	// +kubebuilder:validation:Required
	Servers *ServerSpec `json:"servers"`
}

type ClusterConfigSpec struct {
	// Which type of service to use for the Zookeeper cluster.
	//  - cluster-internal: use ClusterIP service
	//  - external-unstable: use NodePort service
	// +kubebuilder:validation:optional
	// +kubebuilder:validation:Enum="cluster-internal";"external-unstable"
	// +kubebuilder:default="cluster-internal"
	ListenerClass ListenerClass `json:"listenerClass"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=1
	MinServerId int32 `json:"minServerId,omitempty"`

	// +kubebuilder:validation:Optional
	// +default:value=[]
	Authentication []AuthenticationSpec `json:"authentication,omitempty"`

	// +kubebuilder:validation:Optional
	// +default:value={"quorumSecretClass": "tls", "serverSecretClass": "tls"}
	Tls *ZookeeperTls `json:"tls,omitempty"`

	// Name of the Vector aggregator [discovery ConfigMap].
	// It must contain the key `ADDRESS` with the address of the Vector aggregator.
	// Follow the [logging tutorial](DOCS_BASE_URL_PLACEHOLDER/tutorials/logging-vector-aggregator)
	// to learn how to configure log aggregation with Vector.

	// +kubebuilder:validation:Optional
	VectorAggregatorConfigMapName *string `json:"vectorAggregatorConfigMapName,omitempty"`
}

type AuthenticationSpec struct {
	// Only affects client connections. This setting controls:
	// - If clients need to authenticate themselves against the server via TLS
	// - Which ca.crt to use when validating the provided client certs
	//
	// This will override the server TLS settings (if set) in `spec.clusterConfig.tls.serverSecretClass`.
	// +kubebuilder:validation:Required
	AuthenticationClass string `json:"authenticationClass"`
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
}

type ServerSpec struct {
	*commonsv1alpha1.OverridesSpec `json:",inline"`

	// +kubebuilder:validation:Optional
	// +default:value={}
	Config *ConfigSpec `json:"config,omitempty"`

	// +kubebuilder:validation:Optional
	RoleGroups map[string]RoleGroupSpec `json:"roleGroups,omitempty"`

	// +kubebuilder:validation:Optional
	RoleConfig *commonsv1alpha1.RoleConfigSpec `json:"roleConfig,omitempty"`

	// Overrides for the default JVM arguments.
	// +kubebuilder:validation:Optional
	// +default:value={"add": [], "remove": [], "removeRegex": []}
	JVMArgumentOverrides *JVMArgumentOverridesSpec `json:"jvmArgumentOverrides,omitempty"`
}

type JVMArgumentOverridesSpec struct {
	// JVM arguments to add to the default JVM arguments.
	// +kubebuilder:validation:Optional
	Add []string `json:"add,omitempty"`

	// JVM arguments to remove from the default JVM arguments.
	// +kubebuilder:validation:Optional
	Remove []string `json:"remove,omitempty"`

	// Any of regular expressions to match JVM arguments to remove from the default JVM arguments.
	// +kubebuilder:validation:Optional
	RemoveRegex []string `json:"removeRegex,omitempty"`
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
	// +kubebuilder:validation:Minimum=0.0
	MyidOffset int16 `json:"myidOffset,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0.0
	InitLimit int32 `json:"initLimit,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0.0
	SyncLimit int32 `json:"syncLimit,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0.0
	TickTime int32 `json:"tickTime,omitempty"`
}

// cachedScheme is initialized once and reused across all reconcile calls.
var cachedScheme *runtime.Scheme

func init() {
	SchemeBuilder.Register(&ZookeeperCluster{}, &ZookeeperClusterList{})
	cachedScheme = runtime.NewScheme()
	_ = SchemeBuilder.AddToScheme(cachedScheme)
}
