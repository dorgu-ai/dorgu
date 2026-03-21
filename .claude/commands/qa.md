# QA Testing

Run the Dorgu release QA checklist for the given version. If no version is specified, use the current development version (`dorgu version` output or "dev").

## How this skill works

This is an **interactive guided checklist**. For each phase:
1. Show the user what commands to run and what to check
2. **Ask the user** for the result of each stage using AskUserQuestion (pass/fail/skip + any notes)
3. Track results and move to the next phase
4. At the end, produce a summary of pass/fail/skip counts with any notes

Do NOT run cluster-modifying commands yourself. Show them to the user and ask for the outcome.

## Step 0: Ask the user for test mode

Use AskUserQuestion to ask:

**"Are you testing a local development build or a released version?"**
- **Local (dev build)** — testing from source on a feature branch, building with `make build`
- **Released version** — testing a tagged release installed via `go install` or Homebrew

The answer determines install commands for both CLI and operator throughout the checklist.

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

### If LOCAL (dev build):

```bash
cd /home/poklinho/dorgu-dev/dorgu
make check    # must pass before testing
make build
./build/dorgu version
# Use ./build/dorgu for all subsequent commands (or add to PATH)
export PATH="$(pwd)/build:$PATH"
dorgu version
```

### If RELEASED version:

```bash
go install github.com/dorgu-ai/dorgu/cmd/dorgu@<VERSION>
dorgu version
# Verify the version string matches the tag
```

### Smoke test (both modes):

```bash
dorgu --help
dorgu generate --help
dorgu persona --help
dorgu cluster --help
dorgu cluster setup --help
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

### If LOCAL (dev build):

```bash
cd /home/poklinho/dorgu-dev/dorgu-operator

# Build and load operator image into Kind
make docker-build IMG=dorgu-operator:dev
kind load docker-image dorgu-operator:dev --name dorgu-qa-test

# Install CRDs
make install

# Deploy operator with local image
make deploy IMG=dorgu-operator:dev

# Verify
kubectl wait --for=condition=ready pod \
  -l control-plane=controller-manager \
  -n dorgu-operator-system --timeout=120s

kubectl get crd applicationpersonas.dorgu.io
kubectl get crd clusterpersonas.dorgu.io
kubectl logs -n dorgu-operator-system \
  -l control-plane=controller-manager --tail=50
# Expected: no ERROR lines
```

### If RELEASED version:

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

### Verify (both modes):

```bash
# CRDs must be present
kubectl get crd applicationpersonas.dorgu.io
kubectl get crd clusterpersonas.dorgu.io
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

## Phase 8: Cluster Setup — Blessed Stack Wizard

This phase tests `dorgu cluster setup`, the interactive wizard that installs the Blessed Stack (cert-manager, ingress-nginx, OpenObserve, External Secrets) and annotates the ClusterPersona.

**Prerequisite:** Phase 4 (operator installed) and Phase 7 (ClusterPersona initialized) must pass first.

### 8a. Dry-run mode (no cluster changes)

```bash
dorgu cluster setup --dry-run
```

Ask the user to verify:
- Preflight checks show `kubectl found` and `helm found`
- Dry-run mode message is displayed
- All 4 components are shown with educational "Why it matters" text
- Required components say `[Required — will be installed]`
- External Secrets prompts `Install? [y/N]` (press N to skip)
- Installation plan table is printed
- After confirming, dry-run command log shows all helm commands that would execute
- No actual helm installs happened (`helm list -A` unchanged)

### 8b. Dry-run with flags

```bash
dorgu cluster setup --dry-run --cluster-persona qa-cluster --environment production
```

Ask the user to verify:
- ClusterPersona name shows `"qa-cluster" (from --cluster-persona flag)`
- Environment prompt is skipped (uses `production`)
- Plan table shows `Environment: production`

### 8c. Full install (interactive)

```bash
dorgu cluster setup --cluster-persona qa-cluster --environment development
```

Walk the user through:
1. Preflight checks pass
2. All required components show educational text
3. Accept all required components (press Enter)
4. Skip External Secrets (press N)
5. Review installation plan (3 components)
6. Confirm proceed (press y)
7. Watch each component install with spinner
8. Validation shows pods running for each component
9. ClusterPersona annotation succeeds
10. Final summary shows `Installed: 3  Skipped: 1  Failed: 0`

### 8d. Verify installation artifacts

```bash
# Helm releases exist
helm list -A
# Expected: cert-manager, ingress-nginx, openobserve all STATUS=deployed

# Pods running
kubectl get pods -n cert-manager
kubectl get pods -n ingress-nginx
kubectl get pods -n openobserve

# ClusterPersona annotated
kubectl get clusterpersona qa-cluster -o jsonpath='{.metadata.annotations}' | jq .
# Expected: dorgu.io/setup-stack, dorgu.io/setup-environment, dorgu.io/setup-timestamp present

# Annotation value
kubectl get clusterpersona qa-cluster \
  -o jsonpath='{.metadata.annotations.dorgu\.io/setup-stack}'
# Expected: cert-manager,ingress-nginx,openobserve
```

### 8e. Operator addon discovery

```bash
# Wait 5 minutes for operator reconciliation, or check operator logs
kubectl logs -n dorgu-system -l app.kubernetes.io/name=dorgu-operator --tail=20

# Then check status
dorgu cluster status qa-cluster
# Expected: Discovered Add-ons includes cert-manager, ingress-nginx, openobserve
```

### 8f. Idempotency re-run

```bash
dorgu cluster setup --cluster-persona qa-cluster --environment development
# Run again, accept all defaults, confirm proceed
```

Ask the user to verify:
- `helm upgrade --install` succeeds cleanly (no errors)
- All components show as installed (not failed)
- Annotation is re-applied without error
- No duplicate helm releases (`helm list -A` shows same releases, not new ones)

### 8g. Skip validation flag

```bash
dorgu cluster setup --cluster-persona qa-cluster --skip-validation
# Run with --skip-validation, confirm proceed
```

Ask the user to verify:
- Installation proceeds normally
- Validation section shows "skipped" for all components
- Summary still prints correctly

## Phase 9 (optional): Webhook

```bash
helm upgrade dorgu-operator \
  oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version <VERSION> -n dorgu-system \
  --set webhook.enabled=true --set webhook.mode=advisory

kubectl get validatingwebhookconfigurations
# Expected: dorgu-operator-validating-webhook listed
```

## Phase 10 (optional): WebSocket / real-time

```bash
helm upgrade dorgu-operator \
  oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version <VERSION> -n dorgu-system \
  --set websocket.enabled=true

dorgu watch personas   # Ctrl+C after verifying connection
dorgu sync status
```

## Phase 11 (optional): Platform dashboard

This phase tests `dorgu platform serve`, the web dashboard for visualizing ClusterPersona resources. It requires Phases 4 (operator) and 7 (ClusterPersona) to have passed.

### 11a. Start platform

```bash
dorgu platform serve &
sleep 3
```

Ask the user to verify:
- Server starts on `http://localhost:8080`
- Logs show API, WebSocket, and Frontend URLs

### 11b. API endpoints

```bash
curl -s http://localhost:8080/api/clusters | jq .
curl -s http://localhost:8080/api/clusters/qa-cluster | jq .
```

Ask the user to verify:
- `/api/clusters` returns `{"clusters": [...]}` with ClusterPersona data
- `/api/clusters/qa-cluster` returns the specific cluster with `name`, `spec`, `status`
- Content-Type is `application/json`

### 11c. Frontend

```bash
curl -s http://localhost:8080/ | grep -i "dorgu"
```

Ask the user to verify:
- Page loads (either React dashboard or placeholder HTML)
- If React app embedded: shows "Dorgu Platform" header, cluster table renders
- If placeholder: shows "Backend is running" message

### 11d. WebSocket real-time

If Phase 10 (WebSocket) was tested, verify the platform dashboard also receives real-time updates:
- Create a new ClusterPersona while platform is running
- Check server logs for `Broadcasting WebSocket event` messages
- If browser is open, verify dashboard updates without manual refresh

### 11e. Custom port

```bash
# Stop and restart on custom port
kill %1 2>/dev/null
dorgu platform serve --port 3000 &
sleep 3
curl -s http://localhost:3000/api/clusters | jq .
kill %1 2>/dev/null
```

Ask the user to verify:
- Server starts on port 3000
- API returns data on custom port

### 11f. Graceful shutdown

```bash
# Ctrl+C the platform server (or kill %1)
kill %1 2>/dev/null
```

Ask the user to verify:
- Clean shutdown messages in logs
- No error output
- Terminal returns to normal

## Cleanup

### If LOCAL (dev build):

```bash
# Stop platform server if running (Phase 11)
kill $(pgrep -f "dorgu platform serve") 2>/dev/null

# Remove Blessed Stack components (if Phase 8 was run)
helm uninstall cert-manager -n cert-manager 2>/dev/null
helm uninstall ingress-nginx -n ingress-nginx 2>/dev/null
helm uninstall openobserve -n openobserve 2>/dev/null
helm uninstall external-secrets -n external-secrets 2>/dev/null
kubectl delete ns cert-manager ingress-nginx openobserve external-secrets 2>/dev/null

# Remove Dorgu resources and operator
kubectl delete applicationpersona --all -n default
kubectl delete clusterpersona --all
cd /home/poklinho/dorgu-dev/dorgu-operator && make undeploy 2>/dev/null
kubectl delete ns dorgu-operator-system 2>/dev/null

# Destroy cluster
kind delete cluster --name dorgu-qa-test
rm -rf ~/dorgu-qa-test
```

### If RELEASED version:

```bash
# Stop platform server if running (Phase 11)
kill $(pgrep -f "dorgu platform serve") 2>/dev/null

# Remove Blessed Stack components (if Phase 8 was run)
helm uninstall cert-manager -n cert-manager 2>/dev/null
helm uninstall ingress-nginx -n ingress-nginx 2>/dev/null
helm uninstall openobserve -n openobserve 2>/dev/null
helm uninstall external-secrets -n external-secrets 2>/dev/null
kubectl delete ns cert-manager ingress-nginx openobserve external-secrets 2>/dev/null

# Remove Dorgu resources and operator
kubectl delete applicationpersona --all -n default
kubectl delete clusterpersona --all
helm uninstall dorgu-operator -n dorgu-system
kubectl delete ns dorgu-system

# Destroy cluster
kind delete cluster --name dorgu-qa-test
rm -rf ~/dorgu-qa-test
```

## Recording results

After each section, note:
- Pass/Fail for each check
- Any workarounds applied and the corresponding bug to file
- Operator logs for any unexpected errors (`kubectl logs -n dorgu-system -l app.kubernetes.io/name=dorgu-operator`)

File bugs for anything that required a manual workaround. Update `docs-internal/testing/<version>/` with a completed checklist file.
