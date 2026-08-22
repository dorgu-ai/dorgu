package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// CF4-3 — understanding first, writes only where invited.
//
// Dorgu understands the whole cluster. It only changes what you have explicitly
// handed it. A Deployment reconciled by Helm, ArgoCD, Flux or kustomize is not
// Dorgu's to patch: the owner's next apply is what has to carry the fix, and a
// direct patch either loses the fields to a field-manager conflict or is quietly
// reverted. So for owned workloads the CLI recommends and refuses, and it says
// who the owner is while doing so.
//
// The record it reads is spec.workloadRef, written by the operator from the LIVE
// Deployment at proposal time. This file mirrors that type (the CLI does not
// depend on the operator module) and holds everything the CLI derives from it.

// ManagedBy values, mirroring dorgu-operator api/v1. Everything except
// managedByUnmanaged means some other system owns the workload's desired state.
const (
	managedByHelm      = "helm"
	managedByArgoCD    = "argocd"
	managedByFlux      = "flux"
	managedByKustomize = "kustomize"
	managedByUnmanaged = "unmanaged"
	managedByUnknown   = "unknown"
)

// workloadRef mirrors the operator's WorkloadRef: the live workload a
// remediation concerns, who owns it, and what its resources actually were when
// the plan was made.
//
// Name is the workload's name, not the persona's. The two differ in most
// brownfield clusters (persona "frontend" over Deployment "frontend-podinfo").
type workloadRef struct {
	Kind              string             `json:"kind"`
	Name              string             `json:"name"`
	Namespace         string             `json:"namespace"`
	Container         string             `json:"container"`
	ManagedBy         string             `json:"managedBy"`
	ManagedByDetail   string             `json:"managedByDetail"`
	ObservedResources *observedResources `json:"observedResources"`
	ObservedImage     string             `json:"observedImage"`
	ObservedAt        string             `json:"observedAt"`
}

// observedResources is the live container's resource block. An empty string
// means the workload does not set that key, which is a different fact from a
// value of zero and the reason F-05 (a silently added CPU limit) was invisible.
type observedResources struct {
	Requests *resourceValues `json:"requests"`
	Limits   *resourceValues `json:"limits"`
}

// resourceValues is the cpu/memory pair, split the same way a container spec
// splits it.
type resourceValues struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// isOwned mirrors WorkloadRef.IsOwned: true for every ManagedBy except
// "unmanaged", and true for a nil ref.
//
// A nil ref is owned on purpose. It means either an operator too old to record
// ownership or a workload that could not be read, and neither is evidence that
// patching is safe. This governs the CLI patching the Deployment only; the
// operator's persona writes are unaffected.
func (w *workloadRef) isOwned() bool {
	return w == nil || w.ManagedBy != managedByUnmanaged
}

// observed reports whether the operator actually resolved a live workload.
// An unresolved ref still exists (ManagedBy "unknown"), but it names nothing,
// so its absent resource keys mean "not read" rather than "not set".
func (w *workloadRef) observed() bool {
	return w != nil && w.Name != ""
}

// describe renders the workload as a reader would name it: kind, namespace,
// name and the container whose resources were read.
func (w *workloadRef) describe() string {
	if !w.observed() {
		return "not recorded"
	}
	kind := w.Kind
	if kind == "" {
		kind = "Deployment"
	}
	out := fmt.Sprintf("%s %s", kind, w.Name)
	if w.Namespace != "" {
		out = fmt.Sprintf("%s %s/%s", kind, w.Namespace, w.Name)
	}
	if w.Container != "" {
		out += fmt.Sprintf(" (container %s)", w.Container)
	}
	return out
}

// target renders the workload without repeating its kind, for headings that
// already say "Deployment".
func (w *workloadRef) target() string {
	if !w.observed() {
		return ""
	}
	out := w.Name
	if w.Namespace != "" {
		out = w.Namespace + "/" + w.Name
	}
	if w.Container != "" {
		out += ", container " + w.Container
	}
	return out
}

// ownerName names what owns the workload. The operator's managedByDetail is
// used verbatim when it has one, because `Helm release "frontend" in namespace
// apps` is the thing the reader can go and edit, and a paraphrase is not.
func ownerName(w *workloadRef) string {
	if w == nil {
		return "an owner Dorgu could not identify"
	}
	if detail := strings.TrimSpace(w.ManagedByDetail); detail != "" {
		return detail
	}
	switch w.ManagedBy {
	case managedByHelm:
		return "a Helm release"
	case managedByArgoCD:
		return "an ArgoCD application"
	case managedByFlux:
		return "a Flux controller"
	case managedByKustomize:
		return "a kustomize overlay"
	default:
		return "an owner Dorgu could not identify"
	}
}

// whyDorguWillNotPatch is the one plain line explaining the refusal. It is
// written so the reader finishes it understanding what would have gone wrong,
// which is the difference between a guard that reads as competence and one that
// reads as breakage.
func whyDorguWillNotPatch(w *workloadRef) string {
	owner := ownerName(w)
	managedBy := ""
	if w != nil {
		managedBy = w.ManagedBy
	}
	switch managedBy {
	case managedByHelm:
		return fmt.Sprintf(
			"A direct patch would claim the fields it sets away from %s, and your next helm upgrade would then fail with a field-manager conflict.",
			owner)
	case managedByArgoCD:
		return fmt.Sprintf(
			"A direct patch would be reverted on the next sync by %s, or rejected outright under server-side apply.",
			owner)
	case managedByFlux:
		return fmt.Sprintf(
			"A direct patch would be reverted the next time %s reconciles this Deployment.",
			owner)
	case managedByKustomize:
		return fmt.Sprintf(
			"A direct patch would be overwritten the next time %s is applied.",
			owner)
	default:
		if !w.observed() {
			return "Dorgu has no record of the live workload behind this remediation, so it cannot tell what owns it. No record is treated as owned."
		}
		return "Dorgu could not identify what manages this Deployment, and an unseen owner is still an owner: a patch that collides with one breaks their next deploy. Unknown is treated as owned."
	}
}

// ownerChangeLocation names where an owned workload's desired state actually
// lives, and how a change there reaches the cluster. It mirrors the operator's
// ownerSourceOfTruth so a plan the operator shaped and a refusal the CLI prints
// send the reader to the same place.
//
// It hedges where Dorgu genuinely does not know: chart values keys are
// chart-specific and Dorgu has not read the chart.
func ownerChangeLocation(w *workloadRef) string {
	owner := ownerName(w)
	managedBy := ""
	if w != nil {
		managedBy = w.ManagedBy
	}
	switch managedBy {
	case managedByHelm:
		return fmt.Sprintf(
			"the values for %s (the key is chart-specific, commonly under `resources`), then run your usual helm upgrade",
			owner)
	case managedByArgoCD:
		return fmt.Sprintf("the Git manifests for %s, then commit and let ArgoCD sync", owner)
	case managedByFlux:
		return fmt.Sprintf("the Git source reconciled by %s, then commit and let Flux reconcile it", owner)
	case managedByKustomize:
		return "your kustomize overlay for this Deployment, then re-apply the overlay"
	default:
		if !w.observed() {
			return "whatever manages this application"
		}
		return fmt.Sprintf("whatever manages Deployment %s", w.Name)
	}
}

// runnableStepCommand returns a step's kubectl command only where it is
// actually runnable, which is on an unmanaged workload and nowhere else.
//
// The operator already strips workload-writing commands from an owned plan, but
// the CLI reads RemediationActions straight out of the cluster: an older
// operator, or anything with permission to create the CRD, can put a
// `kubectl patch` in there. Handing a reader a command that breaks their next
// deploy is the failure this exists to prevent, so the rule is enforced again on
// the side that does the printing.
func runnableStepCommand(cmd string, w *workloadRef) string {
	if w.isOwned() {
		return ""
	}
	return displayableStepCommand(cmd)
}

// --- workload-vs-workload diff ---

// workloadResourceKey is one resource key's before and after on the LIVE
// container.
//
// Before is what the workload actually had at proposal time. Absent is a state
// of its own: it renders as "not set", never as blank and never as zero,
// because "this container has no CPU limit" is the fact that makes adding one a
// change worth seeing.
type workloadResourceKey struct {
	Key     string
	Before  string
	After   string
	Added   bool
	Changed bool
}

// absentValue is how an unset resource key reads.
const absentValue = "not set"

// unknownValue is how a key reads when Dorgu never observed the workload, which
// is a different thing from observing it and finding the key absent.
const unknownValue = "unknown"

// resourceKeyOrder is the fixed render order, so two runs of the same diff read
// the same way.
var resourceKeyOrder = []struct {
	kind string
	name string
}{
	{"limits", "cpu"},
	{"limits", "memory"},
	{"requests", "cpu"},
	{"requests", "memory"},
}

// buildWorkloadResourceDiff renders what the plan would do to the running
// container, grounded in the observed workload rather than in the persona.
//
// This is the fix for F-05. The review diff used to compare persona to persona,
// so approving a memory fix that also introduced a CPU limit showed only the
// memory line: the added key was invisible precisely because the persona it was
// compared against was not the thing being changed.
func buildWorkloadResourceDiff(w *workloadRef, rc *healResourceChange) []workloadResourceKey {
	absent := absentValue
	if !w.observed() {
		absent = unknownValue
	}

	var out []workloadResourceKey
	for _, k := range resourceKeyOrder {
		before := observedResourceValue(w, k.kind, k.name)
		after := proposedResourceValue(rc, k.kind, k.name)
		if before == "" && after == "" {
			continue
		}

		entry := workloadResourceKey{
			Key:    fmt.Sprintf("resources.%s.%s", k.kind, k.name),
			Before: before,
			After:  after,
		}
		if before == "" {
			entry.Before = absent
			entry.Added = after != "" && w.observed()
		}
		entry.Changed = after != "" && after != before
		out = append(out, entry)
	}
	return out
}

// observedResourceValue reads one key out of the live observation, returning ""
// when the workload does not set it.
func observedResourceValue(w *workloadRef, kind, name string) string {
	if w == nil || w.ObservedResources == nil {
		return ""
	}
	var values *resourceValues
	switch kind {
	case "limits":
		values = w.ObservedResources.Limits
	case "requests":
		values = w.ObservedResources.Requests
	}
	if values == nil {
		return ""
	}
	if name == "cpu" {
		return values.CPU
	}
	return values.Memory
}

// proposedResourceValue reads one key out of the plan's resource change,
// returning "" when the plan does not touch it.
func proposedResourceValue(rc *healResourceChange, kind, name string) string {
	if rc == nil {
		return ""
	}
	if kind == "limits" {
		return rc.Limits[name]
	}
	return rc.Requests[name]
}

// printWorkloadDiff renders the workload-vs-workload change: what happens to the
// Deployment, not to the persona.
func printWorkloadDiff(out io.Writer, w *workloadRef, rc *healResourceChange) {
	rows := buildWorkloadResourceDiff(w, rc)
	if len(rows) == 0 {
		return
	}

	heading := "Deployment change:"
	if w.observed() {
		heading = fmt.Sprintf("Deployment change (%s):", w.target())
	}
	fmt.Fprintln(out, heading)

	width := 0
	for _, row := range rows {
		if len(row.Key) > width {
			width = len(row.Key)
		}
	}

	for _, row := range rows {
		switch {
		case row.Added:
			fmt.Fprintf(out, "  %-*s  %s -> %s   %s\n", width, row.Key, row.Before, row.After,
				output.Dim("(adds a key this workload does not set)"))
		case row.Changed:
			fmt.Fprintf(out, "  %-*s  %s -> %s\n", width, row.Key, row.Before, row.After)
		default:
			fmt.Fprintf(out, "  %-*s  %s %s\n", width, row.Key, row.Before, output.Dim("(unchanged)"))
		}
	}

	if !w.observed() {
		fmt.Fprintln(out, output.Dim(
			"  Dorgu has no record of the live workload, so it cannot show what this container has today."))
	}
	fmt.Fprintln(out)
}

// --- refusal ---

// ownedWorkloadError is returned by the heal preflight when the workload belongs
// to somebody else. It is a decision, not a failure: the command ran, Dorgu
// understood the plan, and declined to write. Callers render it with
// printOwnedWorkloadRefusal and exit ExitDeclined.
type ownedWorkloadError struct {
	ref  *workloadRef
	plan *healPlan
}

func (e *ownedWorkloadError) Error() string {
	return fmt.Sprintf("Dorgu will not patch this workload: %s owns it", ownerName(e.ref))
}

// printOwnedWorkloadRefusal is the trust moment. It names the owner, shows what
// would change on the Deployment, hands over the owner-shaped steps the operator
// already generated, and gives one line on why Dorgu will not do it itself.
func printOwnedWorkloadRefusal(out io.Writer, r *remediationFull, decline *ownedWorkloadError) {
	ref := decline.ref

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Dorgu will not patch this workload.")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Workload: %s\n", ref.describe())
	fmt.Fprintf(out, "  Owner:    %s\n", ownerName(ref))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", whyDorguWillNotPatch(ref))
	fmt.Fprintln(out)

	if !decline.plan.change().isEmpty() {
		printWorkloadDiff(out, ref, decline.plan.change())
	}

	fmt.Fprintln(out, "Apply it where this workload's desired state lives:")
	printOwnerSteps(out, ref, decline.plan)
	fmt.Fprintln(out)

	// A refusal from `approve` also means nothing was approved, and the reader
	// may still want the decision on record. A refusal from `heal` comes after
	// approval, so neither applies.
	if r != nil && r.Status.Phase == "Pending" {
		fmt.Fprintln(out, "Nothing was approved and nothing in the cluster was changed.")
		if r.Metadata.Name != "" {
			fmt.Fprintf(out, "To record the decision without a workload patch: dorgu remediation approve %s -n %s --no-heal\n",
				r.Metadata.Name, r.Metadata.Namespace)
		}
	} else {
		fmt.Fprintln(out, "Nothing in the cluster was changed.")
	}
	fmt.Fprintln(out)
}

// printOwnerSteps prints the owner-shaped plan steps. The operator rewrites
// those steps at proposal time (Helm values instructions, the ArgoCD Git path),
// so they are printed as written rather than reworded here. When the plan
// carries none, the concrete resource change is turned into one instruction, so
// a refusal is never a dead end.
// The steps are renumbered from 1: this is a list of things for the reader to
// do, not the plan's own ordering, and a list that starts at [2] because step 1
// was the operator's persona write reads as though something is missing.
func printOwnerSteps(out io.Writer, ref *workloadRef, plan *healPlan) {
	if plan != nil && len(plan.Advisory) > 0 {
		for i, step := range plan.Advisory {
			fmt.Fprintf(out, "  [%d] %s\n", i+1, step.Description)
		}
		return
	}

	fmt.Fprintf(out, "  [1] %s\n", ownerFallbackInstruction(ref, plan.change()))
}

// change returns the plan's resource change, tolerating a nil plan.
func (p *healPlan) change() *healResourceChange {
	if p == nil {
		return nil
	}
	return p.Change
}

// ownerFallbackInstruction builds the one instruction to print when the plan
// itself carries no owner-shaped step: the concrete keys and values, and where
// to set them.
func ownerFallbackInstruction(ref *workloadRef, rc *healResourceChange) string {
	where := ownerChangeLocation(ref)
	fields := changedFieldList(rc)
	if fields == "" {
		return fmt.Sprintf("Make this change in %s.", where)
	}
	return fmt.Sprintf("Set %s in %s.", fields, where)
}

// changedFieldList renders the changed keys as "resources.limits.memory: 128Mi,
// resources.limits.cpu: 100m", in the fixed key order.
func changedFieldList(rc *healResourceChange) string {
	if rc.isEmpty() {
		return ""
	}
	parts := make([]string, 0, len(resourceKeyOrder))
	for _, k := range resourceKeyOrder {
		if v := proposedResourceValue(rc, k.kind, k.name); v != "" {
			parts = append(parts, fmt.Sprintf("resources.%s.%s: %s", k.kind, k.name, v))
		}
	}
	return strings.Join(parts, ", ")
}
