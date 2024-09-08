package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zncdatadev/operator-go/pkg/util"
)

const (
	DefaultRepository      = "quay.io/zncdatadev"
	DefaultProductVersion  = "3.9.2"
	DefaultProductName     = "zookeeper"
	DefaultPlatformVersion = "0.0.0-dev"
)

type ImageSpec struct {
	// +kubebuilder:validation:Optional
	Custom string `json:"custom,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=quay.io/zncdatadev
	Repo string `json:"repository,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0.0.0-dev"
	PlatformVersion string `json:"platformVersion,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="3.9.2"
	ProductVersion string `json:"productVersion,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=IfNotPresent
	PullPolicy *corev1.PullPolicy `json:"pullPolicy,omitempty"`

	// +kubebuilder:validation:Optional
	PullSecretName string `json:"pullSecretName,omitempty"`
}

func TransformImage(imageSpec *ImageSpec) *util.Image {
	if imageSpec == nil {
		return util.NewImage(DefaultProductName, DefaultPlatformVersion, DefaultProductVersion)
	}
	var pullPolicy corev1.PullPolicy = corev1.PullIfNotPresent
	if imageSpec.PullPolicy != nil {
		pullPolicy = *imageSpec.PullPolicy
	}
	return &util.Image{
		Custom:          imageSpec.Custom,
		Repo:            imageSpec.Repo,
		PlatformVersion: imageSpec.PlatformVersion,
		ProductVersion:  imageSpec.ProductVersion,
		PullPolicy:      pullPolicy,
		PullSecretName:  imageSpec.PullSecretName,
		ProductName:     DefaultProductName,
	}
}
