# Agent-Substrate: Talk & Live Demo Guide

This guide documents the complete live demo environment deployed in GCP project **`boredabdel-lab`** for your talk on **[Agent-Substrate](https://github.com/agent-substrate/substrate)**, along with a presenter runbook containing copy-pasteable commands, expected outputs, and talking points.

---

## 1. Talk Narrative & Core Architecture

### The Problem Agent-Substrate Solves
AI agents need isolated, stateful execution environments (sandboxes, code interpreters, browser/dev environments, long-lived personal assistants). Standard Kubernetes primitives force an uncomfortable trade-off:
- **Keep pods running 24/7**: Prohibitively expensive when 95%+ of agent sessions are idle waiting for user input or LLM thinking turns.
- **Cold-start pods on demand**: Too slow (seconds to tens of seconds for container pull + language runtime + package initialization) and loses in-memory state.

### How Agent-Substrate Solves It
Agent-Substrate introduces the **Stateful Actor Model** on top of Kubernetes:
1. **Golden Snapshots (`ActorTemplate`)**: When you register an `ActorTemplate`, Substrate boots a hidden `_golden` actor once, waits for its `wakeupProbe` (`/readyz`), and snapshots its initialized memory and filesystem to GCS.
2. **Pre-Warmed `WorkerPool`s (`ateom-gvisor` / `ateom-microvm`)**: Generic worker pods sit ready on nodes. An `Actor` is **not** a Kubernetes Pod—it is a snapshot-backed sandbox multiplexed onto a `WorkerPool` when active and evicted to GCS when idle.
3. **Sub-Second Teleport (`Suspend` / `Resume`)**: `atelet` checkpoints both **process RAM** (`SNAPSHOT_CONTENT_SCOPE_FULL`) and **durable volumes** (`durableDir`) to GCS and restores them onto *any* available worker pod in hundreds of milliseconds.
4. **Demand-Driven Ingress (`atenet-router`)**: Inbound HTTP/gRPC/WebSocket requests carrying `ate-target-actor: <atespace>/<actor>` automatically wake suspended actors, or **park** the request if the `WorkerPool` is momentarily saturated.
5. **Zero-Trust Identity & Egress (`atenet-egress` + `k8s-credential-provider`)**: Every running actor receives a unique X.509 SPIFFE certificate minted by `ate-api-server`. Outbound traffic is transparently intercepted via `nftables` + `atunnel`, authenticated by actor UID at `atenet-egress`, checked against per-actor `EgressPolicy`, and can have API secrets injected on the wire without the actor ever holding the secret.

```
                         ┌──────────────────────────────┐
                         │      ate-api-server          │
                         │  (Actors, Templates, Certs)  │
                         └──────────────┬───────────────┘
                                        │ gRPC (mTLS)
     Inbound HTTP/WS     ┌──────────────▼───────────────┐     Restore / Run
  ──────────────────────►│        atenet-router         ├───────────────────────┐
  ate-target-actor:      │ (Auto-Resume & Req Parking)  │                       │
  <atespace>/<actor>     └──────────────────────────────┘                       ▼
                                                                 ┌──────────────────────────────┐
                                                                 │   WorkerPool Pod (gVisor)    │
                                                                 │  ┌────────────────────────┐  │
                                                                 │  │     Actor Sandbox      │  │
                                                                 │  │  (RAM + durableDir)    │  │
                                                                 │  └───────────┬────────────┘  │
                                                                 │              │ nftables      │
                                                                 │              ▼               │
                                                                 │      atunnel (Actor mTLS)    │
                                                                 └──────────────┬───────────────┘
                                                                                │ CONNECT (mTLS)
                         ┌──────────────────────────────┐                       ▼
                         │   k8s-credential-provider    │◄───────┌──────────────────────────────┐
                         │ (Secret-less Header Inject)  │  mTLS  │        atenet-egress         │
                         └──────────────────────────────┘        │ (SPIFFE Auth + EgressPolicy) │
                                                                 └──────────────┬───────────────┘
                                                                                │
                                                                                ▼
                                                                       Upstream Destination
```

---

## 2. Live GCP / GKE Environment Details

Everything is deployed and verified in your GCP project:

| Resource | Value |
| :--- | :--- |
| **GCP Project** | `boredabdel-lab` (Project Number: `902903787694`) |
| **GKE Cluster** | `agent-substrate-demo` (`us-west1-c`, GKE `1.36.4-gke.2046000`) |
| **Compute Node Pool** | `substrate-node-pool` (2x `c3-standard-8`, nested virtualization enabled, node auto-upgrade disabled) |
| **Networking & Auth** | Dataplane V2 enabled, Workload Identity enabled, K8s `PodCertificateRequest` & `ClusterTrustBundle` beta APIs enabled |
| **GCS Snapshot Bucket** | `gs://snapshot-substrate-demo-boredabdel-lab` |
| **Container Registry** | `gcr.io/boredabdel-lab/ate-images/*` |
| **Substrate Version** | `v0.3.0-113-g0b91488d` |
| **Cloud Monitoring** | 3 dashboards deployed: `Substrate gRPC Server — latency / QPS / errors`, `Substrate Routing & E2E Latency`, `Substrate Snapshot Size & QPS` |

### Deployed Namespaces, WorkerPools & ActorTemplates

| Demo | K8s Namespace (`WorkerPool`) | Substrate `Atespace` / `ActorTemplate` | Pre-Created Actor(s) |
| :--- | :--- | :--- | :--- |
| **Core Control Plane** | `ate-system`, `podcertificate-controller-system` | `ate-golden` | `_golden` (per template) |
| **Demo 1: Counter & Teleport** | `ate-demo-counter` (`counter`, 3 replicas) | `ate-demo-counter/counter` | `my-counter-1` |
| **Demo 2: Alpine Sandbox REPL** | `ate-demo-sandbox` (`sandbox-workerpool`, 2 replicas) | `ate-demo-sandbox/sandbox-template` | `my-sandbox-1` |
| **Demo 3: Unmodified JupyterLab** | `ate-demo-jupyter` (`jupyter`, 1 replica + `jupyter-proxy`) | `ate-demo-jupyter/jupyter` | `jupyter-notebook` |
| **Demo 4: Multi-Tenant Pool** | `ate-demo-multi-template-pool` (`shared-pool`, 3 replicas) | `ate-demo-multi-template-counter/counter`<br>`ate-demo-multi-template-fspersist/fspersist` | `c1`, `f1` |
| **Demo 5: Request Parking** | `ate-demo-parking` (`parking`, 2 replicas) | `ate-demo-parking/parking` | `p1`, `p2`, `p3`, `p4` |
| **Demo 6A: Zero-Trust Egress** | `ate-demo-egress` (`egress`, 2 replicas) + `egress-target` (`whoami`) | `ate-demo-egress/egress` | `egress-demo` |
| **Demo 6B: Agent Env & MCP** | `ate-env` (`default-template-workerpool`, 2 replicas + `ate-env-api`) | `ate-env/default-template` | `dev1` |
| **Demo 7: GitHub Actions PR Review** | `ate-demo-github` (`github-runner`, 1 replica + `runner-gateway` LB) | `ate-demo-github/github-runner` | `gh-runner-1` |

---

## 3. Pre-Talk Setup (Run 5 Minutes Before Going on Stage)

From `/Users/abdelfettah/data/code/agent-substrate`, run these two helper scripts:

```bash
cd /Users/abdelfettah/data/code/agent-substrate

# 1. Reset all demo actors to a fresh, clean ACTOR_STATE_SUSPENDED state
./reset-demos.sh

# 2. Export PATH/env vars and start all background port-forwards
source ./start-port-forwards.sh
```

Verify the cluster and actors are ready to present:

```bash
# Show control plane pods
kubectl get pods -n ate-system

# Show all pre-warmed WorkerPools
kubectl get workerpools -A

# Show all ActorTemplates and their Golden Snapshot tags
kubectl ate get actor-templates -A

# Show all pre-created Demo Actors (all starting in ACTOR_STATE_SUSPENDED, consuming 0 workers!)
kubectl ate get actors -A
```

kubectl ate get actor-template counter --atespace ate-demo-counter -o yaml

---

## 4. Live Demo Runbook (7 Demos)

### Demo 1: Stateful Counter & Actor Teleport (RAM + Durable Volume Persistence)

**Goal**: Show that an actor starts in `ACTOR_STATE_SUSPENDED` (consuming zero compute), wakes up transparently on the first HTTP request, preserves **both** volatile in-memory variables and durable disk files across suspend/resume cycles, and supports arbitrary-port ingress via HTTP `CONNECT`.

```bash
# 1. Confirm my-counter-1 is currently SUSPENDED (no worker pod assigned)
kubectl ate get actor my-counter-1 -a ate-demo-counter

# 2. Send 3 HTTP POST requests through atenet-router.
#    The first request automatically restores my-counter-1 from the Golden Snapshot!
curl -s -X POST -H "ate-target-actor: ate-demo-counter/my-counter-1" http://localhost:8000
curl -s -X POST -H "ate-target-actor: ate-demo-counter/my-counter-1" http://localhost:8000
curl -s -X POST -H "ate-target-actor: ate-demo-counter/my-counter-1" http://localhost:8000
```

*Expected output*:
```text
hello from: 169.254.17.2 | preserved memory count: 0 | preserved file counter: 0
hello from: 169.254.17.2 | preserved memory count: 1 | preserved file counter: 1
hello from: 169.254.17.2 | preserved memory count: 2 | preserved file counter: 2
```

```bash
# 3. Inspect the actor — it is now ACTOR_STATE_RUNNING on a worker pod
kubectl ate get actor my-counter-1 -a ate-demo-counter

# 4. Also reach its non-default port (:9090) via atenet-router's HTTP CONNECT tunnel (port 8001)
curl -s -p -x http://localhost:8001 \
  --proxy-header "ate-target-actor: ate-demo-counter/my-counter-1" \
  http://my-counter-1:9090/

# 5. Suspend the actor back to Google Cloud Storage (memory + disk checkpointed)
kubectl ate suspend actor my-counter-1 -a ate-demo-counter
kubectl ate get actor my-counter-1 -a ate-demo-counter

# 6. Send another POST request — it resumes in ~200ms and continues from count=3!
curl -s -X POST -H "ate-target-actor: ate-demo-counter/my-counter-1" http://localhost:8000
```

*Expected output*:
```text
hello from: 169.254.17.2 | preserved memory count: 3 | preserved file counter: 3
```
> **Talking Point**: Notice that `preserved memory count` is a plain Go `int` variable in RAM—never written to disk by the app. Because the template uses `SNAPSHOT_CONTENT_SCOPE_FULL`, gVisor checkpoints the process memory pages directly to GCS.

---

### Demo 2: Interactive Alpine Sandbox REPL (`demo-sandbox`)

**Goal**: Show an interactive shell session inside a gVisor-isolated Alpine container (`my-sandbox-1`) where filesystem modifications survive across suspend and resume.

```bash
# 1. Connect with the interactive REPL client (auto-resumes my-sandbox-1 on start, suspends on exit)
./substrate/bin/sandbox-client --atenet=localhost:8000 --atespace=ate-demo-sandbox --name=my-sandbox-1
```

Inside the `sandbox>` prompt, type:
```text
sandbox> uname -a
sandbox> echo "Live from Agent-Substrate Talk!" > /root/talk-notes.txt
sandbox> ls -la /root/talk-notes.txt
sandbox> exit
```

```bash
# 2. Confirm exiting the REPL automatically suspended the actor back to GCS
kubectl ate get actor my-sandbox-1 -a ate-demo-sandbox

# 3. Reconnect (or send a one-liner) and verify /root/talk-notes.txt is still there!
printf 'cat /root/talk-notes.txt\nexit\n' | ./substrate/bin/sandbox-client --atenet=localhost:8000 --atespace=ate-demo-sandbox --name=my-sandbox-1
```

*Expected output*:
```text
Live from Agent-Substrate Talk!
```

---

### Demo 3: Unmodified Container Image — Serverless JupyterLab (`demo-jupyter`)

**Goal**: Show that you don't need a custom SDK or binary to run on Agent-Substrate. Stock `jupyter/base-notebook` runs unmodified behind a lightweight NGINX reverse proxy (`jupyter-proxy`) that injects `ate-target-actor: ate-demo-jupyter/jupyter-notebook`.

```bash
# 1. Check that jupyter-notebook is currently SUSPENDED
kubectl ate get actor jupyter-notebook -a ate-demo-jupyter

# 2. Open http://localhost:8888/lab in your browser (or curl it)
curl -s -o /dev/null -w "HTTP %{http_code} (activated in %{time_total}s)\n" -L http://localhost:8888/lab
open http://localhost:8888/lab

# 3. Show that the browser navigation woke up the actor onto the jupyter WorkerPool
kubectl ate get actor jupyter-notebook -a ate-demo-jupyter

# 4. In JupyterLab, create a Python 3 notebook, set `x = 42`, and print `x`.
#    Then suspend the notebook from the terminal while the browser tab stays open:
kubectl ate suspend actor jupyter-notebook -a ate-demo-jupyter
kubectl ate get actor jupyter-notebook -a ate-demo-jupyter

# 5. Refresh the browser page (or run the next cell `print(x + 1)`):
#    atenet-router transparently wakes the actor from its memory+disk snapshot!
```

---

### Demo 4: Multi-Tenant `WorkerPool` Sharing Across Atespaces (`demo-multi-template`)

**Goal**: Show that `WorkerPool`s are generic sandbox hosts: two completely different `ActorTemplate`s (`counter` and `fspersist`) living in two different tenant `atespace`s share a single `WorkerPool` (`shared-pool`) via label selector (`workerSelector`).

```bash
# 1. Inspect the two templates in different atespaces
kubectl ate get actor-templates -a ate-demo-multi-template-counter
kubectl ate get actor-templates -a ate-demo-multi-template-fspersist

# 2. Wake up actor c1 (counter binary) and actor f1 (filesystem history binary)
curl -s -H "ate-target-actor: ate-demo-multi-template-counter/c1" http://localhost:8000
curl -s -H "ate-target-actor: ate-demo-multi-template-fspersist/f1" http://localhost:8000

# 3. Show that both actors landed on workers inside the same `shared-pool`
kubectl ate get workers | grep -E "NAME|shared-pool"
kubectl ate get actors -a ate-demo-multi-template-counter
kubectl ate get actors -a ate-demo-multi-template-fspersist

# 4. Suspend f1 and hit it again — its file history persists across restore
kubectl ate suspend actor f1 -a ate-demo-multi-template-fspersist
curl -s -H "ate-target-actor: ate-demo-multi-template-fspersist/f1" http://localhost:8000
```

*Expected output*:
```text
pod: 169.254.17.2
--- history ---
pod=169.254.17.2 | count=1 | time=2026-10-07T...
pod=169.254.17.2 | count=0 | time=2026-10-07T...
```

  ---

  ### Demo 5: Request Parking Under Worker Pool Saturation (`demo-parking`)

  **Goal**: Show what happens when you have more active actors (4 actors: `p1`, `p2`, `p3`, `p4`) than pre-warmed workers (2 workers in `ate-demo-parking`). Instead of failing fast with `HTTP 503`, `atenet-router` **parks** the inbound request and retries until a worker frees up.

```bash
# 1. Wake p1 and p2 — this completely fills all 2 workers in the `parking` pool
curl -s -H "ate-target-actor: ate-demo-parking/p1" http://localhost:8000
curl -s -H "ate-target-actor: ate-demo-parking/p2" http://localhost:8000

# 2. Confirm p1 & p2 are RUNNING and p3 & p4 are SUSPENDED (0 free workers left in parking pool)
kubectl ate get actors -a ate-demo-parking
kubectl ate get workers | grep -E "NAME|parking"

# 3. Request p3 while the pool is 100% full, and free a worker 1.5s later in the background.
#    Notice atenet-router holds (parks) the request for ~1.5s and then returns HTTP 200!
(sleep 1.5 && kubectl ate suspend actor p1 -a ate-demo-parking >/dev/null 2>&1) &
curl -s -w '\n-> HTTP %{http_code} in %{time_total}s (Request parked until worker freed!)\n' \
  -H "ate-target-actor: ate-demo-parking/p3" http://localhost:8000

# 4. Inspect the router's live /statusz parking configuration
curl -s 'http://localhost:4040/statusz?format=json' | jq .parking
```

*Expected output*:
```json
{
  "enabled": true,
  "active": 0,
  "max_parked": 1024,
  "max_wait": "5s"
}
```

*(Optional)* Run the automated contention benchmark (`./substrate/demos/parking/load.sh` for 15s across 4 concurrent actors competing for 2 workers):
```bash
DURATION=15 PORT=8000 ./substrate/demos/parking/load.sh
```

---

### Demo 6: Zero-Trust Egress, Secret-Less Credential Injection & Agent Environment MCP

This finale combines three flagship security and AI-agent capabilities:

#### Part A: Zero-Trust Outbound Egress & Per-Actor SPIFFE Identity (`demo-egress`)

**Goal**: Show that actors have **default-deny egress**. Even though the actor dials plain HTTP (`http://$TARGET_IP:80/`), `atunnel` intercepts the connection, wraps it in mTLS using the actor's X.509 SPIFFE certificate, and `atenet-egress` enforces the actor's `EgressPolicy`.

```bash
# 1. Resume egress-demo (note: reset-demos.sh removed its EgressPolicy)
kubectl ate resume actor egress-demo -a ate-demo-egress

# 2. Try to fetch the in-cluster whoami target WITHOUT an EgressPolicy -> Denied (HTTP 502)!
curl -s -w '\n-> HTTP %{http_code}\n' -X POST http://localhost:8000/ \
  -H 'ate-target-actor: ate-demo-egress/egress-demo' \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"http://${TARGET_IP}:80/\"}"

# 3. Apply an EgressPolicy for egress-demo
kubectl ate create egress-policy egress-demo -a ate-demo-egress -f - <<'EOF'
rules:
- http: {hostnames: ["*"], ports: {all: {}}}
- https: {hostnames: ["*"]}
EOF
kubectl ate get egress-policy egress-demo -a ate-demo-egress

# 4. Wait 10s for the negative-policy cache TTL to expire, then fetch again -> HTTP 200!
sleep 10
curl -s -X POST http://localhost:8000/ \
  -H 'ate-target-actor: ate-demo-egress/egress-demo' \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"http://${TARGET_IP}:80/\"}" | jq .

# 5. Inspect atenet-egress Envoy & ext-proc logs to show the verified Actor SPIFFE SAN!
kubectl -n ate-system logs deploy/atenet-egress -c envoy --tail=10 | grep '\[egress\]'
kubectl -n ate-system logs deploy/atenet-egress -c ext-proc --tail=10 | grep -i 'egress'
```

*Expected Envoy log*:
```text
[egress] authority=34.118.229.252:80 peer_san=spiffe://substrate-actor.local/ateom-for-actor/ate-demo-egress/egress-demo ... code=200
```

#### Part B: Secret-Less Egress Credential Injection (`k8s-credential-provider`)

**Goal**: Show how an AI agent inside an actor can call authenticated external APIs **without ever holding the API key in its env vars, filesystem, or memory snapshots**.

We configured `k8s-credential-provider` with Secret `ns1/example-api` (`token: test-github-token`) and granted `ate-demo-sandbox` permission to resolve Secrets in `ns1`.

```bash
# 1. Inspect the EgressPolicy on my-sandbox-1 (or apply it if reset):
kubectl ate create egress-policy my-sandbox-1 -a ate-demo-sandbox -f - <<'EOF' 2>/dev/null || true
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
kubectl ate get egress-policy my-sandbox-1 -a ate-demo-sandbox -o yaml

# 2. Inside my-sandbox-1, run wget sending `Authorization: placeholder`.
#    Look at the echoed headers from `whoami` — the gateway replaced `placeholder`
#    with `Bearer test-github-token` on the wire!
curl -s -X POST http://localhost:8000/process \
  -H "ate-target-actor: ate-demo-sandbox/my-sandbox-1" \
  -H "Content-Type: application/json" \
  -d "{\"command\":[\"sh\",\"-c\",\"wget -qO- --header='Authorization: placeholder' http://${TARGET_IP}:80/\"]}" | jq -r .stdout
```

*Expected output*:
```text
Hostname: whoami-7fd8895794-sl7jm
IP: 127.0.0.1
IP: 10.88.1.21
RemoteAddr: 10.88.0.23:52256
GET / HTTP/1.1
Host: 34.118.229.252:80
User-Agent: Wget
Accept: */*
Authorization: Bearer test-github-token
X-Forwarded-Proto: http
```

#### Part C: Agent Environment Service & Streamable MCP Server (`ate-env`)

**Goal**: Show [`agent-substrate/env`](https://github.com/agent-substrate/env) (`ate-env`), the higher-level Agentic Environment service built on Substrate that gives AI agents a rich CLI, Go/Python SDKs, and a built-in **Model Context Protocol (MCP)** server (`/v1alpha/envs/{id}/mcp`) with `shell`, `read_file`, `write_file`, `edit_file`, `glob`, and `grep` tools.

```bash
# 1. Execute a shell command and write a file inside environment `dev1` via `ate-env` CLI
ate-env dev1 shell 'echo "Created via ate-env CLI" > /workspace_note.txt && uname -a'
ate-env dev1 read /workspace_note.txt

# 2. List the built-in MCP tools exposed at http://localhost:7777/v1alpha/envs/dev1/mcp
curl -s -X POST http://localhost:7777/v1alpha/envs/dev1/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[] | {name, description}'

# 3. Call the MCP `shell` tool over JSON-RPC (just like Claude/Gemini/ADK agents do)
curl -s -X POST http://localhost:7777/v1alpha/envs/dev1/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "shell",
      "arguments": {"command": "cat /workspace_note.txt && date"}
    }
  }' | jq .

# 4. Suspend the environment when the agent turn finishes
ate-env suspend dev1
kubectl ate get actor dev1 -a ate-env
```

---

### Demo 7: GitHub Actions Self-Hosted Runner & Live `antigravity-cli` PR Review on Agent-Substrate

**Goal**: Show a real-world end-to-end CI/CD + AI Code Review workflow where a **GitHub Actions Self-Hosted Runner (`gh-runner-1`)** runs inside a **Substrate Actor** (`atespace: ate-demo-github`). Between Pull Requests, the runner is suspended to Google Cloud Storage (`ACTOR_STATE_SUSPENDED`, consuming 0 workers). When a developer opens or pushes to a PR in [`boredabdel/substrate-pr-review-demo`](https://github.com/boredabdel/substrate-pr-review-demo), a GitHub webhook wakes the actor in sub-seconds, runs `antigravity-cli review` powered by **Vertex AI Gemini 2.5 Flash** (`boredabdel-lab`), posts a rich PR Review comment with live actor telemetry (proving both in-memory RAM counters and durable disk history survive across runs), and automatically suspends the runner actor back to GCS when the job completes.

#### Architecture of Demo 7

```
GitHub Repo (boredabdel/substrate-pr-review-demo)
   │
   │ 1. Webhook (workflow_job.queued / pull_request)
   ▼
runner-gateway (LoadBalancer: http://136.86.242.175/webhook in ate-demo-github)
   │
   │ 2. HTTP POST /wake (Header: ate-target-actor: ate-demo-github/gh-runner-1)
   ▼
atenet-router ──► Restores Actor `gh-runner-1` (RAM + /home/runner/state) onto `github-runner` WorkerPool
   │
   ▼
Actor `gh-runner-1` (gVisor Sandbox)
 ├── actor-daemon (:80)
 │    ├── Preserves volatile RAM counters (`inMemoryWakes`, `inMemoryReviews`) across gVisor snapshots
 │    ├── Persists GitHub Runner credentials & `/home/runner/state/history.json` on Substrate `durableDir`
 │    └── Launches GitHub Actions `/runner/run.sh` (`runs-on: [self-hosted, agent-substrate]`)
 │
 └── GitHub Actions Job (`antigravity-review.yml`)
      ├── Step 1: Checkout PR branch & show Sandbox Telemetry
      ├── Step 2: `antigravity-cli review` ──► Calls Vertex AI `gemini-2.5-flash` (`boredabdel-lab`)
      │           & posts live AI PR Comment + Substrate RAM/Disk Telemetry on GitHub PR
      └── Step 3: `antigravity-cli suspend-after-job` ──► Cleanly stops `Runner.Listener` after job
                  completion and calls `ateapi.SuspendActor` to checkpoint `gh-runner-1` back to GCS!
```

#### Step-by-Step Live Walkthrough

**1. Show that the GitHub Runner Actor is currently `ACTOR_STATE_SUSPENDED` (0 compute used)**
```bash
# Confirm gh-runner-1 is suspended in GCS and not occupying any worker pod:
kubectl ate get actor gh-runner-1 -a ate-demo-github

# Show that GitHub repository boredabdel/substrate-pr-review-demo has gh-runner-1 registered:
gh api repos/boredabdel/substrate-pr-review-demo/actions/runners \
  --jq '.runners[] | {id, name, status, labels: [.labels[].name]}'
```

**2. Inspect the live PR #1 (or push a new commit to trigger a live wakeup on stage)**
You already have an open Pull Request with two completed reviews at:
- **PR URL**: https://github.com/boredabdel/substrate-pr-review-demo/pull/1

To trigger a brand-new live run on stage, push a commit to the `demo/risky-order-handler` branch from `/Users/abdelfettah/data/code/agent-substrate/substrate-pr-review-demo`:
```bash
cd /Users/abdelfettah/data/code/agent-substrate/substrate-pr-review-demo
git checkout demo/risky-order-handler

# Append a new comment or function to main.go and push to GitHub:
cat << 'EOF' >> main.go

// DebugDumpConfig prints environment variables to HTTP response (live demo trigger)
func DebugDumpConfig(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "DB_PASSWORD=%s\n", os.Getenv("DB_PASSWORD"))
}
EOF

git add main.go
git commit -m "feat: add debug config endpoint for live demo"
git push origin demo/risky-order-handler
```

**3. Watch the Substrate Actor wake up from GCS, run the GitHub Action, and auto-suspend**
```bash
# Watch the actor transition:
#   ACTOR_STATE_SUSPENDED -> ACTOR_STATE_RUNNING (runs job) -> ACTOR_STATE_SUSPENDED
kubectl ate get actor gh-runner-1 -a ate-demo-github -w
```
*(In a second terminal or after Ctrl-C, watch the GitHub Actions run finish)*:
```bash
gh run list --repo boredabdel/substrate-pr-review-demo --limit 3
```

**4. Open the GitHub PR in the Browser to Show the Live Gemini 2.5 Flash Review & Telemetry**
```bash
open https://github.com/boredabdel/substrate-pr-review-demo/pull/1
```
Or view the latest comment directly in the terminal:
```bash
gh pr view 1 --repo boredabdel/substrate-pr-review-demo --comments
```

> **Talking Points on the PR Comment**:
> 1. **Real Self-Hosted Runner on gVisor**: The job ran on `runs-on: [self-hosted, agent-substrate]` inside our GKE `github-runner` WorkerPool (`Linux 4.4.0` gVisor kernel).
> 2. **Volatile RAM + Durable Disk Persistence**: Point to the **Substrate Actor Telemetry** table at the top of the PR comment:
>    - **Preserved In-Memory Wakes (RAM)** and **Preserved In-Memory Reviews (RAM)** increment on every PR push—even though the actor is completely evicted from Kubernetes nodes between runs!
>    - **Durable Disk History (`/home/runner/state/history.json`)** lists every past PR review and commit SHA across suspend/resume cycles.
> 3. **Zero Idle Cost**: Within 8 seconds after the GitHub Actions job finishes, `gh-runner-1` is back in `ACTOR_STATE_SUSPENDED` (`WORKER POD: <none>`), costing $0 in compute until the next `git push`.

---

## 5. Bonus Reference: Always-On Agent (`always-on-agent`)

We also cloned [`/Users/abdelfettah/data/code/agent-substrate/always-on-agent`](./always-on-agent), which demonstrates a production multi-tenant **Always-On Personal AI Assistant (OpenClaw)** architecture on Agent-Substrate:
- A lightweight stateless **Gateway** handles Slack/Discord/Telegram webhooks and routes each user's request to their dedicated Substrate actor (`ate-target-actor: <atespace>/<user-actor>`).
- Each user gets an isolated OpenClaw instance with persistent memory (`/home/openclaw/.openclaw`) that automatically suspends between conversations and resumes in milliseconds when a message arrives.

---

## 6. Post-Talk Teardown (When You No Longer Need the Cluster)

When your talk is over and you want to tear down the GKE cluster and GCS bucket to stop billing in `boredabdel-lab`:

```bash
cd /Users/abdelfettah/data/code/agent-substrate/substrate
source .ate-dev-env.sh
go run ./tools/setup-gcp teardown --cluster-name="$CLUSTER_NAME" --zone="$ZONE" --bucket-name="$BUCKET_NAME"
```
