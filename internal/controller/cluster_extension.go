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

// PostReconcile is a no-op.
func (e *ClusterServiceExtension) PostReconcile(_ context.Context, _ client.Client, _ opcommon.ClusterInterface) error {
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
