package security

import (
	"fmt"
	"strconv"

	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
)

const (
	ZkClientPortConfigItem string = "clientPort"

	// Volume names
	ServerTlsVolumeName string = "server-tls"
	ClientTlsVolumeName string = "client-tls"
	QuorumTlsVolumeName string = "quorum-tls"

	// Quorum TLS config keys
	SSLQuorum                     string = "sslQuorum"
	SSLQuorumClientAuth           string = "ssl.quorum.clientAuth"
	SSLQuorumHostNameVerification string = "ssl.quorum.hostnameVerification"
	SSLQuorumKeyStoreLocation     string = "ssl.quorum.keyStore.location"
	SSLQuorumKeyStorePassword     string = "ssl.quorum.keyStore.password"
	SSLQuorumTrustStoreLocation   string = "ssl.quorum.trustStore.location"
	SSLQuorumTrustStorePassword   string = "ssl.quorum.trustStore.password"

	// Client TLS config keys
	SSLClientAuth           string = "ssl.clientAuth"
	SSLHostNameVerification string = "ssl.hostnameVerification"
	SSLKeyStoreLocation     string = "ssl.keyStore.location"
	SSLKeyStorePassword     string = "ssl.keyStore.password"
	SSLTrustStoreLocation   string = "ssl.trustStore.location"
	SSLTrustStorePassword   string = "ssl.trustStore.password"

	// Common TLS config keys
	SSLAuthProviderX509 string = "authProvider.x509"
	ServerCnxnFactory   string = "serverCnxnFactory"

	// Misc
	StorePasswordEnv string = "STORE_PASSWORD"

	// Authentication defaults
	TlsDefaultSecretClass string = "tls"

	TrueString = "true"
)

// TLSEnabled checks if TLS encryption is enabled based on server SecretClass or client AuthenticationClass.
func (z *ZookeeperSecurity) TLSEnabled() bool {
	return z.serverSecretClass != "" || z.resolvedAuthenticationClasses.GetTLSAuthenticationClass() != nil
}

// ClientPort returns the ZooKeeper (secure) client port depending on TLS or authentication settings.
func (z *ZookeeperSecurity) ClientPort() uint16 {
	if z.TLSEnabled() {
		return zkv1alpha1.SecureClientPort
	}
	return zkv1alpha1.ClientPort
}

// ConfigSettings returns required ZooKeeper configuration settings for the `zoo.cfg` properties file.
// The provisioner parameter provides mount paths for secret volumes — ZookeeperSecurity does not own it.
func (z *ZookeeperSecurity) ConfigSettings(provisioner *opgosecurity.SecretProvisioner) map[string]string {
	config := make(map[string]string)

	if z.quorumSecretClass != "" {
		authNeeded := "need"
		// Quorum TLS
		config[SSLQuorum] = TrueString
		config[SSLQuorumHostNameVerification] = TrueString
		config[SSLQuorumClientAuth] = authNeeded
		config[ServerCnxnFactory] = "org.apache.zookeeper.server.NettyServerCnxnFactory"
		config[SSLAuthProviderX509] = "org.apache.zookeeper.server.auth.X509AuthenticationProvider"
		config[SSLQuorumKeyStoreLocation] = fmt.Sprintf("%s/keystore.p12", provisioner.MustPath(QuorumTlsVolumeName))
		config[SSLQuorumTrustStoreLocation] = fmt.Sprintf("%s/truststore.p12", provisioner.MustPath(QuorumTlsVolumeName))
		if z.sslStorePassword != "" {
			config[SSLQuorumKeyStorePassword] = z.sslStorePassword
			config[SSLQuorumTrustStorePassword] = z.sslStorePassword
		}
	}

	// Server TLS
	if z.TLSEnabled() {
		// We set only the clientPort and portUnification here because otherwise there is a port bind exception
		// See: https://issues.apache.org/jira/browse/ZOOKEEPER-4276
		config[ZkClientPortConfigItem] = strconv.FormatUint(uint64(z.ClientPort()), 10)
		config["client.portUnification"] = TrueString
		config[SSLHostNameVerification] = TrueString

		config[SSLKeyStoreLocation] = fmt.Sprintf("%s/keystore.p12", provisioner.MustPath(ServerTlsVolumeName))

		trustStoreVolumeName := ServerTlsVolumeName
		// Auth TLS
		if tlsAuthClass := z.resolvedAuthenticationClasses.GetTLSAuthenticationClass(); tlsAuthClass != nil {
			config[SSLClientAuth] = "need"
			if tlsAuthClass.Spec.AuthenticationProvider.TLS.ClientCertSecretClass != "" {
				trustStoreVolumeName = ClientTlsVolumeName
			}
		}

		config[SSLTrustStoreLocation] = fmt.Sprintf("%s/truststore.p12", provisioner.MustPath(trustStoreVolumeName))

		if z.sslStorePassword != "" {
			config[SSLKeyStorePassword] = z.sslStorePassword
			config[SSLTrustStorePassword] = z.sslStorePassword
		}

	} else {
		config[ZkClientPortConfigItem] = strconv.FormatUint(uint64(z.ClientPort()), 10)
	}

	return config
}
