package cli

import "strings"

// liveOwner is evidence, read from the live Deployment, that something other
// than a human with kubectl reconciles it.
type liveOwner struct {
	managedBy string
	detail    string
}

func (o *liveOwner) workloadRef(name, namespace string) *workloadRef {
	return &workloadRef{
		Kind: "Deployment", Name: name, Namespace: namespace,
		ManagedBy: o.managedBy, ManagedByDetail: o.detail,
	}
}

// liveOwnerOf re-detects ownership from the Deployment as it is now. It is the
// CLI-side stopgap for CLI-01, CLI-02 and CLI-06 until the operator's ticket
// carries the ownership decision itself: the proposal's workloadRef is a claim
// made when the plan was written, by whoever wrote it, and a heal must not rest
// on it alone. Any evidence of an owner refuses; the absence of evidence is not
// a statement that patching is safe, only that this check found nothing, and the
// operator's own ownership decision is still required before it gets here.
//
// The evidence list is deliberately short and mirrors what the operator's
// detection treats as unambiguous: Helm's, ArgoCD's and Flux's stamps, kustomize
// build metadata, a controller ownerReference, and the field managers of the
// GitOps controllers.
func liveOwnerOf(d deploymentSummary) *liveOwner {
	if release := d.Annotations["meta.helm.sh/release-name"]; release != "" {
		return &liveOwner{managedByHelm, `Helm release "` + release + `"`}
	}
	if v := d.Labels["app.kubernetes.io/managed-by"]; strings.EqualFold(v, "helm") {
		return &liveOwner{managedByHelm, "a Helm release"}
	}
	if v := d.Annotations["argocd.argoproj.io/tracking-id"]; v != "" {
		return &liveOwner{managedByArgoCD, "an ArgoCD application"}
	}
	if v := d.Labels["argocd.argoproj.io/instance"]; v != "" {
		return &liveOwner{managedByArgoCD, `ArgoCD application "` + v + `"`}
	}
	if v := d.Labels["kustomize.toolkit.fluxcd.io/name"]; v != "" {
		return &liveOwner{managedByFlux, `Flux Kustomization "` + v + `"`}
	}
	if v := d.Labels["helm.toolkit.fluxcd.io/name"]; v != "" {
		return &liveOwner{managedByFlux, `Flux HelmRelease "` + v + `"`}
	}
	if d.Annotations["config.kubernetes.io/origin"] != "" || d.Annotations["alpha.config.kubernetes.io/transformations"] != "" {
		return &liveOwner{managedByKustomize, "a kustomize build"}
	}
	for _, ref := range d.OwnerReferences {
		if ref.Controller {
			return &liveOwner{managedByUnknown, ref.Kind + " " + ref.Name + " controls it"}
		}
	}
	for _, manager := range d.Managers {
		switch manager {
		case "argocd-controller", "argocd-application-controller":
			return &liveOwner{managedByArgoCD, "an ArgoCD application"}
		case "kustomize-controller", "helm-controller":
			return &liveOwner{managedByFlux, "a Flux controller"}
		case "helm":
			return &liveOwner{managedByHelm, "a Helm release"}
		}
	}
	return nil
}
