package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnerOf(t *testing.T) {
	cases := map[string]struct {
		d    deploymentSummary
		want string
	}{
		"nothing":            {deploymentSummary{Name: "web"}, ""},
		"helm annotation":    {deploymentSummary{Annotations: map[string]string{"meta.helm.sh/release-name": "web"}}, managedByHelm},
		"helm label":         {deploymentSummary{Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"}}, managedByHelm},
		"argo tracking":      {deploymentSummary{Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "a:b"}}, managedByArgoCD},
		"argo label":         {deploymentSummary{Labels: map[string]string{"argocd.argoproj.io/instance": "web"}}, managedByArgoCD},
		"flux kustomization": {deploymentSummary{Labels: map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps"}}, managedByFlux},
		"flux helmrelease":   {deploymentSummary{Labels: map[string]string{"helm.toolkit.fluxcd.io/name": "web"}}, managedByFlux},
		"kustomize build":    {deploymentSummary{Annotations: map[string]string{"config.kubernetes.io/origin": "x"}}, managedByKustomize},
		"controller owner": {deploymentSummary{OwnerReferences: []ownerReference{
			{Kind: "Rollout", Name: "web", Controller: true}}}, managedByUnknown},
		"non-controller owner": {deploymentSummary{OwnerReferences: []ownerReference{{Kind: "Thing", Name: "x"}}}, ""},
		"argo manager":         {deploymentSummary{Managers: []string{"argocd-controller"}}, managedByArgoCD},
		"flux manager":         {deploymentSummary{Managers: []string{"kustomize-controller"}}, managedByFlux},
		"helm manager":         {deploymentSummary{Managers: []string{"helm"}}, managedByHelm},
		"kubectl is a human":   {deploymentSummary{Managers: []string{"kubectl-edit", "dorgu"}}, ""},
		"unrelated label":      {deploymentSummary{Labels: map[string]string{"app.kubernetes.io/managed-by": "Tilt"}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := liveOwnerOf(tc.d)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.managedBy)
			ref := got.workloadRef("web", "shop")
			assert.True(t, ref.isOwned())
			assert.Equal(t, "web", ref.Name)
		})
	}
}

func TestParseDeploymentCarriesIdentityAndOwnershipEvidence(t *testing.T) {
	d, err := parseDeploymentObject([]byte(`{"metadata":{"name":"web","uid":"u1","resourceVersion":"7",
		"annotations":{"meta.helm.sh/release-name":"web"},
		"ownerReferences":[{"kind":"Rollout","name":"web","controller":true}],
		"managedFields":[{"manager":"helm"},{"manager":"dorgu"}]},
		"spec":{"template":{"spec":{"containers":[{"name":"app"}]}}}}`))
	require.NoError(t, err)
	assert.Equal(t, "u1", d.UID)
	assert.Equal(t, "7", d.ResourceVersion)
	assert.Equal(t, "web", d.Annotations["meta.helm.sh/release-name"])
	assert.True(t, d.OwnerReferences[0].Controller)
	assert.Equal(t, []string{"helm", "dorgu"}, d.Managers)
}
