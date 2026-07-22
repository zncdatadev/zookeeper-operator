package controller

import (
	"context"
	"fmt"

	opcommon "github.com/zncdatadev/operator-go/pkg/common"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterServiceExtension is a cluster-scope reconciliation extension that creates a
// cluster-wide client Service named after the ZookeeperCluster.
//
// The GenericReconciler only builds role-group-scoped resources ({cluster}-{group}
// services). A single Service named after the cluster is still required by:
//   - the ZookeeperZnode controller, which connects to common.ClusterServiceName(cluster)
//     to create/delete znodes; and
//   - external-unstable discovery, which reads the NodePort from a Service named after
//     the cluster.
//
// This extension restores that cluster-level Service, selecting all server pods across
// role groups via the app.kubernetes.io/instance + role labels.
type ClusterServiceExtension struct {
	scheme *runtime.Scheme
}

var _ opcommon.ClusterExtension[opcommon.ClusterInterface] = &ClusterServiceExtension{}

// NewClusterServiceExtension creates a new ClusterServiceExtension.
func NewClusterServiceExtension(scheme *runtime.Scheme) *ClusterServiceExtension {
	return &ClusterServiceExtension{scheme: scheme}
}

// Name implements opcommon.Extension.
func (e *ClusterServiceExtension) Name() string { return "zookeeper-cluster-service" }

// PreReconcile ensures the cluster-wide client Service exists before role groups are
// reconciled, so the ZookeeperZnode controller can connect as soon as pods are ready.
func (e *ClusterServiceExtension) PreReconcile(ctx context.Context, c client.Client, cr opcommon.ClusterInterface) error {
	zkCluster, ok := cr.(*zkv1alpha1.ZookeeperCluster)
	if !ok {
		// Not a ZookeeperCluster; the global registry is shared, so just skip.
		return nil
	}
	return e.ensureClusterService(ctx, c, zkCluster)
}

// PostReconcile creates the cluster-level discovery ConfigMap(s) after the role groups (and their
// pods/endpoints) have been reconciled.
func (e *ClusterServiceExtension) PostReconcile(ctx context.Context, c client.Client, cr opcommon.ClusterInterface) error {
	zkCluster, ok := cr.(*zkv1alpha1.ZookeeperCluster)
	if !ok {
		return nil
	}
	return e.ensureClusterDiscovery(ctx, c, zkCluster)
}

// ensureClusterDiscovery creates the cluster-level discovery ConfigMap(s) so clients can connect
// to the whole ensemble at the root znode "/" WITHOUT creating a ZookeeperZnode. This restores
// pre-refactor behavior: the cluster reconcile owned a discovery ConfigMap named after the cluster
// (the GenericReconciler only builds role-group-scoped resources, and the ZookeeperZnode controller
// only creates per-znode discovery). A ClusterInternal ConfigMap ("<cluster>") is always produced;
// an ExternalUnstable ConfigMap ("<cluster>-nodeport") is added for the external-unstable listener
// class, mirroring the per-znode discovery the ZookeeperZnode controller emits.
func (e *ClusterServiceExtension) ensureClusterDiscovery(ctx context.Context, c client.Client, cr *zkv1alpha1.ZookeeperCluster) error {
	zkSecurity, err := security.NewZookeeperSecurity(ctx, c, cr.Spec.ClusterConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve zookeeper security for cluster discovery: %w", err)
	}
	// Root znode: the cluster-level discovery advertises the ensemble at "/".
	znodeInfo := &common.ZNodeInfo{Name: cr.Name, Namespace: cr.Namespace, ZNodePath: "/"}

	if err := e.applyDiscoveryConfigMap(ctx, c, cr, zkSecurity, znodeInfo, zkv1alpha1.ClusterInternal); err != nil {
		return err
	}
	if cr.Spec.ClusterConfig != nil && cr.Spec.ClusterConfig.ListenerClass == zkv1alpha1.ExternalUnstable {
		if err := e.applyDiscoveryConfigMap(ctx, c, cr, zkSecurity, znodeInfo, zkv1alpha1.ExternalUnstable); err != nil {
			return err
		}
	}
	return nil
}

// applyDiscoveryConfigMap builds a discovery ConfigMap via the shared discoverer and applies it,
// owned by the cluster so it is garbage-collected with it.
func (e *ClusterServiceExtension) applyDiscoveryConfigMap(
	ctx context.Context,
	c client.Client,
	cr *zkv1alpha1.ZookeeperCluster,
	zkSecurity *security.ZookeeperSecurity,
	znodeInfo *common.ZNodeInfo,
	listenerClass zkv1alpha1.ListenerClass,
) error {
	desired, err := common.CreateDiscoveryConfigMap(ctx, c, cr, cr, zkSecurity, znodeInfo, listenerClass)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
		cm.Labels = desired.Labels
		cm.Data = desired.Data
		return controllerutil.SetControllerReference(cr, cm, e.scheme)
	}); err != nil {
		return fmt.Errorf("failed to apply discovery configmap %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	return nil
}

// OnReconcileError is a no-op.
func (e *ClusterServiceExtension) OnReconcileError(_ context.Context, _ client.Client, _ opcommon.ClusterInterface, _ error) error {
	return nil
}

func (e *ClusterServiceExtension) ensureClusterService(ctx context.Context, c client.Client, cr *zkv1alpha1.ZookeeperCluster) error {
	zkSecurity, err := security.NewZookeeperSecurity(ctx, c, cr.Spec.ClusterConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve zookeeper security for cluster service: %w", err)
	}
	clientPort := int32(zkSecurity.ClientPort())

	svcType := corev1.ServiceTypeClusterIP
	if cr.Spec.ClusterConfig != nil && cr.Spec.ClusterConfig.ListenerClass == zkv1alpha1.ExternalUnstable {
		svcType = corev1.ServiceTypeNodePort
	}

	// Selector uses the product-owned identity labels (cluster + role, without role-group) so
	// it matches all server pods across role groups. The product-domain prefix guarantees it
	// never selects another product's "server" pods in the same namespace.
	selector := map[string]string{
		reconciler.ClusterLabelKey(LabelDomain): cr.Name,
		reconciler.RoleLabelKey(LabelDomain):    serverRoleName,
	}
	labels := map[string]string{
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/name":      zkv1alpha1.DefaultProductName,
		"app.kubernetes.io/component": serverRoleName,
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ClusterServiceName(cr.Name),
			Namespace: cr.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, c, svc, func() error {
		// Preserve an already-allocated NodePort across updates to avoid churn that
		// would invalidate the discovery ConfigMap.
		var existingNodePort int32
		for _, p := range svc.Spec.Ports {
			if p.Name == zkv1alpha1.ClientPortName {
				existingNodePort = p.NodePort
				break
			}
		}

		port := corev1.ServicePort{
			Name:       zkv1alpha1.ClientPortName,
			Port:       clientPort,
			TargetPort: intstr.FromInt(int(clientPort)),
			Protocol:   corev1.ProtocolTCP,
		}
		if svcType == corev1.ServiceTypeNodePort && existingNodePort != 0 {
			port.NodePort = existingNodePort
		}

		svc.Labels = labels
		svc.Spec.Type = svcType
		svc.Spec.Selector = selector
		// PublishNotReadyAddresses mirrors the headless service so clients can resolve
		// peers during rolling restarts.
		svc.Spec.PublishNotReadyAddresses = true
		svc.Spec.Ports = []corev1.ServicePort{port}
		return controllerutil.SetControllerReference(cr, svc, e.scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to ensure cluster service %s/%s: %w", cr.Namespace, svc.Name, err)
	}

	log.FromContext(ctx).V(1).Info("ensured cluster service", "name", svc.Name, "type", svcType)
	return nil
}
