package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

// captureStdout temporarily redirects os.Stdout, runs fn, and returns what
// was written. Used to assert against display output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestDisplayComponentInfos_FormatsAllFields(t *testing.T) {
	infos := []setup.ComponentInfo{
		{
			ID:             "argocd",
			DisplayName:    "Argo CD",
			Namespace:      "argocd",
			Installed:      true,
			ServiceName:    "argocd-server",
			ServiceType:    "ClusterIP",
			ClusterIP:      "10.96.10.1",
			WebUIPort:      443,
			WebUIURL:       "https://localhost:8443",
			PortForwardCmd: "kubectl port-forward -n argocd svc/argocd-server 8443:443",
			CredentialCmd:  "kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d",
			Username:       "admin",
			Notes:          "Change password after first login",
		},
		{
			ID:          "cert-manager",
			DisplayName: "cert-manager",
			Namespace:   "cert-manager",
			Installed:   true,
			ServiceName: "cert-manager",
			ServiceType: "ClusterIP",
		},
	}

	out := captureStdout(t, func() {
		displayComponentInfos("qa-cluster", infos)
	})

	wants := []string{
		"Blessed Stack",
		"Access Guide",
		"qa-cluster",
		"Argo CD",
		"argocd-server",
		"kubectl port-forward -n argocd svc/argocd-server 8443:443",
		"https://localhost:8443",
		"admin",
		"argocd-initial-admin-secret",
		"cert-manager",
		"Running (no web UI)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestDisplayComponentInfos_ShowsServiceError(t *testing.T) {
	infos := []setup.ComponentInfo{
		{
			ID:           "argocd",
			DisplayName:  "Argo CD",
			Namespace:    "argocd",
			Installed:    true,
			ServiceName:  "argocd-server",
			ServiceError: `services "argocd-server" not found`,
		},
	}

	out := captureStdout(t, func() {
		displayComponentInfos("qa-cluster", infos)
	})

	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' in output, got:\n%s", out)
	}
}

func TestClusterInfoJSONErrorPath(t *testing.T) {
	output.Init(true, false)
	defer output.Init(false, false)

	out := captureStdout(t, func() {
		_ = output.PrintJSON(map[string]any{
			"error":          `ClusterPersona "dorgu-cluster" has no installed components recorded yet`,
			"clusterPersona": "dorgu-cluster",
			"components":     []any{},
		})
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n--- output ---\n%s", err, out)
	}
	if _, ok := result["error"]; !ok {
		t.Error("expected 'error' key in JSON output")
	}
	if result["clusterPersona"] != "dorgu-cluster" {
		t.Errorf("clusterPersona = %v, want dorgu-cluster", result["clusterPersona"])
	}
	comps, ok := result["components"]
	if !ok {
		t.Fatal("expected 'components' key in JSON output")
	}
	// Must be [] not null — scripting consumers rely on this.
	if comps == nil {
		t.Error("components must be [] not null in JSON error output")
	}
}

func TestClusterInfoJSONAutoDetectFail(t *testing.T) {
	output.Init(true, false)
	defer output.Init(false, false)

	out := captureStdout(t, func() {
		_ = output.PrintJSON(map[string]any{
			"error":      "no ClusterPersona found",
			"components": []any{},
		})
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n--- output ---\n%s", err, out)
	}
	if _, ok := result["error"]; !ok {
		t.Error("expected 'error' key in JSON output")
	}
	if result["components"] == nil {
		t.Error("components must be [] not null")
	}
}

func TestComponentInfo_JSONRoundtrip(t *testing.T) {
	info := setup.ComponentInfo{
		ID:             "argocd",
		DisplayName:    "Argo CD",
		Namespace:      "argocd",
		Installed:      true,
		ServiceName:    "argocd-server",
		ServiceType:    "ClusterIP",
		WebUIPort:      443,
		WebUIURL:       "https://localhost:8443",
		PortForwardCmd: "kubectl port-forward -n argocd svc/argocd-server 8443:443",
		CredentialCmd:  "kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d",
		Username:       "admin",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out setup.ComponentInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PortForwardCmd != info.PortForwardCmd {
		t.Errorf("PortForwardCmd round-trip mismatch: %q != %q", out.PortForwardCmd, info.PortForwardCmd)
	}
	if out.WebUIURL != info.WebUIURL {
		t.Errorf("WebUIURL round-trip mismatch: %q != %q", out.WebUIURL, info.WebUIURL)
	}
	if out.Username != "admin" {
		t.Errorf("Username = %q, want admin", out.Username)
	}
}
