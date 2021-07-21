#!/usr/bin/env bash
#
# Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

QUIC_CLIENT_IMAGE=ghcr.io/mvladev/quic-reverse-http-tunnel/quic-client-tcp:v0.1.2
QUIC_SERVER_IMAGE=ghcr.io/mvladev/quic-reverse-http-tunnel/quic-server:v0.1.2

QUIC_SECRET_NAME=quic-tunnel-certs
QUIC_CLIENT_CONTAINER=gardener-quic-client

CERTS_DIR=$(pwd)/tmp/certs

checkPrereqs() {
  command -v host > /dev/null || echo "please install host command for lookup"
  command -v docker > /dev/null || echo "please install docker https://www.docker.com"
}

createOrUpdateWebhookSVC(){
namespace=${1:-}
[[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1

providerName=${2:-}
[[ -z $providerName ]] && echo "Please specify the provider name (aws,gcp,azure,..etc.)!" && exit 1

local quicServerPort=${3:-}
[[ -z $quicServerPort ]] && echo "Please specify the quic pod server port!" && exit 1

tmpService=$(mktemp)
kubectl get svc gardener-extension-provider-$providerName -o yaml > $tmpService

    cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: gardener-extension-provider-$providerName
    app.kubernetes.io/instance: provider-$providerName
    app.kubernetes.io/name: gardener-extension-provider-$providerName
  name: gardener-extension-provider-$providerName
  namespace: $namespace
spec:
  ports:
  - port: 443
    protocol: TCP
    targetPort: $quicServerPort
  selector:
    app: quic-server
    app.kubernetes.io/instance: provider-$providerName
    app.kubernetes.io/name: gardener-extension-provider-$providerName
  type: ClusterIP
EOF
}


createQuicLB(){
namespace=${1:-}
[[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1

local quicTunnelPort=${2:-}
[[ -z $quicTunnelPort ]] && echo "Please specify the quic pod tunnel port!" && exit 1

cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: quic-lb
  name: quic-lb
  namespace: $namespace
spec:
  externalTrafficPolicy: Cluster
  ports:
  - name: quic-tunnel-port
    port: $quicTunnelPort
    protocol: UDP
    targetPort: $quicTunnelPort
  selector:
    app: quic-server
  type: LoadBalancer
EOF
}

waitForQuicLBToBeReady(){
    namespace=${1:-}
    [[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1

    providerName=${2:-}
    [[ -z $providerName ]] && echo "Please specify the provider name (aws,gcp,azure,..etc.)!" && exit 1

    # slightly different template for aws and everything else
    local template=""
    case $providerName in
    aws*)
      template="{{ index (index  .status.loadBalancer.ingress 0).hostname }}"
      ;;
    *)
      template="{{ index (index  .status.loadBalancer.ingress 0).ip }}"
      ;;
    esac
    until host $(kubectl -n $namespace get svc quic-lb -o go-template="${template}") 2>&1 > /dev/null
    do
      sleep 2s
    done
    echo $(kubectl -n $namespace get svc quic-lb -o go-template="${template}")
}

createServerDeploy(){
namespace=${1:-}
[[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1

providerName=${2:-}
[[ -z $providerName ]] && echo "Please specify the provider name (aws,gcp,azure,..etc.)!" && exit 1

local quicServerPort=${3:-}
[[ -z $quicServerPort ]] && echo "Please specify the quic pod server port!" && exit 1

local quicTunnelPort=${4:-}
[[ -z $quicTunnelPort ]] && echo "Please specify the quic pod tunnel port!" && exit 1

cat <<EOF | kubectl apply -f -
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: quic-server
    app.kubernetes.io/instance: provider-$providerName
    app.kubernetes.io/name: gardener-extension-provider-$providerName
    networking.gardener.cloud/to-dns: allowed
    networking.gardener.cloud/to-public-networks: allowed
  name: quic-server
  namespace: $namespace
spec:
  replicas: 1
  selector:
    matchLabels:
      app: quic-server
  template:
    metadata:
      labels:
        app: quic-server
        app.kubernetes.io/instance: provider-$providerName
        app.kubernetes.io/name: gardener-extension-provider-$providerName
    spec:
      containers:
      - args:
        - --listen-tcp=0.0.0.0:$quicServerPort
        - --listen-quic=0.0.0.0:$quicTunnelPort
        - --cert-file=/certs/tls.crt
        - --cert-key=/certs/tls.key
        - --client-ca-file=/certs/ca.crt
        image: "${QUIC_SERVER_IMAGE}"
        imagePullPolicy: IfNotPresent
        name: quic-server
        volumeMounts:
        - name: quic-tls
          mountPath: "/certs"
          readOnly: true
        resources:
          limits:
            cpu: 50m
            memory: 128Mi
          requests:
            cpu: 20m
            memory: 64Mi
      - args:
        - "sleep"
        - "8000s"
        image: alpine
        imagePullPolicy: IfNotPresent
        name: debug
        resources:
          limits:
            cpu: 50m
            memory: 128Mi
          requests:
            cpu: 20m
            memory: 64Mi
      volumes:
      - name: quic-tls
        secret:
          secretName: ${QUIC_SECRET_NAME}
      dnsPolicy: ClusterFirst
      enableServiceLinks: true
      restartPolicy: Always
EOF
}

waitForQuicDeployToBeReady(){
    namespace=${1:-}
    [[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1

    until test "$(kubectl -n $namespace get deploy quic-server --no-headers | awk '{print $2}')" = "1/1"
    do
      sleep 2s
    done
}

createCerts() {
  local dir=${1:-}
  [[ -z $dir ]] && echo "Please specify certs directory!" && exit 1

  mkdir -p $dir
(
  cd $dir

  cat > server.conf << EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = localhost
DNS.2 = quic-tunnel-server
IP.1 = 127.0.0.1
EOF

  cat > client.conf << EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

  # Create a certificate authority
  openssl genrsa -out ca.key 2048
  openssl req -x509 -new -nodes -key ca.key -days 100000 -out ca.crt -subj "/CN=quic-tunnel-ca"

  # Create a server certiticate
  openssl genrsa -out tls.key 2048
  openssl req -new -key tls.key -out server.csr -subj "/CN=quic-tunnel-server" -config server.conf
  openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out tls.crt -days 100000 -extensions v3_req -extfile server.conf

  # Create a client certiticate
  openssl genrsa -out client.key 2048
  openssl req -new -key client.key -out client.csr -subj "/CN=quic-tunnel-client" -config client.conf
  openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 100000 -extensions v3_req -extfile client.conf

  # Clean up after we're done.
  rm ./*.csr
  rm ./*.srl
  rm ./*.conf
)
}

loadCerts() {
  local certsDir=${1:-}
  local namespace=${2:-}
  local secret=${3:-}
  [[ -z $certsDir ]] && echo "Please specify local certs Dir!" && exit 1
  [[ -z $namespace ]] && echo "Please specify extension namespace!" && exit 1
  [[ -z $secret ]] && echo "Please specify webhook secret name!" && exit 1

  # if it already exists, we get rid of it
  kubectl -n $namespace delete secret $secret 2>/dev/null || true

  # now create it anew
  (
  cd $certsDir
  kubectl -n $namespace create secret generic $secret --from-file=ca.crt --from-file=tls.key --from-file=tls.crt
  )
}


cleanUP() {
   namespace=${1:-}
   [[ -z $namespace ]] && echo "Please specify the extension namespace!" && exit 1

   echo "cleaning up local-dev setup.."

   echo "Deleting quic service..."
   kubectl -n $namespace delete  svc/quic-lb

   echo "Deleting the quic deploy..."
   kubectl -n $namespace delete  deploy/quic-server

   echo "Deleting the quic certs..."
   kubectl -n $namespace delete  secret/quic-tunnel-certs

   echo "Re-applying old service values..."
   kubectl apply -f $tmpService

   docker kill $QUIC_CLIENT_CONTAINER
   exit 0
}

usage(){
  echo "==================================================================DISCLAIMER============================================================================"
  echo "This scripts needs to be run against the KUBECONFIG of a seed cluster, please set your KUBECONFIG accordingly"
  echo "You also need to set the \`ignoreResources\` variable in your extension chart to \`true\`, generate and apply the corresponding controller-installation"
  echo "========================================================================================================================================================"

  echo ""

  echo "===================================PRE-REQs========================================="
  echo "\`host\` commands for DNS"
  echo "\`docker\` https://www.docker.com"
  echo "===================================================================================="

  echo ""

  echo "========================================================USAGE======================================================================"
  echo "> ./hack/hook-me.sh <provider e.g., aws> <extension namespace e.g. extension-provider-aws-fpr6w> <webhookserver port e.g., 8443> [<quic-server port, e.g. 9443>]"
  echo "> \`make EXTENSION_NAMESPACE=<extension namespace e.g. extension-provider-aws-fpr6w> WEBHOOK_CONFIG_MODE=service start\`"
  echo "=================================================================================================================================="

  echo ""

  echo "===================================CLEAN UP COMMANDS========================================="
  echo ">  kubectl -n $namespace delete  svc/quic-lb"
  echo ">  kubectl -n $namespace delete  deploy/quic-server"
  echo "============================================================================================="

  exit 0
}
if [[ "${BASH_SOURCE[0]}" = "$0" ]]; then

  if [ "$1" == "-h" ] ; then
        usage
  fi

  providerName=${1:-}
  [[ -z $providerName ]] && echo "Please specify the provider name (aws,gcp,azure,..etc.)!" && exit 1

  namespace=${2:-}
  [[ -z $namespace ]] && echo "Please specify the extension namespace!" && exit 1

  webhookServerPort=${3:-}
  [[ -z $webhookServerPort ]] && echo "Please specify webhook server port" && exit 1

  quicServerPort=${4:-}
  [[ -z $quicServerPort ]] && echo "quic-server port not specified, using default port of 9443" && quicServerPort=9443

  quicTunnelPort=${5:-}
  [[ -z $quicTunnelPort ]] && echo "quic-tunnel port not specified, using default port of 9444" && quicTunnelPort=9444


  trap 'cleanUP $namespace' SIGINT SIGTERM

  while true; do
    read -p "[STEP 0] Have you already set the \`ignoreResources\` chart value to \`true\` for your extension controller-registration?" yn
    case $yn in
        [Yy]* )
            echo "[STEP 1] Checking Pre-reqs!"
            checkPrereqs

            echo "[STEP 2] Creating Quic LB Service..!"
            createQuicLB $namespace $quicTunnelPort && sleep 2s

            echo "[STEP 3] Waiting for Quic LB Service to be created..!";
            output=$(waitForQuicLBToBeReady $namespace $providerName)
            loadbalancerIPOrHostName=$(echo "$output" | tail -n1)
            echo "[Info] LB IP is $loadbalancerIPOrHostName"

            echo "[STEP 4] Creating the CA, client and server keys and certs..!";
            createCerts $CERTS_DIR

            echo "[STEP 5] Loading quic tunnel certs into cluster..!";
            loadCerts $CERTS_DIR $namespace $QUIC_SECRET_NAME

            echo "[STEP 6] Creating the server Deploy for TLS Termination and Tunneling connection..!";
            createServerDeploy $namespace $providerName $quicServerPort $quicTunnelPort

            echo "[STEP 7] Waiting for Quic Deploy to be ready..!";
            waitForQuicDeployToBeReady $namespace

            echo "[STEP 8] Creating WebhookSVC LB..!"
            createOrUpdateWebhookSVC $namespace $providerName $quicServerPort

            echo "[STEP 9] Initializing the quic client";
            echo "[Info] Quic initialized, you are ready to go ahead and run \"make EXTENSION_NAMESPACE=$namespace WEBHOOK_CONFIG_MODE=service start\""
            echo "[Info] It will take about 5 seconds for the connection to succeeed!"

            echo "[Step 10] Running quic client"
            docker run \
              --name ${QUIC_CLIENT_CONTAINER} \
              --rm \
              -v "$CERTS_DIR":/certs \
              $QUIC_CLIENT_IMAGE \
              --server="$loadbalancerIPOrHostName:$quicTunnelPort" \
              --upstream="host.docker.internal:$webhookServerPort" \
              --ca-file=/certs/ca.crt \
              --cert-file=/certs/client.crt \
              --cert-key=/certs/client.key
        ;;
        [Nn]* ) echo "You need to set  \`ignoreResources\` to true and generate the controller installlation first in your extension chart before proceeding!"; exit;;
        * ) echo "Please answer yes or no.";;
    esac
done
fi
