package common

import (
	"fmt"
	"maps"
	"strconv"
	"time"

	commonsv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/commons/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/constants"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Role string

const (
	Server Role = "server"
)

// ZookeeperConfig defines the desired state of ZookeeperServer
type ZookeeperConfig struct {
	initLimit  *int
	syncLimit  *int
	tickTime   *int
	myidOffset int

	resources *commonsv1alpha1.ResourcesSpec
	// Logging Logging `json:"logging,omitempty"`
	common *GeneralNodeConfig

	securityProps map[string]string
}

type GeneralNodeConfig struct {
	Affinity *corev1.Affinity

	gracefulShutdownTimeoutSeconds time.Duration
}

func (G *GeneralNodeConfig) GetgracefulShutdownTimeoutSeconds() *string {
	seconds := G.gracefulShutdownTimeoutSeconds.Seconds()
	v := strconv.Itoa(int(seconds)) + "s"
	return &v
}

const (
	INIT_LIMIT = "initLimit"
	SYNC_LIMIT = "syncLimit"
	TICK_TIME  = "tickTime"
	DATA_DIR   = "dataDir"

	MyIdOffset     = "MYID_OFFSET"
	ServerJvmFlags = "SERVER_JVMFLAGS"
	ZKServerHeap   = "ZK_SERVER_HEAP"

	DefaultServerGrace = 120
	DefaultInitLimit   = 5
	DefaultSyncLimit   = 2
	DefaultTickTime    = 3000
	DefaultMyidOffset  = 1
)

func DefaultServerConfig(clusterName string) ZookeeperConfig {
	return ZookeeperConfig{
		initLimit:  func() *int { v := DefaultInitLimit; return &v }(),
		syncLimit:  func() *int { v := DefaultSyncLimit; return &v }(),
		tickTime:   func() *int { v := DefaultTickTime; return &v }(),
		myidOffset: func() int { v := DefaultMyidOffset; return v }(),
		resources:  defaultResources(),
		common: &GeneralNodeConfig{
			Affinity:                       getAffinity(clusterName),
			gracefulShutdownTimeoutSeconds: DefaultServerGrace * time.Second,
		},
		securityProps: DefautlSercurityProperties(),
	}
}

func defaultResources() *commonsv1alpha1.ResourcesSpec {
	return &commonsv1alpha1.ResourcesSpec{
		CPU: &commonsv1alpha1.CPUResource{
			Max: *parseQuantity("800m"),
			Min: *parseQuantity("200m"),
		},
		Memory: &commonsv1alpha1.MemoryResource{
			Limit: *parseQuantity("512Mi"),
		},
		Storage: &commonsv1alpha1.StorageResource{
			Capacity: *parseQuantity("1Gi"),
		},
	}
}

// │ networkaddress.cache.negative.ttl=0
// │ networkaddress.cache.ttl=5
func DefautlSercurityProperties() map[string]string {
	return map[string]string{
		"networkaddress.cache.ttl":          "5",
		"networkaddress.cache.negative.ttl": "0",
	}
}

func getAffinity(clusterName string) *corev1.Affinity {
	return NewAffinityBuilder(
		*NewPodAffinity(map[string]string{LabelCrName: clusterName, LabelComponent: string(Server)}, false, true).Weight(70),
	).Build()
}

func parseQuantity(q string) *resource.Quantity {
	r := resource.MustParse(q)
	return &r
}

func (n *ZookeeperConfig) defaultZooCfg() map[string]string {
	return map[string]string{
		INIT_LIMIT: strconv.Itoa(*n.initLimit),
		SYNC_LIMIT: strconv.Itoa(*n.syncLimit),
		TICK_TIME:  strconv.Itoa(*n.tickTime),
		DATA_DIR:   constants.KubedoopDataDir,
	}
}

func (n *ZookeeperConfig) MergeDefaultConfig(mergedCfg *zkv1alpha1.RoleGroupSpec) {
	config := mergedCfg.Config

	if config == nil {
		config = &zkv1alpha1.ConfigSpec{}
	}
	// mergedresources
	if mergedresources := config.Resources; mergedresources == nil {
		mergedCfg.Config.Resources = n.resources
	} else {
		if mergedCpu := mergedresources.CPU; mergedCpu == nil {
			mergedCfg.Config.Resources.CPU = n.resources.CPU
		}
		if mergedMemory := mergedresources.Memory; mergedMemory == nil {
			mergedCfg.Config.Resources.Memory = n.resources.Memory
		}
		if mergedStorage := mergedresources.Storage; mergedStorage == nil {
			mergedCfg.Config.Resources.Storage = n.resources.Storage
		}
	}

	//affinity
	if config.Affinity == nil {
		mergedCfg.Config.Affinity = n.common.Affinity
	}

	// gracefulShutdownTimeoutSeconds
	if config.GracefulShutdownTimeout == nil {
		mergedCfg.Config.GracefulShutdownTimeout = n.common.GetgracefulShutdownTimeoutSeconds()
	}

	// configOverride
	if mergedCfg.ConfigOverrides == nil {
		mergedCfg.ConfigOverrides = &zkv1alpha1.ConfigOverridesSpec{}
	}

	// zoo.cfg
	if mergedCfg.ConfigOverrides.ZooCfg == nil {
		mergedCfg.ConfigOverrides.ZooCfg = n.defaultZooCfg()
	} else {
		src := mergedCfg.ConfigOverrides.ZooCfg
		dist := n.defaultZooCfg()
		maps.Copy(dist, src)
		mergedCfg.ConfigOverrides.ZooCfg = dist
	}
	// security.properties
	if mergedCfg.ConfigOverrides.SercurityProps == nil {
		mergedCfg.ConfigOverrides.SercurityProps = n.securityProps
	} else {
		src := mergedCfg.ConfigOverrides.SercurityProps
		dist := n.securityProps
		maps.Copy(dist, src)
		mergedCfg.ConfigOverrides.SercurityProps = dist
	}
	// You can continue to add logic to handle other fields
	// config.FieldByName("Logging").Set(reflect.ValueOf(n.common.Logging))
}

func HeapLimit(resource *commonsv1alpha1.ResourcesSpec) *string {
	if resource != nil && resource.Memory != nil {
		memoryLimit := resource.Memory.Limit
		heapLimit := util.QuantityToMB(memoryLimit) * 0.8
		value := fmt.Sprintf("%.0f", heapLimit)
		return &value
	}
	return nil
}
