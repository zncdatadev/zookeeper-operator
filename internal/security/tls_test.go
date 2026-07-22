package security

import (
	"testing"

	opgosecurity "github.com/zncdatadev/operator-go/pkg/security"
)

// newTestProvisioner builds a SecretProvisioner with the given TLS volumes registered, so
// ConfigSettings' MustPath lookups resolve during the test.
func newTestProvisioner(volumes ...string) *opgosecurity.SecretProvisioner {
	p := opgosecurity.NewSecretProvisioner()
	for _, v := range volumes {
		p.Register(opgosecurity.TLS(v, "tls"))
	}
	return p
}

// TestConfigSettingsCommonTLSSettings locks the contract that the Netty connection factory and
// the X509 auth provider are emitted whenever ANY TLS is enabled — not just quorum TLS. ZooKeeper
// client/server TLS is unusable with the default NIO factory, so a client-TLS-only config (server
// secret class or TLS auth class, no quorum) must still get them.
func TestConfigSettingsCommonTLSSettings(t *testing.T) {
	const nettyFactory = "org.apache.zookeeper.server.NettyServerCnxnFactory"
	emptyAuth := &ResolvedAuthenticationClasses{}

	tests := []struct {
		name    string
		sec     *ZookeeperSecurity
		volumes []string
		wantTLS bool // expect the common Netty factory + X509 provider
	}{
		{
			name:    "no TLS",
			sec:     &ZookeeperSecurity{resolvedAuthenticationClasses: emptyAuth},
			wantTLS: false,
		},
		{
			name:    "server TLS only (no quorum)",
			sec:     &ZookeeperSecurity{resolvedAuthenticationClasses: emptyAuth, serverSecretClass: "tls", sslStorePassword: "changeit"},
			volumes: []string{ServerTlsVolumeName},
			wantTLS: true,
		},
		{
			name:    "quorum TLS only",
			sec:     &ZookeeperSecurity{resolvedAuthenticationClasses: emptyAuth, quorumSecretClass: "tls", sslStorePassword: "changeit"},
			volumes: []string{QuorumTlsVolumeName},
			wantTLS: true,
		},
		{
			name:    "server + quorum TLS",
			sec:     &ZookeeperSecurity{resolvedAuthenticationClasses: emptyAuth, serverSecretClass: "tls", quorumSecretClass: "tls", sslStorePassword: "changeit"},
			volumes: []string{ServerTlsVolumeName, QuorumTlsVolumeName},
			wantTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.sec.ConfigSettings(newTestProvisioner(tt.volumes...))

			gotFactory := cfg[ServerCnxnFactory] == nettyFactory
			if gotFactory != tt.wantTLS {
				t.Errorf("%s = %q, want present=%v (cfg=%v)", ServerCnxnFactory, cfg[ServerCnxnFactory], tt.wantTLS, cfg)
			}
			gotX509 := cfg[SSLAuthProviderX509] != ""
			if gotX509 != tt.wantTLS {
				t.Errorf("%s present=%v, want %v", SSLAuthProviderX509, gotX509, tt.wantTLS)
			}
		})
	}
}
