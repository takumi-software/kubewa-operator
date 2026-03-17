# KubeWA Operator 🚀

> **The first Kubernetes WhatsApp Operator** — turns WhatsApp (via Twilio) into a native, bidirectional interface for Kubernetes incident management, with AI-powered remediation suggestions.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/takumi-software/kubewa-operator)](https://goreportcard.com/report/github.com/takumi-software/kubewa-operator)

---

## ✨ Features

| Feature | Description |
|---|---|
| 📲 **WhatsApp Alerts** | Sends interactive incident alerts with up to 3 quick-reply action buttons |
| 🔄 **On-Call Rotation** | Automatic daily/weekly rotation with backup escalation |
| ⚡ **K8s Remediation** | Executes rollback, scale, restart, patch directly against the Kubernetes API |
| 🤖 **AI Suggestions** | Ollama sidecar (Llama 3) generates fix suggestions in natural language |
| 🌍 **Natural Language** | Understands "rollback la api", "escalar a 2 replicas" (Spanish/Portuguese/English) |
| 🔒 **Zero External Servers** | Everything runs inside the cluster |
| 📋 **Full Audit Trail** | Every action is recorded as a Kubernetes Event and in the Incident status |
| 🔐 **Security First** | Twilio signature verification, phone whitelist, rate limiting, minimal RBAC |

---

## 📐 Architecture

```
Prometheus / Alertmanager / ArgoCD / Manual CR
           │
           ▼
  ┌─────────────────────────────────────────────┐
  │              Kubernetes Cluster              │
  │                                             │
  │  ┌──────────────────────────────────────┐   │
  │  │        KubeWA Operator Pod            │   │
  │  │                                      │   │
  │  │  ┌──────────────┐ ┌───────────────┐  │   │
  │  │  │  Incident     │ │  OnCallSchedule│  │   │
  │  │  │  Controller   │ │  Controller   │  │   │
  │  │  └──────┬───────┘ └───────────────┘  │   │
  │  │         │                            │   │
  │  │  ┌──────▼───────┐ ┌───────────────┐  │   │
  │  │  │ Twilio Client│ │ Ollama Sidecar │  │   │
  │  │  │ (WhatsApp)   │ │  (AI/Llama 3) │  │   │
  │  │  └──────┬───────┘ └───────────────┘  │   │
  │  │         │                            │   │
  │  │  ┌──────▼───────────────────────┐    │   │
  │  │  │   Twilio Webhook Handler     │    │   │
  │  │  │  (signature verification +   │    │   │
  │  │  │   rate limiting + dispatch)  │    │   │
  │  │  └──────────────────────────────┘    │   │
  │  └──────────────────────────────────────┘   │
  │                                             │
  └─────────────────────────────────────────────┘
           │                    ▲
           ▼                    │
  ┌─────────────────┐   ┌──────────────────┐
  │  Twilio / WA    │   │  On-Call Engineer│
  │  Business API   │◄──│  (WhatsApp reply) │
  └─────────────────┘   └──────────────────┘
```

---

## 🛠 CRDs

### `OnCallSchedule`

Defines rotating on-call personnel that receive WhatsApp incident alerts.

```yaml
apiVersion: kubewa.dev/v1alpha1
kind: OnCallSchedule
metadata:
  name: platform-team
  namespace: kubewa-system
spec:
  rotation: weekly          # daily | weekly
  timezone: "America/Sao_Paulo"
  ackTimeout: "15m"
  schedule:
    - name: "Alice Silva"
      phone: "+5511999990001"
    - name: "Bob Santos"
      phone: "+5511999990002"
  backup:
    name: "Manager On-Call"
    phone: "+5511999990000"
```

**Status fields:** `currentOnCallName`, `currentOnCallPhone`, `nextShift`

### `Incident`

When an Incident CR is created, the operator sends a WhatsApp alert with interactive remediation buttons to the current on-call person.

```yaml
apiVersion: kubewa.dev/v1alpha1
kind: Incident
metadata:
  name: api-high-latency-001
  namespace: production
spec:
  title: "API gateway latency > 5s (P99)"
  severity: critical        # critical | high | medium | low
  onCallScheduleRef: platform-team
  source: prometheus
  remediationActions:
    - name: rollback
      type: RollbackDeployment
      targetName: api-gateway
      approvalRequired: false
    - name: scale-down
      type: ScaleDeployment
      targetName: api-gateway
      replicas: 1
      approvalRequired: true
    - name: restart
      type: RestartDeployment
      targetName: api-gateway
      approvalRequired: false
```

**Supported action types:** `RollbackDeployment`, `ScaleDeployment`, `RestartDeployment`, `PatchDeployment`, `RollbackStatefulSet`, `ScaleStatefulSet`, `RestartStatefulSet`

**Status fields:** `state` (Open/Acked/Resolved/EscalatedToBackup), `notifiedPhone`, `ackedBy`, `auditTrail[]`

---

## 🚀 Quick Start (5 minutes)

### Prerequisites

- Kubernetes cluster (k8s ≥ 1.26)
- Helm 3
- A Twilio account with WhatsApp Business API enabled
- `kubectl` configured

### 1. Install the operator

```bash
helm upgrade --install kube-wa-operator \
  oci://ghcr.io/takumi-software/helm/kubewa-operator \
  --namespace kubewa-system \
  --create-namespace \
  --set twilio.accountSID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx \
  --set twilio.authToken=your_auth_token \
  --set twilio.fromNumber="whatsapp:+14155238886" \
  --set webhook.publicURL="https://your-public-url/twilio-incoming"
```

### 2. Configure Twilio webhook

In your [Twilio Console](https://console.twilio.com), set the WhatsApp sandbox / business number webhook to:

```
https://<your-public-url>/twilio-incoming?incident=<name>&ns=<namespace>
```

> 💡 **Tip:** Use [ngrok](https://ngrok.com) or a Kubernetes Ingress/LoadBalancer to expose the webhook endpoint during development.

### 3. Create your first on-call schedule

```bash
kubectl apply -f examples/oncallschedule.yaml
kubectl get oncallschedule -n kubewa-system
```

### 4. Create an incident and watch the magic

```bash
kubectl apply -f examples/incident.yaml
```

Within seconds, the current on-call person receives a WhatsApp message:

```
🚨 *INCIDENT ALERT* 🚨

*API gateway latency > 5s (P99)*
Severity: *CRITICAL*

💡 *Suggested fix:*
Consider rolling back the last deployment of api-gateway
to restore latency to normal levels.

Reply with the number to take action:
  *1.* rollback
  *2.* scale-down
  *3.* restart

Or reply *ack* to acknowledge.
```

### 5. Reply and remediate

Reply **1** (or **"rollback"**, **"rollback la api"**, etc.) and the operator will:

1. Execute `kubectl rollout undo deployment/api-gateway -n production`
2. Update the `Incident` status to `Acked`
3. Record the action in `status.auditTrail`
4. Emit a Kubernetes Event
5. Send a confirmation WhatsApp message: `✅ Action rollback executed: Rollback triggered for Deployment production/api-gateway`

---

## ⚙️ Configuration Reference

### Helm values

| Key | Default | Description |
|---|---|---|
| `replicaCount` | `1` | Number of operator replicas |
| `image.repository` | `ghcr.io/takumi-software/kubewa-operator` | Image repository |
| `image.tag` | chart `appVersion` | Image tag |
| `twilio.existingSecret` | `""` | Use existing K8s Secret for Twilio credentials |
| `twilio.accountSID` | `""` | Twilio Account SID |
| `twilio.authToken` | `""` | Twilio Auth Token |
| `twilio.fromNumber` | `""` | WhatsApp from number (e.g. `whatsapp:+14155238886`) |
| `webhook.publicURL` | `""` | Public URL for Twilio to POST incoming messages |
| `webhook.serviceType` | `ClusterIP` | K8s Service type for webhook endpoint |
| `security.allowedPhones` | `""` | Comma-separated phone whitelist |
| `ollama.enabled` | `true` | Enable Ollama AI sidecar |
| `ollama.model` | `llama3` | Ollama model to use |
| `leaderElection` | `true` | Enable leader election |

### Environment variables

| Variable | Description |
|---|---|
| `TWILIO_ACCOUNT_SID` | Twilio Account SID |
| `TWILIO_AUTH_TOKEN` | Twilio Auth Token |
| `TWILIO_FROM_NUMBER` | WhatsApp from number |
| `OLLAMA_URL` | Ollama API base URL (default: `http://localhost:11434`) |
| `OLLAMA_MODEL` | Ollama model (default: `llama3`) |
| `ALLOWED_PHONES` | Comma-separated phone whitelist |

---

## 🔒 Security

- **Twilio Signature Verification:** Every incoming webhook is verified using HMAC-SHA1 against `X-Twilio-Signature`.
- **Phone Whitelist:** Set `security.allowedPhones` to restrict which phones can trigger actions.
- **Rate Limiting:** Built-in per-phone rate limiter (10 messages/minute).
- **Minimal RBAC:** The `ClusterRole` only grants access to `Deployments`, `StatefulSets`, `ReplicaSets`, `Events`, and KubeWA CRDs — nothing else.
- **Non-root Container:** The operator runs as uid 65532 (`nonroot`) on a distroless base image.
- **Read-only Filesystem:** The container filesystem is read-only.
- **Secrets Management:** Twilio credentials are stored in a Kubernetes Secret.

---

## 🧪 Development

```bash
# Run unit tests
make test

# Run with coverage
make test-coverage

# Build binary
make build

# Generate CRD manifests
make manifests

# Run locally (requires kubeconfig)
export TWILIO_ACCOUNT_SID=ACxx TWILIO_AUTH_TOKEN=xx TWILIO_FROM_NUMBER="whatsapp:+14155238886"
make run

# Lint Helm chart
make helm-lint
```

### Running integration tests with kind

```bash
kind create cluster --name kubewa-test
make install          # install CRDs
make deploy IMG=...   # deploy operator
kubectl apply -f examples/
```

---

## 📦 Project Structure

```
kubewa-operator/
├── api/v1alpha1/
│   ├── groupversion_info.go       # Group/version registration
│   ├── oncallschedule_types.go    # OnCallSchedule CRD types
│   ├── incident_types.go          # Incident CRD types
│   └── zz_generated.deepcopy.go  # Generated deep copy functions
├── cmd/
│   └── main.go                   # Operator entry point
├── internal/
│   ├── controller/
│   │   ├── oncallschedule_controller.go  # Rotation logic
│   │   ├── oncallschedule_controller_test.go
│   │   └── incident_controller.go        # Alert sending + escalation
│   ├── twilio/
│   │   ├── client.go              # Twilio SDK wrapper + signature verification
│   │   └── client_test.go
│   ├── ai/
│   │   ├── ollama.go              # Ollama/LLM integration
│   │   └── ollama_test.go
│   ├── actions/
│   │   ├── executor.go            # K8s remediation action executor
│   │   └── executor_test.go
│   └── webhook/
│       └── handler.go             # Twilio incoming webhook handler
├── config/
│   ├── crd/bases/                 # Generated CRD YAML manifests
│   ├── rbac/                      # ClusterRole + ClusterRoleBinding + SA
│   ├── manager/                   # Deployment + Service manifests
│   └── default/                   # Kustomize overlay
├── helm/kubewa-operator/          # Helm chart
├── examples/                      # Example CRs
├── Dockerfile                     # Multi-stage distroless build
├── Makefile                       # Developer tasks
└── README.md
```

---

## 🗺 Roadmap

- [x] OnCallSchedule CRD with daily/weekly rotation
- [x] Incident CRD with WhatsApp alerts and interactive buttons
- [x] Twilio signature verification middleware
- [x] K8s action executor (rollback, scale, restart, patch)
- [x] Ollama sidecar for AI suggestions and NL command parsing
- [x] Ack timeout with backup escalation
- [x] Full audit trail in Incident status + K8s Events
- [x] Helm chart + Kustomize config
- [ ] Prometheus Operator alert integration (AlertmanagerConfig)
- [ ] ArgoCD Application sync-failure integration
- [ ] Post-mortem CR with Loki export
- [ ] Multi-cluster support via Cluster API
- [ ] Twilio Content API (rich interactive buttons via template)
- [ ] OLM / OperatorHub metadata
- [ ] Rate-limit configurability per OnCallSchedule

---

## 📄 License

Apache License 2.0 — see [LICENSE](LICENSE).

---

## 🌎 Community

Built with ❤️ for the LATAM and global cloud-native community.

- **Issues:** [github.com/takumi-software/kubewa-operator/issues](https://github.com/takumi-software/kubewa-operator/issues)
- **Discussions:** [github.com/takumi-software/kubewa-operator/discussions](https://github.com/takumi-software/kubewa-operator/discussions)
