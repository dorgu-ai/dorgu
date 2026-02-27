# QA Testing

Run the Dorgu release QA checklist for the given version. If no version is specified, use the current development version (`dorgu version` output or "dev").

## Before starting

Read the following files to get the full checklist and any version-specific notes:
- `docs-internal/QA_TESTING_GUIDE.md` — canonical 90-case checklist
- `docs-internal/testing/` — any version-specific checklist files (e.g. `v0.2.1/CHECKLIST_GO_APP.md`)

## Prerequisites

Verify the environment before running any tests:

```bash
go version          # must be 1.21+
kubectl version --client
helm version        # must be v3.x
docker info
kind version
kubectl cluster-info
```

Create a clean Kind cluster if needed:
```bash
kind create cluster --name dorgu-qa-test
kubectl cluster-info --context kind-dorgu-qa-test
```

## Phase 1: CLI install and smoke test

```bash
# Install from source (dev) or from release tag
make build
./build/dorgu version

# Or install from release
go install github.com/dorgu-ai/dorgu/cmd/dorgu@<VERSION>
dorgu version

# Smoke test
dorgu --help
dorgu generate --help
dorgu persona --help
dorgu cluster --help
dorgu watch --help
dorgu sync --help
```

## Phase 2: Manifest generation

Use the embedded sample Go app from `docs-internal/QA_TESTING_GUIDE.md` section 2, or use `dorgu-test/sample_app_go_net_http` if it exists in the workspace.

```bash
# Create a test directory with Dockerfile + docker-compose.yml + Go source
mkdir -p ~/dorgu-qa-test/sample-app && cd ~/dorgu-qa-test/sample-app

# Basic generate
dorgu generate . -o ./k8s-out
ls k8s-out/

# Dry-run (no files written)
dorgu generate . --dry-run

# Skip flags
dorgu generate . --skip-argocd
dorgu generate . --skip-ci
dorgu generate . --skip-persona
dorgu generate . --skip-validation

# Validate generated manifests
kubectl apply --dry-run=client -f k8s-out/
```

Key checks:
- `k8s-out/deployment.yaml` — valid, has `app.kubernetes.io/name` label, resource limits set
- `k8s-out/service.yaml` — valid
- `k8s-out/hpa.yaml` — min/max replicas sane
- `k8s-out/persona.yaml` — `apiVersion: dorgu.io/v1`, `kind: ApplicationPersona`
- `PERSONA.md` — generated alongside manifests

**Known issue (from v0.2.1 QA):** App names with underscores (e.g. `sample_app_go_net_http`) must be sanitized to RFC 1123 DNS subdomains (`sample-app-go-net-http`) for Kubernetes resource names and labels. Verify the generator handles this automatically; if not, manually edit before applying and file a bug.

## Phase 3: App configuration

```bash
# Init (interactive)
dorgu init
dorgu init --minimal
dorgu init --full

# Global config
dorgu init --global
dorgu config list
dorgu config set defaults.namespace qa-test
dorgu config get defaults.namespace   # expected: qa-test

# Regenerate uses config
dorgu generate . -o ./k8s-out
grep "qa-test" k8s-out/deployment.yaml
```

## Phase 4: Operator install

```bash
helm install dorgu-operator \
  oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version <VERSION> \
  --namespace dorgu-system \
  --create-namespace

# Verify
kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/name=dorgu-operator \
  -n dorgu-system --timeout=120s

kubectl get crd applicationpersonas.dorgu.io
kubectl get crd clusterpersonas.dorgu.io
kubectl logs -n dorgu-system \
  -l app.kubernetes.io/name=dorgu-operator --tail=50
# Expected: no ERROR lines
```

## Phase 5: ApplicationPersona lifecycle

```bash
# Generate persona YAML
dorgu persona generate . --dry-run
dorgu persona generate . -o ./k8s-out

# Apply to cluster
dorgu persona apply . -n default
kubectl get applicationpersona -n default

# Check initial phase (no deployment yet)
kubectl get applicationpersona <name> -n default \
  -o jsonpath='{.status.phase}'
# Expected: Pending

dorgu persona status <name> -n default
```

## Phase 6: Deployment validation (operator reconciliation)

```bash
# If using Kind with a local image
docker build -t <app>:latest .
kind load docker-image <app>:latest --name dorgu-qa-test

# Apply manifests
kubectl apply -f k8s-out/deployment.yaml -n default
kubectl apply -f k8s-out/service.yaml -n default
kubectl rollout status deployment/<name> -n default --timeout=120s

# Wait for operator reconciliation (~60s)
sleep 60

# Check persona status
kubectl get applicationpersona <name> -n default \
  -o jsonpath='{.status.phase}'
# Expected: Active (healthy pods) or Failed (pods crashing — also valid)

kubectl get applicationpersona <name> -n default \
  -o jsonpath='{.status.validation.passed}'
# Expected: true

kubectl get applicationpersona <name> -n default \
  -o jsonpath='{.status.health.status}'
# Expected: Healthy or Unhealthy (reflects actual pod state)

dorgu persona status <name> -n default
```

**Note:** If pods crash due to missing dependencies (e.g. a MySQL the app needs), the operator correctly reports `phase=Failed` and `health=Unhealthy` with `validation.passed=true`. This is correct behavior — validation checks manifest conformance, not app runtime health.

**Known issue (from v0.2.1 QA):** If the Dockerfile has no `USER` instruction or runs as root, and the generated manifest sets `runAsNonRoot: true`, pods will fail with `CreateContainerConfigError`. The generator should emit `runAsUser: 65534` in this case. Verify this is handled; if not, file a bug.

## Phase 7: ClusterPersona

```bash
dorgu cluster init --name qa-cluster --environment development
kubectl get clusterpersona

# Wait for operator discovery (~30s)
dorgu cluster status

kubectl get clusterpersona qa-cluster \
  -o jsonpath='{.status.phase}'
# Expected: Ready or Discovering

kubectl get clusterpersona qa-cluster \
  -o jsonpath='{.status.kubernetesVersion}'
kubectl get clusterpersona qa-cluster \
  -o jsonpath='{.status.platform}'
# Expected: Kind (for Kind clusters)
```

## Phase 8 (optional): Webhook

```bash
helm upgrade dorgu-operator \
  oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version <VERSION> -n dorgu-system \
  --set webhook.enabled=true --set webhook.mode=advisory

kubectl get validatingwebhookconfigurations
# Expected: dorgu-operator-validating-webhook listed
```

## Phase 9 (optional): WebSocket / real-time

```bash
helm upgrade dorgu-operator \
  oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version <VERSION> -n dorgu-system \
  --set websocket.enabled=true

dorgu watch personas   # Ctrl+C after verifying connection
dorgu sync status
```

## Cleanup

```bash
kubectl delete applicationpersona --all -n default
kubectl delete clusterpersona --all
helm uninstall dorgu-operator -n dorgu-system
kubectl delete ns dorgu-system
kind delete cluster --name dorgu-qa-test
rm -rf ~/dorgu-qa-test
```

## Recording results

After each section, note:
- Pass/Fail for each check
- Any workarounds applied and the corresponding bug to file
- Operator logs for any unexpected errors (`kubectl logs -n dorgu-system -l app.kubernetes.io/name=dorgu-operator`)

File bugs for anything that required a manual workaround. Update `docs-internal/testing/<version>/` with a completed checklist file.
