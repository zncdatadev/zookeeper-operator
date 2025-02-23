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
# Initialize retry counter
retry_count=0
max_retries=2
retry_delay=5

# Try connection with retries
echo "Testing unsecure connection..."
while [ $retry_count -le $max_retries ]; do
    if /kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / &> /dev/null; then
        echo "[SUCCESS] Unsecure client connection established!"
        break
    else
        retry_count=$((retry_count + 1))
        if [ $retry_count -le $max_retries ]; then
            echo "[WARN] Could not establish unsecure connection! Retrying in ${retry_delay} seconds... (Attempt ${retry_count}/${max_retries})"
            sleep $retry_delay
        else
            echo "[ERROR] Could not establish unsecure connection after ${max_retries} retries!"
            exit 1
        fi
    fi
done

############################################################################
# We set the correct client tls credentials and expect to be able to connect
############################################################################
echo "Testing secure connection with client certificates..."
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

output=$(/kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / 2>&1)
if [ $? -ne 0 ]; then
  echo "[ERROR] Could not establish secure connection using client certificates!"
  echo "Command output:"
  echo "----------------------------------------"
  echo "$output"
  echo "----------------------------------------"
  exit 1
fi
echo "[SUCCESS] Secure and authenticated client connection established!"

############################################################################
# We set the (wrong) quorum tls credentials and expect to fail (wrong certificate)
############################################################################
echo "Testing secure connection with quorum certificates..."
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

output=$(/kubedoop/zookeeper/bin/zkCli.sh -server "${SERVER}" ls / 2>&1)
if [ $? -ne 0 ]; then
then
  echo "[ERROR] Could establish secure connection with quorum certificates (should not be happening)!"
  echo "Command output:"
  echo "----------------------------------------"
  echo "$output"
  echo "----------------------------------------"
  exit 1
fi
echo "[SUCCESS] Could not establish secure connection with (wrong) quorum certificates!"


echo "All TLS tests successful!"
exit 0
