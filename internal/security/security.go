package security

import (
	"context"
	"fmt"

	authv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/authentication/v1alpha1"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolvedAuthenticationClasses holds the resolved AuthenticationClass resources
type ResolvedAuthenticationClasses struct {
	authenticationClasses []authv1alpha1.AuthenticationClass
}

// GetTLSAuthenticationClass returns the first TLS AuthenticationClass if available
func (r *ResolvedAuthenticationClasses) GetTLSAuthenticationClass() *authv1alpha1.AuthenticationClass {
	for i := range r.authenticationClasses {
		if r.authenticationClasses[i].Spec.AuthenticationProvider != nil &&
			r.authenticationClasses[i].Spec.AuthenticationProvider.TLS != nil {
			return &r.authenticationClasses[i]
		}
	}
	return nil
}

// Validate validates the resolved AuthenticationClasses
// Currently errors out if:
// - More than one AuthenticationClass was provided
// - AuthenticationClass mechanism was not supported (only TLS is supported)
func (r *ResolvedAuthenticationClasses) Validate() error {
	if len(r.authenticationClasses) > 1 {
		return fmt.Errorf("multiple authentication classes provided, only one is supported")
	}

	for _, authClass := range r.authenticationClasses {
		if authClass.Spec.AuthenticationProvider == nil {
			return fmt.Errorf("authentication class %s has no provider configured", authClass.Name)
		}

		provider := authClass.Spec.AuthenticationProvider
		// Only TLS is supported for ZooKeeper
		if provider.TLS == nil {
			if provider.LDAP != nil {
				return fmt.Errorf("LDAP authentication is not supported for ZooKeeper, authentication class: %s", authClass.Name)
			}
			if provider.OIDC != nil {
				return fmt.Errorf("OIDC authentication is not supported for ZooKeeper, authentication class: %s", authClass.Name)
			}
			if provider.Static != nil {
				return fmt.Errorf("static authentication is not supported for ZooKeeper, authentication class: %s", authClass.Name)
			}
			return fmt.Errorf("unsupported authentication method in class: %s", authClass.Name)
		}
	}

	return nil
}

// ResolveAuthenticationClasses resolves provided AuthenticationClasses via API calls and validates the contents
func ResolveAuthenticationClasses(
	ctx context.Context,
	k8sClient client.Client,
	authSpecs []zkv1alpha1.AuthenticationSpec,
) (*ResolvedAuthenticationClasses, error) {
	resolved := &ResolvedAuthenticationClasses{
		authenticationClasses: make([]authv1alpha1.AuthenticationClass, 0, len(authSpecs)),
	}

	for _, authSpec := range authSpecs {
		var authClass authv1alpha1.AuthenticationClass
		// AuthenticationClass is cluster-scoped, so no namespace is needed
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: authSpec.AuthenticationClass,
		}, &authClass)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve authentication class %s: %w", authSpec.AuthenticationClass, err)
		}
		resolved.authenticationClasses = append(resolved.authenticationClasses, authClass)
	}

	if err := resolved.Validate(); err != nil {
		return nil, fmt.Errorf("authentication class validation failed: %w", err)
	}

	return resolved, nil
}

// NewZookeeperSecurity creates a ZookeeperSecurity struct from the Zookeeper custom resource and resolves all provided AuthenticationClass references.
func NewZookeeperSecurity(
	ctx context.Context,
	k8sClient client.Client,
	clusterConfig *zkv1alpha1.ClusterConfigSpec,
) (*ZookeeperSecurity, error) {
	var resolvedAuthenticationClasses *ResolvedAuthenticationClasses
	var err error

	// Resolve authentication classes if configured
	if clusterConfig != nil && len(clusterConfig.Authentication) > 0 {
		resolvedAuthenticationClasses, err = ResolveAuthenticationClasses(ctx, k8sClient, clusterConfig.Authentication)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve authentication classes: %w", err)
		}
	} else {
		// Initialize with empty resolved authentication classes
		resolvedAuthenticationClasses = &ResolvedAuthenticationClasses{
			authenticationClasses: []authv1alpha1.AuthenticationClass{},
		}
	}

	sslStorePassword := "changeit"
	serverSecretClass := ""
	quorumSecretClass := ""
	if clusterConfig != nil && clusterConfig.Tls != nil {
		serverSecretClass = clusterConfig.Tls.ServerSecretClass
		quorumSecretClass = clusterConfig.Tls.QuorumSecretClass
	}

	return &ZookeeperSecurity{
		resolvedAuthenticationClasses: resolvedAuthenticationClasses,
		serverSecretClass:             serverSecretClass,
		quorumSecretClass:             quorumSecretClass,
		sslStorePassword:              sslStorePassword,
	}, nil
}

type ZookeeperSecurity struct {
	resolvedAuthenticationClasses *ResolvedAuthenticationClasses
	serverSecretClass             string
	quorumSecretClass             string
	sslStorePassword              string
}
