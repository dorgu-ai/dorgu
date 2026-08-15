package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// brownfieldDeployment is the shape the clean-room manifest produced: nothing on
// the Deployment object, app=<name> on the selector. Helm, kustomize and most
// hand-written YAML look like this, and discovery used to miss all of it.
func brownfieldDeployment(name, appLabel string) deploymentSummary {
	return deploymentSummary{
		Name:           name,
		Containers:     []string{"app"},
		SelectorLabels: map[string]string{"app": appLabel},
	}
}

func TestSelectDeployment_FallbackChain(t *testing.T) {
	tests := []struct {
		name        string
		deployments []deploymentSummary
		appName     string
		want        string
	}{
		{
			name: "rung 1: recommended label on the Deployment object",
			deployments: []deploymentSummary{
				{Name: "web-deploy", Labels: map[string]string{labelAppName: "web"}},
			},
			appName: "web",
			want:    "web-deploy",
		},
		{
			name: "rung 2: short app label on the Deployment object",
			deployments: []deploymentSummary{
				{Name: "web-deploy", Labels: map[string]string{"app": "web"}},
			},
			appName: "web",
			want:    "web-deploy",
		},
		{
			name:        "rung 3: F-01 regression, pod-template-only labels resolve by name",
			deployments: []deploymentSummary{brownfieldDeployment("report-worker", "report-worker")},
			appName:     "report-worker",
			want:        "report-worker",
		},
		{
			name:        "rung 4: selector matches when the Deployment name differs",
			deployments: []deploymentSummary{brownfieldDeployment("checkout-api-v2", "checkout-api")},
			appName:     "checkout-api",
			want:        "checkout-api-v2",
		},
		{
			name: "rung 4: selector on the recommended key",
			deployments: []deploymentSummary{
				{Name: "checkout-api-v2", SelectorLabels: map[string]string{labelAppName: "checkout-api"}},
			},
			appName: "checkout-api",
			want:    "checkout-api-v2",
		},
		{
			name: "earlier rungs win over later ones",
			deployments: []deploymentSummary{
				brownfieldDeployment("web", "web"),
				{Name: "web-canary", Labels: map[string]string{labelAppName: "web"}},
			},
			appName: "web",
			want:    "web-canary",
		},
		{
			name: "the whole brownfield namespace, one target",
			deployments: []deploymentSummary{
				brownfieldDeployment("web", "web"),
				brownfieldDeployment("checkout-api", "checkout-api"),
				brownfieldDeployment("report-worker", "report-worker"),
			},
			appName: "report-worker",
			want:    "report-worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectDeployment(tt.deployments, tt.appName)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestSelectDeployment_Errors(t *testing.T) {
	t.Run("empty namespace says so instead of pointing at --workload", func(t *testing.T) {
		_, err := selectDeployment(nil, "web")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no Deployments in this namespace")
	})

	t.Run("no match lists what is actually there", func(t *testing.T) {
		_, err := selectDeployment([]deploymentSummary{
			brownfieldDeployment("web", "web"),
			brownfieldDeployment("checkout-api", "checkout-api"),
		}, "billing")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "checkout-api, web")
		assert.Contains(t, err.Error(), labelAppName)
		assert.Contains(t, err.Error(), "metadata.name")
		assert.Contains(t, err.Error(), "--workload")
	})

	t.Run("ambiguous match refuses to guess", func(t *testing.T) {
		_, err := selectDeployment([]deploymentSummary{
			brownfieldDeployment("web-blue", "web"),
			brownfieldDeployment("web-green", "web"),
		}, "web")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "web-blue, web-green")
		assert.Contains(t, err.Error(), "--workload")
	})

	t.Run("empty app name never resolves", func(t *testing.T) {
		_, err := selectDeployment([]deploymentSummary{brownfieldDeployment("web", "web")}, "")

		require.Error(t, err)
	})
}

func TestMatchesApp(t *testing.T) {
	tests := []struct {
		name       string
		deployment deploymentSummary
		appName    string
		want       bool
	}{
		{"recommended label", deploymentSummary{Labels: map[string]string{labelAppName: "web"}}, "web", true},
		{"short label", deploymentSummary{Labels: map[string]string{"app": "web"}}, "web", true},
		{"name", deploymentSummary{Name: "web"}, "web", true},
		{"selector", brownfieldDeployment("anything", "web"), "web", true},
		{"no match", brownfieldDeployment("web", "web"), "billing", false},
		{"empty app name", deploymentSummary{Name: ""}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesApp(tt.deployment, tt.appName))
		})
	}
}

func TestWorkloadChainDescription(t *testing.T) {
	got := workloadChainDescription()

	for _, rung := range []string{rungLabelAppName, rungLabelApp, rungName, rungSelector} {
		assert.Contains(t, got, rung)
	}
}
