package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// CF5-3 / F-03 — we change it, we do not own it.
//
// The ownership guard used to rest on a claim that turns out to be false: that
// a `kubectl patch` leaves an Update-operation field-manager entry which
// "claims no ongoing ownership of the fields, so it says nothing about whether
// a future patch will conflict". Server-side apply does not work that way. It
// conflicts with whoever owns the field, and an Update-operation entry owns the
// fields it set just as an Apply-operation one does:
//
//	$ kubectl apply --server-side --field-manager=some-gitops-tool -f rw.yaml
//	error: Apply failed with 1 conflict: conflict with "kubectl-patch" using apps/v1:
//	  .spec.template.spec.containers[name="report-worker"].resources.limits.memory
//
// So Dorgu was creating, on the one class of workload it is willing to write
// to, exactly the failure its ownership guard exists to prevent. Anyone who
// later brought a healed Deployment under Helm, ArgoCD or Flux hit a hard apply
// failure with Dorgu's fingerprints on it.
//
// The fix is to leave no fingerprints. The patch is made under Dorgu's own
// field manager so its footprint is uniquely identifiable, and that entry is
// then removed. `--server-side --field-manager=dorgu --force-conflicts` was the
// alternative and is worse: it would make Dorgu a *persistent* Apply-operation
// owner of those fields, which is precisely the thing the next `helm upgrade`
// would have to fight.
//
// Removing the entry does more than avoid harm. An Update takes the fields it
// writes away from whoever held them, so a heal moves a pre-existing
// `kubectl-set` claim onto `dorgu` and then drops it: the fields end up owned
// by nobody, and a later server-side apply that would have failed now succeeds.
//
// Verified end to end against kube-apiserver 1.36.2.

// dorguFieldManager is the field manager Dorgu patches under.
//
// It has to be Dorgu's own name rather than kubectl's default `kubectl-patch`,
// for two reasons. Its entry must be distinguishable from a `kubectl patch` the
// user ran themselves, which Dorgu has no business deleting. And the operator's
// ownership detection skips managers prefixed `dorgu`, so a footprint that
// somehow survives cannot make Dorgu refuse to heal the same workload again.
const dorguFieldManager = "dorgu"

// managedFieldsResetEntry is the API server's documented form for clearing the
// managedFields list: a list holding one empty entry, not an empty list. It is
// only needed in the degenerate case where Dorgu's entry is the only one on the
// object, because the meaning of `[]` has varied between API server versions
// while `[{}]` has not.
const (
	managedFieldsResetEntry = `{}`
	managedFieldsResetValue = `[` + managedFieldsResetEntry + `]`
)

// deploymentFootprint is a Deployment's field-manager state at the moment it
// was read: the resourceVersion, and every managedFields entry kept as the raw
// JSON the API server returned.
//
// The entries are kept raw on purpose. Removing one means writing the whole
// list back, so every entry Dorgu is not removing has to go back byte for byte.
// Round-tripping them through a struct would silently drop any field this CLI
// does not know about, and rewriting another manager's ownership record is a
// far worse bug than the footprint being fixed here.
type deploymentFootprint struct {
	ResourceVersion string
	Entries         []footprintEntry
}

// footprintEntry is one managedFields entry: the manager name Dorgu matches on,
// and the untouched JSON it came in as.
type footprintEntry struct {
	Manager string
	Raw     json.RawMessage
}

// has reports whether any entry belongs to the given field manager.
func (f deploymentFootprint) has(manager string) bool {
	for _, e := range f.Entries {
		if strings.EqualFold(e.Manager, manager) {
			return true
		}
	}
	return false
}

// parseDeploymentFootprint reads the managedFields state out of
// `kubectl get deployment -o json --show-managed-fields` output.
func parseDeploymentFootprint(raw []byte) (deploymentFootprint, error) {
	var obj struct {
		Metadata struct {
			ResourceVersion string            `json:"resourceVersion"`
			ManagedFields   []json.RawMessage `json:"managedFields"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return deploymentFootprint{}, fmt.Errorf("parse deployment: %w", err)
	}

	out := deploymentFootprint{
		ResourceVersion: obj.Metadata.ResourceVersion,
		Entries:         make([]footprintEntry, 0, len(obj.Metadata.ManagedFields)),
	}
	for _, raw := range obj.Metadata.ManagedFields {
		var head struct {
			Manager string `json:"manager"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return deploymentFootprint{}, fmt.Errorf("parse managedFields entry: %w", err)
		}
		out.Entries = append(out.Entries, footprintEntry{Manager: head.Manager, Raw: raw})
	}
	return out, nil
}

// buildFootprintStripPatch builds the merge patch that removes every
// managedFields entry belonging to manager and leaves the rest untouched.
// It reports false when there is nothing belonging to that manager, which is
// the normal outcome on a workload Dorgu has not patched.
//
// The resourceVersion goes in as an optimistic-concurrency precondition. Writing
// a managedFields list back is a read-modify-write over state Dorgu does not
// own, so if anything else touched the Deployment in between, the API server
// must reject this rather than let Dorgu overwrite a list it never saw.
func buildFootprintStripPatch(f deploymentFootprint, manager string) (string, bool, error) {
	kept := make([]json.RawMessage, 0, len(f.Entries))
	removed := false
	for _, e := range f.Entries {
		if strings.EqualFold(e.Manager, manager) {
			removed = true
			continue
		}
		kept = append(kept, e.Raw)
	}
	if !removed {
		return "", false, nil
	}
	if len(kept) == 0 {
		kept = []json.RawMessage{json.RawMessage(managedFieldsResetEntry)}
	}

	metadata := map[string]any{"managedFields": kept}
	if f.ResourceVersion != "" {
		metadata["resourceVersion"] = f.ResourceVersion
	}
	b, err := json.Marshal(map[string]any{"metadata": metadata})
	if err != nil {
		return "", false, fmt.Errorf("marshal managedFields patch: %w", err)
	}
	return string(b), true, nil
}

// footprintError says that Dorgu patched the workload but could not take its
// own field-manager entry back off it. The heal itself succeeded, so this is
// never returned as the command's error: it is reported to the user, loudly,
// because a footprint left in silence is the failure mode this whole fix set
// exists to remove.
type footprintError struct {
	Namespace  string
	Deployment string
	Cause      error
}

func (e *footprintError) Error() string {
	return fmt.Sprintf("could not remove Dorgu's field-manager entry from %s/%s: %v",
		e.Namespace, e.Deployment, e.Cause)
}

func (e *footprintError) Unwrap() error { return e.Cause }

// stripFootprintAttempts is how many read-modify-write rounds the strip gets.
// A Conflict means something else wrote to the Deployment between the read and
// the patch, which is worth one retry against fresh state and not worth more:
// the Deployment was just patched, so a controller writing status is the likely
// cause and it settles immediately.
const stripFootprintAttempts = 3

// stripDorguFootprint removes Dorgu's own managedFields entry from a Deployment
// it has just patched, and confirms the removal took.
//
// The confirmation is not ceremony. The API server accepts a client-supplied
// managedFields list on some paths and quietly recomputes it on others, so the
// only honest way to report "Dorgu owns nothing here" is to read the object back
// and look. If the entry is still there, this returns an error and the caller
// warns; it never reports a footprint removed that is not.
func stripDorguFootprint(ctx context.Context, kubeconfig, ns, name string) error {
	fail := func(cause error) error {
		return &footprintError{Namespace: ns, Deployment: name, Cause: cause}
	}

	for attempt := 0; attempt < stripFootprintAttempts; attempt++ {
		before, err := readDeploymentFootprint(ctx, kubeconfig, ns, name)
		if err != nil {
			return fail(err)
		}
		patch, needed, err := buildFootprintStripPatch(before, dorguFieldManager)
		if err != nil {
			return fail(err)
		}
		if !needed {
			// Nothing attributable to Dorgu is on the object. Either the patch
			// was a no-op or the entry is already gone; either way there is no
			// footprint to remove.
			return nil
		}

		if err := patchDeploymentMetadata(ctx, kubeconfig, ns, name, patch); err != nil {
			if isConflictError(err) {
				continue
			}
			return fail(err)
		}

		after, err := readDeploymentFootprint(ctx, kubeconfig, ns, name)
		if err != nil {
			return fail(fmt.Errorf("verify removal: %w", err))
		}
		if after.has(dorguFieldManager) {
			return fail(fmt.Errorf(
				"the API server kept a %q entry in managedFields after it was removed", dorguFieldManager))
		}
		return nil
	}

	return fail(fmt.Errorf(
		"the Deployment kept changing under the removal (%d attempts)", stripFootprintAttempts))
}

// readDeploymentFootprint reads a Deployment's managedFields. The
// --show-managed-fields flag is required: kubectl hides managedFields from
// object output by default, and without it every Deployment reads as having no
// field managers at all.
func readDeploymentFootprint(ctx context.Context, kubeconfig, ns, name string) (deploymentFootprint, error) {
	out, err := kubectlCmd(ctx, kubeconfig,
		"get", "deployment", name, "-n", ns, "-o", "json", "--show-managed-fields").CombinedOutput()
	if err != nil {
		return deploymentFootprint{}, fmt.Errorf("read field managers: %s", kubectlErrText(out))
	}
	return parseDeploymentFootprint(out)
}

// patchDeploymentMetadata applies the managedFields merge patch. It is a merge
// patch rather than a strategic one because managedFields is a list Dorgu is
// replacing wholesale, and strategic merge would try to merge it by key.
func patchDeploymentMetadata(ctx context.Context, kubeconfig, ns, name, patch string) error {
	out, err := kubectlCmd(ctx, kubeconfig,
		"patch", "deployment", name, "-n", ns,
		"--type", "merge", "--field-manager", dorguFieldManager, "-p", patch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", kubectlErrText(out))
	}
	return nil
}

// isConflictError reports whether kubectl failed the optimistic-concurrency
// precondition. The text is matched because the CLI shells out to kubectl and
// has no typed API error to inspect.
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "(conflict)") ||
		strings.Contains(text, "the object has been modified")
}

// printFootprintWarning tells the user, in full, that Dorgu left an ownership
// footprint it meant to clean up, what that will break, and how to remove it by
// hand. The alternative was to leave it silently, and a reliability tool that
// quietly plants a future `helm upgrade` failure is the thing this product
// exists not to be.
func printFootprintWarning(w io.Writer, exec *healExecution, err error) {
	// Fwarn rather than Warn: the rest of this warning goes to w, and a
	// headline that lands on a different stream is a warning the reader can
	// end up seeing the body of without the point of it.
	output.Fwarn(w, fmt.Sprintf(
		"The workload was patched, but Dorgu could not remove its own field-manager entry: %v", err))

	fmt.Fprintf(w, "  Dorgu now owns %s on %s/%s.\n",
		strings.Join(footprintFieldNames(exec.Change), " and "), exec.Namespace, exec.Deployment)
	fmt.Fprintln(w, "  A later server-side apply (helm upgrade, ArgoCD, Flux) will fail with a conflict against it.")
	fmt.Fprintln(w, "  Clear it with:")
	fmt.Fprintf(w, "    kubectl patch deployment %s -n %s --type merge -p '{\"metadata\":{\"managedFields\":%s}}'\n",
		exec.Deployment, exec.Namespace, managedFieldsResetValue)
	fmt.Fprintln(w, output.Dim(
		"    That clears the whole managedFields list. Every other manager reclaims its fields on its next apply."))
}

// footprintFieldNames names the fields Dorgu's entry now holds, so the warning
// says what is actually at stake rather than "some fields".
func footprintFieldNames(rc *healResourceChange) []string {
	names := make([]string, 0, len(resourceKeyOrder))
	for _, k := range resourceKeyOrder {
		if proposedResourceValue(rc, k.kind, k.name) != "" {
			names = append(names, fmt.Sprintf("resources.%s.%s", k.kind, k.name))
		}
	}
	if len(names) == 0 {
		return []string{"the fields it patched"}
	}
	return names
}
