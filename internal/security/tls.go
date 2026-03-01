package security

import (
	"fmt"
	"strconv"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
)

const (
	ZkClientPortConfigItem string = "clientPort"

	// volume name and mount path
	ServerTlsVolumeName string = "server-tls"
	ClientTlsVolumeName string = "client-tls"
	QuorumTlsVolumeName string = "quorum-tls"

	QuorumTLSDir        string = "/kubedoop/quorum_tls"
	QuorumTLSMountDir   string = "/kubedoop/quorum_tls_mount"
	ServerTLSDir        string = "/kubedoop/server_tls"
	ServerTLSMountDir   string = "/kubedoop/server_tls_mount"
	ClientTLSDir        string = "/kubedoop/client_tls"
	SystemTrustStoreDir string = "/etc/pki/java/cacerts"

	// Quorum TLS
	SSLQuorum                     string = "sslQuorum"
	SSLQuorumClientAuth           string = "ssl.quorum.clientAuth"
	SSLQuorumHostNameVerification string = "ssl.quorum.hostnameVerification"
	SSLQuorumKeyStoreLocation     string = "ssl.quorum.keyStore.location"
	SSLQuorumKeyStorePassword     string = "ssl.quorum.keyStore.password"
	SSLQuorumTrustStoreLocation   string = "ssl.quorum.trustStore.location"
	SSLQuorumTrustStorePassword   string = "ssl.quorum.trustStore.password"

	// client TLS
	SSLClientAuth           string = "ssl.clientAuth"
	SSLHostNameVerification string = "ssl.hostnameVerification"
	SSLKeyStoreLocation     string = "ssl.keyStore.location"
	SSLKeyStorePassword     string = "ssl.keyStore.password"
	SSLTrustStoreLocation   string = "ssl.trustStore.location"
	SSLTrustStorePassword   string = "ssl.trustStore.password"

	// Common tls
	SSLAuthProviderX509 string = "authProvider.x509"
	ServerCnxnFactory   string = "serverCnxnFactory"

	// mis
	StorePasswordEnv string = "STORE_PASSWORD"

	// authentication classes
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

// AddVolumeMounts adds required volumes and volume mounts to the pod and container builders depending on TLS and authentication settings.
func (z *ZookeeperSecurity) AddVolumeMounts(podBuilder *corev1.PodTemplateSpec, zkContainer *corev1.Container) {
	// Server Identity (KeyStore)
	if z.serverSecretClass != "" {
		z.addVolumeMount(zkContainer, ServerTlsVolumeName, ServerTLSDir)
		tlsVolume := util.CreateTlsKeystoreVolume(ServerTlsVolumeName, z.serverSecretClass, z.sslStorePassword)
		z.addVolume(podBuilder, tlsVolume)
	}

	// Client Trust (TrustStore) from AuthenticationClass
	if tlsAuthClass := z.resolvedAuthenticationClasses.GetTLSAuthenticationClass(); tlsAuthClass != nil {
		if clientCertSecretClass := tlsAuthClass.Spec.AuthenticationProvider.TLS.ClientCertSecretClass; clientCertSecretClass != "" {
			z.addVolumeMount(zkContainer, ClientTlsVolumeName, ClientTLSDir)
			tlsVolume := util.CreateTlsKeystoreVolume(ClientTlsVolumeName, clientCertSecretClass, z.sslStorePassword)
			z.addVolume(podBuilder, tlsVolume)
		}
	}

	if z.quorumSecretClass != "" {
		z.addVolumeMount(zkContainer, QuorumTlsVolumeName, QuorumTLSDir)
		quorumTLSVolume := util.CreateTlsKeystoreVolume(QuorumTlsVolumeName, z.quorumSecretClass, z.sslStorePassword)
		z.addVolume(podBuilder, quorumTLSVolume)
	}
}

// statefulset add tls volumes
func (z *ZookeeperSecurity) addVolume(podSpec *corev1.PodTemplateSpec, volume corev1.Volume) {
	podSpec.Spec.Volumes = append(podSpec.Spec.Volumes, volume)
}

// container add tls volume mount
func (z *ZookeeperSecurity) addVolumeMount(container *corev1.Container, volumeName, mountPath string) {
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: mountPath})
}

// ConfigSettings returns required ZooKeeper configuration settings for the `zoo.cfg` properties file depending on TLS and authentication settings.
func (z *ZookeeperSecurity) ConfigSettings() map[string]string {
	config := make(map[string]string)

	if z.quorumSecretClass != "" {
		authNeeded := "need"
		// Quorum TLS
		config[SSLQuorum] = TrueString
		config[SSLQuorumHostNameVerification] = TrueString
		config[SSLQuorumClientAuth] = authNeeded
		config[ServerCnxnFactory] = "org.apache.zookeeper.server.NettyServerCnxnFactory"
		config[SSLAuthProviderX509] = "org.apache.zookeeper.server.auth.X509AuthenticationProvider"
		config[SSLQuorumKeyStoreLocation] = fmt.Sprintf("%s/keystore.p12", QuorumTLSDir)
		config[SSLQuorumTrustStoreLocation] = fmt.Sprintf("%s/truststore.p12", QuorumTLSDir)
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

		config[SSLKeyStoreLocation] = fmt.Sprintf("%s/keystore.p12", ServerTLSDir)

		trustStoreDir := ServerTLSDir
		// Auth TLS
		if tlsAuthClass := z.resolvedAuthenticationClasses.GetTLSAuthenticationClass(); tlsAuthClass != nil {
			config[SSLClientAuth] = "need"
			if tlsAuthClass.Spec.AuthenticationProvider.TLS.ClientCertSecretClass != "" {
				trustStoreDir = ClientTLSDir
			}
		}

		config[SSLTrustStoreLocation] = fmt.Sprintf("%s/truststore.p12", trustStoreDir)

		if z.sslStorePassword != "" {
			config[SSLKeyStorePassword] = z.sslStorePassword
			config[SSLTrustStorePassword] = z.sslStorePassword
		}

	} else {
		config[ZkClientPortConfigItem] = strconv.FormatUint(uint64(z.ClientPort()), 10)
	}

	return config
}
