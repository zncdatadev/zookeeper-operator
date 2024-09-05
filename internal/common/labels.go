package common

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LabelCrName    = "app.kubernetes.io/name"
	LabelComponent = "app.kubernetes.io/component"
	LabelManagedBy = "app.kubernetes.io/managed-by"
)

type RoleLabels[T client.Object] struct {
	Cr   T
	Name string
}

func (r *RoleLabels[T]) GetLabels() map[string]string {
	return map[string]string{
		LabelCrName:    strings.ToLower(r.Cr.GetName()),
		LabelComponent: r.Name,
		LabelManagedBy: "zookeeper-operator",
	}
}
