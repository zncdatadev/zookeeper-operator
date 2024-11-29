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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=zookeeperznodes,shortName=znode;znodes,singular=zookeeperznode
// +kubebuilder:subresource:status

// ZookeeperZnode is the Schema for the zookeeperznodes API
type ZookeeperZnode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZookeeperZnodeSpec     `json:"spec,omitempty"`
	Status ZookeeperClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ZookeeperZnodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZookeeperZnode `json:"items"`
}

type ZookeeperZnodeSpec struct {
	// +kubebuilder:validation:Required
	ClusterRef *ClusterRefSpec `json:"clusterRef"`
}

type ClusterRefSpec struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace"`
}

func init() {
	SchemeBuilder.Register(&ZookeeperZnode{}, &ZookeeperZnodeList{})
}
