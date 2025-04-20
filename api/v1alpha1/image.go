package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultRepository     = "quay.io/zncdatadev"
	DefaultProductVersion = "3.9.2"
	DefaultProductName    = "zookeeper"
)

// +k8s:openapi-gen=true
type ImageSpec struct {
	// +kubebuilder:validation:Optional
	Custom string `json:"custom,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=quay.io/zncdatadev
	Repo string `json:"repo,omitempty"`

	// +kubebuilder:validation:Optional
	KubedoopVersion string `json:"kubedoopVersion,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="3.9.2"
	ProductVersion string `json:"productVersion,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=IfNotPresent
	// +kubebuilder:validation:Enum=IfNotPresent;Always;Never
	PullPolicy *corev1.PullPolicy `json:"pullPolicy,omitempty"`

	// +kubebuilder:validation:Optional
	PullSecretName string `json:"pullSecretName,omitempty"`
}
