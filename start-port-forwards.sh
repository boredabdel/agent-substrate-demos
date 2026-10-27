#!/usr/bin/env bash
# Helper script for the Agent-Substrate Live Demos
# Usage: source ./start-port-forwards.sh

export PATH="/opt/homebrew/bin:/Users/abdelfettah/.local/bin:/Users/abdelfettah/data/bin/google-cloud-sdk/bin:$HOME/go/bin:$PATH"
if [ -f "/Users/abdelfettah/data/code/agent-substrate/substrate/.ate-dev-env.sh" ]; then
  source "/Users/abdelfettah/data/code/agent-substrate/substrate/.ate-dev-env.sh"
fi

echo "Stopping any existing demo port-forwards..."
pkill -f "kubectl.*port-forward.*(8000:80|8001:8081|4040:4040|8888:80|7777:7777)" 2>/dev/null || true
sleep 1

echo "Starting background port-forwards to GKE cluster ($CLUSTER_NAME)..."
kubectl port-forward -n ate-system svc/atenet-router 8000:80 >/tmp/pf-atenet-http.log 2>&1 &
kubectl port-forward -n ate-system svc/atenet-router 8001:8081 >/tmp/pf-atenet-connect.log 2>&1 &
kubectl port-forward -n ate-system deployment/atenet-router 4040:4040 >/tmp/pf-atenet-statusz.log 2>&1 &
kubectl port-forward --address 0.0.0.0 -n ate-demo-jupyter svc/jupyter-proxy 8888:80 >/tmp/pf-jupyter.log 2>&1 &
kubectl port-forward -n ate-env svc/ate-env-api 7777:7777 >/tmp/pf-ate-env.log 2>&1 &
sleep 2

export TARGET_IP=$(kubectl -n egress-target get svc whoami -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
export RUNNER_GATEWAY_IP=$(kubectl -n ate-demo-github get svc runner-gateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null)

echo "=================================================================="
echo "Agent-Substrate Demo Environment Ready!"
echo "  GKE Cluster         : $CLUSTER_NAME ($ZONE, project: $PROJECT_ID)"
echo "  GCS Snapshot Bucket : gs://$BUCKET_NAME"
echo "  atenet-router HTTP  : http://localhost:8000"
echo "  atenet-router PROXY : http://localhost:8001 (HTTP CONNECT)"
echo "  atenet-router STATUS: http://localhost:4040/statusz"
echo "  Jupyter Notebook    : http://localhost:8888/lab"
echo "  ate-env API & MCP   : http://localhost:7777"
echo "  Egress Target IP    : ${TARGET_IP}"
echo "  GitHub Webhook GW   : http://${RUNNER_GATEWAY_IP}/webhook"
echo "  GitHub Demo Repo    : https://github.com/boredabdel/substrate-pr-review-demo"
echo "=================================================================="
