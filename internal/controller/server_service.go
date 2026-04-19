package controller

import (
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// buildHeadlessService creates the headless service for StatefulSet network identity.
func (h *ZkRoleGroupHandler) buildHeadlessService(
	buildCtx *reconciler.RoleGroupBuildContext,
	labels map[string]string,
	zkSecurity *security.ZookeeperSecurity,
) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildCtx.ResourceName + "-headless",
			Namespace: buildCtx.ClusterNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 labels,
			Ports: []corev1.ServicePort{
				{
					Name:       zkv1alpha1.ClientPortName,
					Port:       int32(zkSecurity.ClientPort()),
					TargetPort: intstr.FromInt(int(zkSecurity.ClientPort())),
				},
				{
					Name:       zkv1alpha1.MetricsPortName,
					Port:       zkv1alpha1.MetricsPort,
					TargetPort: intstr.FromInt(int(zkv1alpha1.MetricsPort)),
				},
			},
		},
	}
}

// buildClientService creates the client-facing service (ClusterIP or NodePort).
func (h *ZkRoleGroupHandler) buildClientService(
	buildCtx *reconciler.RoleGroupBuildContext,
	labels map[string]string,
	zkSecurity *security.ZookeeperSecurity,
	isNodePort bool,
) *corev1.Service {
	svcType := corev1.ServiceTypeClusterIP
	if isNodePort {
		svcType = corev1.ServiceTypeNodePort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildCtx.ResourceName,
			Namespace: buildCtx.ClusterNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       zkv1alpha1.ClientPortName,
					Port:       int32(zkSecurity.ClientPort()),
					TargetPort: intstr.FromInt(int(zkSecurity.ClientPort())),
				},
				{
					Name:       zkv1alpha1.MetricsPortName,
					Port:       zkv1alpha1.MetricsPort,
					TargetPort: intstr.FromInt(int(zkv1alpha1.MetricsPort)),
				},
			},
		},
	}
}
