#!/usr/bin/env bash
# Reset all Agent-Substrate demo actors to a clean starting state before the talk
set -euo pipefail

export PATH="/opt/homebrew/bin:/Users/abdelfettah/.local/bin:/Users/abdelfettah/data/bin/google-cloud-sdk/bin:$HOME/go/bin:$PATH"
source "/Users/abdelfettah/data/code/agent-substrate/substrate/.ate-dev-env.sh"

echo "1. Resetting Demo 1 (my-counter-1 in ate-demo-counter)..."
kubectl ate delete actor my-counter-1 -a ate-demo-counter --any-state 2>/dev/null || true
kubectl ate create actor my-counter-1 -a ate-demo-counter --template counter

echo "2. Resetting Demo 2 (my-sandbox-1 in ate-demo-sandbox)..."
kubectl ate delete egress-policy my-sandbox-1 -a ate-demo-sandbox 2>/dev/null || true
kubectl ate delete actor my-sandbox-1 -a ate-demo-sandbox --any-state 2>/dev/null || true
kubectl ate create actor my-sandbox-1 -a ate-demo-sandbox --template sandbox-template
kubectl ate create egress-policy my-sandbox-1 -a ate-demo-sandbox -f - <<'EOF'
rules:
- http:
    hostnames: ["*"]
    ports: {all: {}}
    effects:
      replaceHeaders:
      - header: Authorization
        prefix: "Bearer "
        credentialUri: ate-secret://k8s.io/default/ns1/example-api/token
- https:
    hostnames: ["*"]
EOF

echo "3. Resetting Demo 3 (jupyter-notebook in ate-demo-jupyter)..."
kubectl ate delete actor jupyter-notebook -a ate-demo-jupyter --any-state 2>/dev/null || true
kubectl ate create actor jupyter-notebook -a ate-demo-jupyter --template jupyter

echo "4. Resetting Demo 4 (c1 and f1 in shared-pool atespaces)..."
kubectl ate delete actor c1 -a ate-demo-multi-template-counter --any-state 2>/dev/null || true
kubectl ate delete actor f1 -a ate-demo-multi-template-fspersist --any-state 2>/dev/null || true
kubectl ate create actor c1 -a ate-demo-multi-template-counter --template counter
kubectl ate create actor f1 -a ate-demo-multi-template-fspersist --template fspersist

echo "5. Resetting Demo 5 (p1..p4 in ate-demo-parking)..."
for id in p1 p2 p3 p4; do
  kubectl ate delete actor "$id" -a ate-demo-parking --any-state 2>/dev/null || true
  kubectl ate create actor "$id" -a ate-demo-parking --template parking
done

echo "6. Resetting Demo 6 (egress-demo in ate-demo-egress & dev1 in ate-env)..."
kubectl ate delete egress-policy egress-demo -a ate-demo-egress 2>/dev/null || true
kubectl ate delete actor egress-demo -a ate-demo-egress --any-state 2>/dev/null || true
kubectl ate create actor egress-demo -a ate-demo-egress --template egress

kubectl ate delete actor dev1 -a ate-env --any-state 2>/dev/null || true
kubectl ate create actor dev1 -a ate-env --template default-template

echo "7. Ensuring Demo 7 (gh-runner-1 in ate-demo-github) is suspended and ready..."
kubectl ate suspend actor gh-runner-1 -a ate-demo-github 2>/dev/null || true

echo "All demo actors reset to clean ACTOR_STATE_SUSPENDED!"
kubectl ate get actors -A
