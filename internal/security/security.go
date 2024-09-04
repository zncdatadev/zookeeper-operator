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

	//volume name and mount path
	ServerTlsVolumeName string = "server-tls"
	QuorumTlsVolumeName string = "quorum-tls"

	QuorumTLSDir        string = "/kubedoop/quorum_tls"
	QuorumTLSMountDir   string = "/kubedoop/quorum_tls_mount"
	ServerTLSDir        string = "/kubedoop/server_tls"
	ServerTLSMountDir   string = "/kubedoop/server_tls_mount"
	SystemTrustStoreDir string = "/etc/pki/java/cacerts"

	//Quorum TLS
	SSLQuorum                     string = "sslQuorum"
	SSLQuorumClientAuth           string = "ssl.quorum.clientAuth"
	SSLQuorumHostNameVerification string = "ssl.quorum.hostnameVerification"
	SSLQuorumKeyStoreLocation     string = "ssl.quorum.keyStore.location"
	SSLQuorumKeyStorePassword     string = "ssl.quorum.keyStore.password"
	SSLQuorumTrustStoreLocation   string = "ssl.quorum.trustStore.location"
	SSLQuorumTrustStorePassword   string = "ssl.quorum.trustStore.password"

	//client TLS
	SSLClientAuth           string = "ssl.clientAuth"
	SSLHostNameVerification string = "ssl.hostnameVerification"
	SSLKeyStoreLocation     string = "ssl.keyStore.location"
	SSLKeyStorePassword     string = "ssl.keyStore.password"
	SSLTrustStoreLocation   string = "ssl.trustStore.location"
	SSLTrustStorePassword   string = "ssl.trustStore.password"

	//Common tls
	SSLAuthProviderX509 string = "authProvider.x509"
	ServerCnxnFactory   string = "serverCnxnFactory"

	//mis
	StorePasswordEnv string = "STORE_PASSWORD"

	//authentication classes
	TlsDefaultSecretClass string = "tls"
)

// NewZookeeperSecurity creates a ZookeeperSecurity struct from the Zookeeper custom resource and resolves all provided AuthenticationClass references.
func NewZookeeperSecurity(clusterConfig *zkv1alpha1.ClusterConfigSpec) (*ZookeeperSecurity, error) {
	resolvedAuthenticationClasses := "" // TODO, unsupported for now

	sslStorePassword := "changeit"
	// quorumSecretClass := quorumTLSDefault()
	// serverSecretClass := serverTLSDefault()
	serverSecretClass := ""
	quorumSecretClass := ""
	if clusterConfig.Tls != nil {
		serverSecretClass = clusterConfig.Tls.ServerSecretClass
		sslStorePassword = clusterConfig.Tls.SSLStorePassword
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
	resolvedAuthenticationClasses string
	serverSecretClass             string
	quorumSecretClass             string
	sslStorePassword              string
}

// TLSEnabled checks if TLS encryption is enabled based on server SecretClass or client AuthenticationClass.
func (z *ZookeeperSecurity) TLSEnabled() bool {
	return z.serverSecretClass != "" || z.resolvedAuthenticationClasses != ""
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
	tlsSecretClass := z.getTLSSecretClass()
	if tlsSecretClass != "" {
		z.addVolumeMount(zkContainer, ServerTlsVolumeName, ServerTLSDir)
		tlsVolume := util.CreateTlsKeystoreVolume(ServerTlsVolumeName, tlsSecretClass, z.sslStorePassword)
		z.addVolume(podBuilder, tlsVolume)
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
		config[SSLQuorum] = "true"
		config[SSLQuorumHostNameVerification] = "true"
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
		// --> Normally we would like to only set the secureClientPort (check out commented code below)
		// What we tried:
		// 1) Set clientPort and secureClientPort will fail with
		// "static.config different from dynamic config .. "
		// config.insert(
		//     Self::CLIENT_PORT_NAME.to_string(),
		//     CLIENT_PORT.to_string(),
		// );
		// config.insert(
		//     Self::SECURE_CLIENT_PORT_NAME.to_string(),
		//     SECURE_CLIENT_PORT.to_string(),
		// );

		// 2) Setting only secureClientPort will config in the above mentioned bind exception.
		// The NettyFactory tries to bind multiple times on the secureClientPort.
		// config.insert(
		//     Self::SECURE_CLIENT_PORT_NAME.to_string(),
		//     self.client_port(.to_string()),
		// );

		// 3) Using the clientPort and portUnification still allows plaintext connection without
		// authentication, but at least TLS and authentication works when connecting securely.
		config[ZkClientPortConfigItem] = strconv.FormatUint(uint64(z.ClientPort()), 10)
		config["client.portUnification"] = "true"
		config[SSLHostNameVerification] = "true"
		// todo and checked in init container. The keystore and truststore passwords should not be in the configmap and are generated
		// and written later via script in the init container
		config[SSLKeyStoreLocation] = fmt.Sprintf("%s/keystore.p12", ServerTLSDir)
		config[SSLTrustStoreLocation] = fmt.Sprintf("%s/truststore.p12", ServerTLSDir)

		if z.sslStorePassword != "" {
			config[SSLKeyStorePassword] = z.sslStorePassword
			config[SSLTrustStorePassword] = z.sslStorePassword
		}

		//todo auth tls
		if z.resolvedAuthenticationClasses != "" {
			config[SSLClientAuth] = "need"
		}
	} else {
		config[ZkClientPortConfigItem] = strconv.FormatUint(uint64(z.ClientPort()), 10)
	}

	return config
}

// GetTLSSecretClass returns the SecretClass provided in an AuthenticationClass for TLS.
func (z *ZookeeperSecurity) getTLSSecretClass() string {
	authClass := z.resolvedAuthenticationClasses
	if authClass != "" {
		return authClass
	}

	if z.serverSecretClass != "" {
		return z.serverSecretClass
	}

	return ""
}

// Helper methods to provide defaults in the CRDs and tests
// func serverTLSDefault() string {
// 	return TlsDefaultSecretClass
// }

// // quorumTLSDefault
// // Helper methods to provide defaults in the CRDs and tests
// func quorumTLSDefault() string {
// 	return TlsDefaultSecretClass
// }
