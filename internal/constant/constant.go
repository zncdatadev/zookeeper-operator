package constant

// Kubedoop directory constants (previously from operator-go/pkg/constants).
// The *-mount paths are framework-canonical (operator-go pkg/constant); reference those
// directly rather than redefining them here, so the config-mount path can never diverge
// from where the framework mounts the config ConfigMap.
const (
	KubedoopRoot      = "/kubedoop"
	KubedoopDataDir   = "/kubedoop/data"
	KubedoopConfigDir = "/kubedoop/config"
	KubedoopLogDir    = "/kubedoop/log"
)
