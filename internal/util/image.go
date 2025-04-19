package util

import (
	"github.com/zncdatadev/operator-go/pkg/util"
	corev1 "k8s.io/api/core/v1"

	zookeeperv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	zookeeperversion "github.com/zncdatadev/zookeeper-operator/internal/util/version"
)

func TransformImage(imageSpec *zookeeperv1alpha1.ImageSpec) *util.Image {
	if imageSpec == nil {
		return util.NewImage(
			zookeeperv1alpha1.DefaultProductName,
			zookeeperversion.BuildVersion,
			zookeeperv1alpha1.DefaultProductVersion,
		)
	}
	var pullPolicy = corev1.PullIfNotPresent
	if imageSpec.PullPolicy != nil {
		pullPolicy = *imageSpec.PullPolicy
	}
	return &util.Image{
		Custom:          imageSpec.Custom,
		Repo:            imageSpec.Repo,
		KubedoopVersion: imageSpec.KubedoopVersion,
		ProductVersion:  imageSpec.ProductVersion,
		PullPolicy:      pullPolicy,
		PullSecretName:  imageSpec.PullSecretName,
		ProductName:     zookeeperv1alpha1.DefaultProductName,
	}
}
