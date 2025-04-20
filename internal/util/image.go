package util

import (
	"github.com/zncdatadev/operator-go/pkg/util"

	zookeeperv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	zookeeperversion "github.com/zncdatadev/zookeeper-operator/internal/util/version"
)

func TransformImage(imageSpec *zookeeperv1alpha1.ImageSpec) *util.Image {
	image := util.NewImage(
		zookeeperv1alpha1.DefaultProductName,
		zookeeperversion.BuildVersion,
		imageSpec.ProductVersion,
		func(options *util.ImageOptions) {
			options.Custom = imageSpec.Custom
			options.Repo = imageSpec.Repo
			options.PullPolicy = *imageSpec.PullPolicy
		},
	)

	if imageSpec.KubedoopVersion != "" {
		image.KubedoopVersion = imageSpec.KubedoopVersion
	}

	return image
}
