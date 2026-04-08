package setup

import (
	"fmt"
	"strings"
	"testing"
)

// scriptedExecutor returns canned responses by matching the (name, joined-args) string.
// Each call increments the index for that key, allowing different responses on
// repeat invocations. Unmatched calls return an error.
type scriptedExecutor struct {
	responses map[string][]scriptedResponse
	calls     []string
}

type scriptedResponse struct {
	output string
	err    error
}

func newScriptedExecutor() *scriptedExecutor {
	return &scriptedExecutor{responses: map[string][]scriptedResponse{}}
}

func (s *scriptedExecutor) addResponse(key string, output string, err error) {
	s.responses[key] = append(s.responses[key], scriptedResponse{output: output, err: err})
}

func (s *scriptedExecutor) Run(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, key)
	resps, ok := s.responses[key]
	if !ok || len(resps) == 0 {
		return "", fmt.Errorf("scriptedExecutor: no response for %q", key)
	}
	r := resps[0]
	s.responses[key] = resps[1:]
	return r.output, r.err
}

const argoCDServiceJSON = `{
  "spec": {
    "type": "ClusterIP",
    "clusterIP": "10.96.10.1",
    "ports": [
      {"name": "http", "port": 80, "targetPort": 8080, "protocol": "TCP"},
      {"name": "https", "port": 443, "targetPort": 8080, "protocol": "TCP"}
    ]
  },
  "status": {"loadBalancer": {}}
}`

const openObserveServiceJSON = `{
  "spec": {
    "type": "ClusterIP",
    "clusterIP": "10.96.20.5",
    "ports": [
      {"name": "http", "port": 5080, "targetPort": 5080, "protocol": "TCP"}
    ]
  },
  "status": {"loadBalancer": {}}
}`

const ingressLBJSON = `{
  "spec": {
    "type": "LoadBalancer",
    "clusterIP": "10.96.0.5",
    "ports": [
      {"name": "http", "port": 80, "targetPort": "http", "protocol": "TCP"}
    ]
  },
  "status": {
    "loadBalancer": {
      "ingress": [{"ip": "192.168.1.100"}]
    }
  }
}`

func findComponent(t *testing.T, id ComponentID) ComponentConfig {
	t.Helper()
	for _, c := range blessedComponents() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("component %q not found in blessed stack", id)
	return ComponentConfig{}
}

func TestGetComponentInfo_ArgoCD(t *testing.T) {
	c := findComponent(t, ComponentArgoCd)
	ex := newScriptedExecutor()
	ex.addResponse("kubectl get svc argocd-server -n argocd -o json", argoCDServiceJSON, nil)

	info := GetComponentInfo(ex, c)

	if info.ID != ComponentArgoCd {
		t.Errorf("ID = %q, want %q", info.ID, ComponentArgoCd)
	}
	if info.ServiceName != "argocd-server" {
		t.Errorf("ServiceName = %q, want argocd-server", info.ServiceName)
	}
	if info.ServiceType != "ClusterIP" {
		t.Errorf("ServiceType = %q, want ClusterIP", info.ServiceType)
	}
	if info.ClusterIP != "10.96.10.1" {
		t.Errorf("ClusterIP = %q, want 10.96.10.1", info.ClusterIP)
	}
	if info.WebUIPort != 443 {
		t.Errorf("WebUIPort = %d, want 443", info.WebUIPort)
	}
	wantPF := "kubectl port-forward -n argocd svc/argocd-server 8443:443"
	if info.PortForwardCmd != wantPF {
		t.Errorf("PortForwardCmd = %q, want %q", info.PortForwardCmd, wantPF)
	}
	if info.WebUIURL != "https://localhost:8443" {
		t.Errorf("WebUIURL = %q, want https://localhost:8443", info.WebUIURL)
	}
	if info.Username != "admin" {
		t.Errorf("Username = %q, want admin", info.Username)
	}
	if !strings.Contains(info.CredentialCmd, "argocd-initial-admin-secret") {
		t.Errorf("CredentialCmd = %q, missing 'argocd-initial-admin-secret'", info.CredentialCmd)
	}
	if !strings.Contains(info.CredentialCmd, "{.data.password}") {
		t.Errorf("CredentialCmd = %q, missing password key", info.CredentialCmd)
	}
	if info.ServiceError != "" {
		t.Errorf("ServiceError unexpectedly set: %q", info.ServiceError)
	}
}

func TestGetComponentInfo_OpenObserve(t *testing.T) {
	c := findComponent(t, ComponentOpenObserve)
	ex := newScriptedExecutor()
	ex.addResponse("kubectl get svc openobserve -n openobserve -o json", openObserveServiceJSON, nil)

	info := GetComponentInfo(ex, c)

	if info.WebUIPort != 5080 {
		t.Errorf("WebUIPort = %d, want 5080", info.WebUIPort)
	}
	wantPF := "kubectl port-forward -n openobserve svc/openobserve 5080:5080"
	if info.PortForwardCmd != wantPF {
		t.Errorf("PortForwardCmd = %q, want %q", info.PortForwardCmd, wantPF)
	}
	if info.WebUIURL != "http://localhost:5080" {
		t.Errorf("WebUIURL = %q, want http://localhost:5080", info.WebUIURL)
	}
	if info.Username != "root@example.com" {
		t.Errorf("Username = %q, want root@example.com", info.Username)
	}
	if !strings.Contains(info.CredentialCmd, "ZO_ROOT_USER_PASSWORD") {
		t.Errorf("CredentialCmd = %q, missing ZO_ROOT_USER_PASSWORD", info.CredentialCmd)
	}
}

func TestGetComponentInfo_NoAccess(t *testing.T) {
	c := findComponent(t, ComponentCertManager) // no Access field
	ex := newScriptedExecutor()
	ex.addResponse("kubectl get svc cert-manager -n cert-manager -o json",
		`{"spec":{"type":"ClusterIP","clusterIP":"10.96.0.10","ports":[]},"status":{"loadBalancer":{}}}`, nil)

	info := GetComponentInfo(ex, c)

	if info.PortForwardCmd != "" {
		t.Errorf("PortForwardCmd = %q, want empty for component without Access", info.PortForwardCmd)
	}
	if info.CredentialCmd != "" {
		t.Errorf("CredentialCmd = %q, want empty", info.CredentialCmd)
	}
	if info.WebUIPort != 0 {
		t.Errorf("WebUIPort = %d, want 0", info.WebUIPort)
	}
	if !info.Installed {
		t.Errorf("Installed = false, want true")
	}
}

func TestGetComponentInfo_ServiceNotFound(t *testing.T) {
	c := findComponent(t, ComponentArgoCd)
	ex := newScriptedExecutor()
	ex.addResponse("kubectl get svc argocd-server -n argocd -o json",
		`Error from server (NotFound): services "argocd-server" not found`,
		fmt.Errorf("exit status 1"))

	info := GetComponentInfo(ex, c)

	if info.Installed != true {
		t.Errorf("Installed = false, want true even when service lookup fails")
	}
	if info.ServiceError == "" {
		t.Errorf("ServiceError empty, want populated error string")
	}
	// Static metadata (port-forward command) should still be available since
	// it's derived from ComponentAccess, not from the service query.
	if info.PortForwardCmd == "" {
		t.Errorf("PortForwardCmd empty, want static command from Access metadata")
	}
}

func TestGetComponentInfo_LoadBalancerExternalIP(t *testing.T) {
	c := findComponent(t, ComponentIngressNginx) // no Access — but we still parse svc state
	// ingress-nginx default service name is the helm release name
	ex := newScriptedExecutor()
	ex.addResponse("kubectl get svc ingress-nginx -n ingress-nginx -o json", ingressLBJSON, nil)

	info := GetComponentInfo(ex, c)

	if info.ServiceType != "LoadBalancer" {
		t.Errorf("ServiceType = %q, want LoadBalancer", info.ServiceType)
	}
	if info.ExternalIP != "192.168.1.100" {
		t.Errorf("ExternalIP = %q, want 192.168.1.100", info.ExternalIP)
	}
	if len(info.Ports) != 1 || info.Ports[0].Port != 80 {
		t.Errorf("Ports = %#v, want one port=80", info.Ports)
	}
	if info.Ports[0].TargetPort != "http" {
		t.Errorf("TargetPort = %q, want \"http\"", info.Ports[0].TargetPort)
	}
}

func TestGetInstalledComponentsInfo_FiltersToInstalled(t *testing.T) {
	ex := newScriptedExecutor()
	ex.addResponse(
		`kubectl get clusterpersona my-cluster -o jsonpath={.metadata.annotations.dorgu\.io/setup-stack}`,
		"cert-manager,argocd", nil)
	// Only cert-manager and argocd should be queried for service info.
	ex.addResponse("kubectl get svc cert-manager -n cert-manager -o json",
		`{"spec":{"type":"ClusterIP","clusterIP":"10.96.0.10","ports":[]},"status":{"loadBalancer":{}}}`, nil)
	ex.addResponse("kubectl get svc argocd-server -n argocd -o json", argoCDServiceJSON, nil)

	infos, err := GetInstalledComponentsInfo(ex, "my-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("len(infos) = %d, want 2", len(infos))
	}
	if infos[0].ID != ComponentCertManager {
		t.Errorf("infos[0].ID = %q, want cert-manager", infos[0].ID)
	}
	if infos[1].ID != ComponentArgoCd {
		t.Errorf("infos[1].ID = %q, want argocd", infos[1].ID)
	}
}

func TestGetInstalledComponentsInfo_EmptyAnnotation(t *testing.T) {
	ex := newScriptedExecutor()
	ex.addResponse(
		`kubectl get clusterpersona empty -o jsonpath={.metadata.annotations.dorgu\.io/setup-stack}`,
		"", nil)

	infos, err := GetInstalledComponentsInfo(ex, "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len(infos) = %d, want 0", len(infos))
	}
}

func TestGetInstalledComponentsInfo_PersonaReadFails(t *testing.T) {
	ex := newScriptedExecutor()
	ex.addResponse(
		`kubectl get clusterpersona missing -o jsonpath={.metadata.annotations.dorgu\.io/setup-stack}`,
		`Error from server (NotFound): clusterpersonas.dorgu.io "missing" not found`,
		fmt.Errorf("exit status 1"))

	_, err := GetInstalledComponentsInfo(ex, "missing")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want to mention persona name", err)
	}
}

func TestGetInstalledComponentsInfo_EmptyName(t *testing.T) {
	ex := newScriptedExecutor()
	if _, err := GetInstalledComponentsInfo(ex, ""); err == nil {
		t.Fatal("expected error for empty cluster persona name")
	}
}

func TestSuggestLocalPort(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{443, 8443},
		{80, 8080},
		{5080, 5080},
		{9090, 9090},
		{8080, 8080},
		{22, 8022},
	}
	for _, tc := range cases {
		got := suggestLocalPort(tc.in)
		if got != tc.want {
			t.Errorf("suggestLocalPort(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseInstalledIDs(t *testing.T) {
	cases := []struct {
		in   string
		want []ComponentID
	}{
		{"", nil},
		{"cert-manager", []ComponentID{ComponentCertManager}},
		{"cert-manager,argocd", []ComponentID{ComponentCertManager, ComponentArgoCd}},
		{" cert-manager , argocd ", []ComponentID{ComponentCertManager, ComponentArgoCd}},
		{",,cert-manager,,", []ComponentID{ComponentCertManager}},
	}
	for _, tc := range cases {
		got := parseInstalledIDs(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseInstalledIDs(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Errorf("parseInstalledIDs(%q) missing %q", tc.in, id)
			}
		}
	}
}

func TestTargetPortString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"http", "http"},
		{float64(8080), "8080"},
		{int(443), "443"},
	}
	for _, tc := range cases {
		got := targetPortString(tc.in)
		if got != tc.want {
			t.Errorf("targetPortString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
