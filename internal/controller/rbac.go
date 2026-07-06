package controller

// ZookeeperCluster controller RBAC.
//
// Cluster reconciliation runs through operator-go's GenericReconciler — which builds StatefulSets,
// ConfigMaps, Services and PodDisruptionBudgets and provisions a ServiceAccount — plus a pod-exec
// service health check. These permissions are in addition to the ZookeeperZnode controller's
// markers (see internal/znodecontroller). Without them the manager cannot even watch its own
// ZookeeperCluster CRD and fails to sync its informer caches at startup.
//
// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=zookeeper.kubedoop.dev,resources=zookeeperclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=authentication.kubedoop.dev,resources=authenticationclasses,verbs=get;list;watch
