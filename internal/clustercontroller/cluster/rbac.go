package cluster

import (
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/client"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

func NewServiceAccountReconciler(
	client client.Client,
	lables map[string]string,
) reconciler.ResourceReconciler[*builder.GenericServiceAccountBuilder] {
	saName := builder.ServiceAccountName(zkv1alpha1.DefaultProductName)
	saBuilder := builder.NewGenericServiceAccountBuilder(&client, saName, lables, nil)
	return reconciler.NewGenericResourceReconciler(&client, saName, saBuilder)
}

func NewClusterRoleBindingReconciler(
	client client.Client,
	lables map[string]string,
) reconciler.ResourceReconciler[*builder.GenericRoleBindingBuilder] {
	roleBindingName := builder.RoleBindingName(zkv1alpha1.DefaultProductName)
	roleBindingBuilder := builder.NewGenericRoleBindingBuilder(&client, roleBindingName, lables, nil)
	roleBindingBuilder.AddSubject(builder.ServiceAccountName(zkv1alpha1.DefaultProductName))
	roleBindingBuilder.SetRoleRef(builder.ClusterRoleName(zkv1alpha1.DefaultProductName), true)
	return reconciler.NewGenericResourceReconciler(&client, roleBindingName, roleBindingBuilder)
}
