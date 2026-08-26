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
// The framework records reconcile progress and faults as Events on the cluster CR and the resources
// it builds, in the namespace the CR lives in. Without this the API server rejects every one of
// them ("events is forbidden"), so the operator's own warnings — an ignored immutable field, a
// failed role group — are invisible to `kubectl describe`. Patch is required as well as create:
// repeated events are aggregated onto the existing object rather than written anew.
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=authentication.kubedoop.dev,resources=authenticationclasses,verbs=get;list;watch
