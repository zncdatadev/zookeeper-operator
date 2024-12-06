#!/usr/bin/env bash
# Usage: test_tls.sh namespace

NAMESPACE=$1

SERVER="test-zk-server-default-1.test-zk-server-default.${NAMESPACE}.svc.cluster.local:2282"

# just to be safe...
unset QUORUM_STORE_SECRET
unset CLIENT_STORE_SECRET
unset CLIENT_JVMFLAGS

echo "Start TLS testing..."
############################################################################
# Test the plaintext unsecured connection
############################################################################
if ! /kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / &> /dev/null;
then
  echo "[ERROR] Could not establish unsecure connection!"
  exit 1
fi
echo "[SUCCESS] Unsecure client connection established!"

############################################################################
# We set the correct client tls credentials and expect to be able to connect
############################################################################
CLIENT_STORE_SECRET="$(< /kubedoop/config/zoo.cfg grep "ssl.keyStore.password" | cut -d "=" -f2)"
export CLIENT_STORE_SECRET
export CLIENT_JVMFLAGS="
-Dzookeeper.authProvider.x509=org.apache.zookeeper.server.auth.X509AuthenticationProvider
-Dzookeeper.clientCnxnSocket=org.apache.zookeeper.ClientCnxnSocketNetty
-Dzookeeper.client.secure=true
-Dzookeeper.ssl.keyStore.location=/kubedoop/server_tls/keystore.p12
-Dzookeeper.ssl.keyStore.password=${CLIENT_STORE_SECRET}
-Dzookeeper.ssl.trustStore.location=/kubedoop/server_tls/truststore.p12
-Dzookeeper.ssl.trustStore.password=${CLIENT_STORE_SECRET}"

if ! /kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / &> /dev/null;
then
  echo "[ERROR] Could not establish secure connection using client certificates!"
  exit 1
fi
echo "[SUCCESS] Secure and authenticated client connection established!"

############################################################################
# We set the (wrong) quorum tls credentials and expect to fail (wrong certificate)
############################################################################
QUORUM_STORE_SECRET="$(< /kubedoop/config/zoo.cfg grep "ssl.quorum.keyStore.password" | cut -d "=" -f2)"
export QUORUM_STORE_SECRET
export CLIENT_JVMFLAGS="
-Dzookeeper.authProvider.x509=org.apache.zookeeper.server.auth.X509AuthenticationProvider
-Dzookeeper.clientCnxnSocket=org.apache.zookeeper.ClientCnxnSocketNetty
-Dzookeeper.client.secure=true
-Dzookeeper.ssl.keyStore.location=/kubedoop/quorum_tls/keystore.p12
-Dzookeeper.ssl.keyStore.password=${QUORUM_STORE_SECRET}
-Dzookeeper.ssl.trustStore.location=/kubedoop/quorum_tls/truststore.p12
-Dzookeeper.ssl.trustStore.password=${QUORUM_STORE_SECRET}"

if /kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / &> /dev/null;
then
  echo "[ERROR] Could establish secure connection with quorum certificates (should not be happening)!"
  exit 1
fi
echo "[SUCCESS] Could not establish secure connection with (wrong) quorum certificates!"

echo "All TLS tests successful!"
exit 0
