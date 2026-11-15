package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	dorguv1 "github.com/dorgu-ai/dorgu-operator/api/v1"
	"github.com/dorgu-ai/dorgu-operator/api/v1alpha1"
)

// Ticketed rollback (P2-D10). `dorgu remediation rollback <action>` asks the operator
// to restore what it changed. It writes a RemediationProposal with rollbackOf, as the
// user the cluster says it is, and nothing else: no steps, no values. The operator
// derives the restore from what it recorded before it applied the change, the gate rules
// on it, and the result is an ordinary RemediationAction waiting for a person's
// approval. This command prints that action and the approve command; it never approves
// and never touches the workload.

var (
	// proposalWaitTimeout is how long the command waits for the operator's decision on
	// the proposal, proposalPollInterval how often it looks. Variables so tests do not wait.
	proposalWaitTimeout  = time.Minute
	proposalPollInterval = time.Second
)

const (
	// actionRollback is the verb a protected-context refusal opens with.
	actionRollback protectedAction = "roll back"

	// rollbackNameLimit keeps the generated proposal name well inside the 253 characters
	// a name may have, whatever the action is called.
	rollbackNameLimit = 60

	// defaultRollbackReason is the rationale when --reason is not given. The CRD requires
	// one; the operator shows it to whoever approves, as text.
	defaultRollbackReason = "Rollback requested with `dorgu remediation rollback`."

	maxRollbackReason = v1alpha1.MaxRationaleLength - 200
)

// rollbackRefusals is one sentence per Intake reason a rollback can be refused with.
// The reason token is the contract; the operator's own message after it is shown too,
// cleaned, because it names the field or the workload. No em dashes.
var rollbackRefusals = map[string]string{
	v1alpha1.ReasonRollbackTargetNotFound: "That remediation does not exist in this namespace. " +
		"List them with: dorgu remediation list -n " + namespacePlaceholder,
	v1alpha1.ReasonRollbackTargetMismatch: "That remediation changed a different application than the one this rollback targets.",
	v1alpha1.ReasonRollbackNotApplied: "Nothing was applied for that remediation, so there is nothing to roll back. " +
		"Only a remediation that ended Completed or RolledBack can be rolled back.",
	v1alpha1.ReasonRollbackOfRollback: "That remediation is itself a rollback. " +
		"To change the workload again, wait for a new proposal.",
	v1alpha1.ReasonRollbackNoRecordedState: "Dorgu did not record the values from before that change, " +
		"and it only ever restores what it recorded.",
	v1alpha1.ReasonRollbackWorkloadDrifted: "The workload (or its persona) has been changed since that remediation was applied, " +
		"so restoring the recorded values would overwrite that change. " +
		"Look at the live values and set them yourself if you still want to go back.",
	v1alpha1.ReasonRollbackNothingToRestore: "The workload already has the values it had before the change. There is nothing to roll back.",
	v1alpha1.ReasonRollbackAlreadyOpen: "A rollback of that remediation is already open. " +
		"Approve or reject it first: dorgu remediation list -n " + namespacePlaceholder,
	v1alpha1.ReasonRollbackNotHuman: rollbackNeedsAPerson,
}

// rollbackNeedsAPerson is the sentence for a service account asking for a rollback,
// from the operator's own refusal and from the admission policy.
const rollbackNeedsAPerson = "A rollback has to be requested by a person, not a service account " +
	"(an agent, a CI job or the operator). Run this with your own credentials."

// rollbackAdmissionReasons are the admission denials that mean something different for a
// rollback than for an approval: the policy reuses the approval gate's reasons, because
// asking for a rollback is the same authority.
var rollbackAdmissionReasons = map[string]string{
	"ServiceAccountCannotApprove": rollbackNeedsAPerson,
	"MissingApproveVerb": "Your account lacks the approve permission on remediations in " + namespacePlaceholder +
		", which asking for a rollback needs; ask an admin for the dorgu-approver role.",
}

func newRemediationRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <name>",
		Short: "Ask for a rollback of an applied remediation",
		Long: `Ask the operator to restore what an applied remediation changed.

The operator works out the restore itself, from the values it recorded before it
applied the change. This command sends it only the name of the remediation and your
reason; it never writes a value. The operator then checks the restore against the
cluster as it is now, like any other plan, and the result is a new remediation that
waits for approval. Approve it with the usual command, and the CLI applies it with
your own credentials. You may approve your own rollback request.

The request is written as you: the username the cluster reports for your credentials
(what 'kubectl auth whoami' prints). A service account cannot ask for a rollback, and
asking needs the approve permission on remediations in the namespace (the
dorgu-approver role).

A rollback is refused, with the reason, when:
  - the remediation did not end Completed or RolledBack, or applied nothing
  - the operator recorded no values from before the change
  - the workload or its persona has been changed since (restoring would overwrite
    that change; Dorgu never guesses)
  - the workload already has the old values, or a rollback is already open
Exit code 4 means refused, which is a decision and not a breakage: nothing was changed.

This needs operator v0.14 or later.

Examples:
  dorgu remediation rollback fix-oom-api-server -n production
  dorgu remediation rollback fix-oom-api-server -n production --reason "the new limit starves the node"`,
		Args: cobra.MaximumNArgs(1),
		RunE: runRemediationRollback,
	}
	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation")
	cmd.Flags().String("reason", "", "why the rollback is wanted (shown to whoever approves it)")
	cmd.Flags().String("kubeconfig", "", kubeconfigFlagUsage)
	return cmd
}

func runRemediationRollback(cmd *cobra.Command, args []string) error {
	out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
	fail := func(msg string) { _, _ = fmt.Fprintln(errw, "✗ "+msg) }

	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation rollback")
	}
	namespace, _ := cmd.Flags().GetString("namespace")
	reason, _ := cmd.Flags().GetString("reason")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")
	if len(args) == 0 {
		return fmt.Errorf("remediation name required")
	}
	name := args[0]
	if namespace == "" {
		return fmt.Errorf("--namespace is required")
	}
	rationale, err := rollbackRationale(reason)
	if err != nil {
		return err
	}
	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), proposalWaitTimeout+2*remediationCmdTimeout)
	defer cancel()

	// A rollback is a cluster write that ends in a change to a workload, so a protected
	// context refuses it, as it refuses an approval.
	ctxName, err := currentKubeContext(ctx, kubeconfig)
	if err != nil {
		return err
	}
	if err := guardKubeContextFor(ctxName, actionRollback); err != nil {
		fail(err.Error())
		_, _ = fmt.Fprintln(out, "Nothing was requested and nothing was changed.")
		return errSilent
	}

	user, err := resolveIdentity(ctx, kubeconfig)
	if err != nil {
		fail(err.Error())
		return errSilent
	}
	if strings.HasPrefix(user, "system:") {
		fail(rollbackNeedsAPerson)
		_, _ = fmt.Fprintln(out, "Nothing was requested and nothing was changed.")
		return withExitCode(ExitDeclined, errSilent)
	}
	if operatorPredatesTicketV1(operatorVersionReader(ctx, kubeconfig)) {
		fail("Cannot roll back: ticketed rollback needs operator v0.14 or later; upgrade the operator (and apply its CRDs).")
		_, _ = fmt.Fprintln(out, "Nothing was requested and nothing was changed.")
		return errSilent
	}

	target, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Requesting a rollback of %s/%s as %s\n", namespace, name, displayUser(user))
	proposal, err := createRollbackProposal(ctx, kubeconfig, namespace, target, user, rationale)
	if err != nil {
		return reportRollbackCreateError(out, errw, err, namespace)
	}

	decided, err := waitForProposal(ctx, kubeconfig, proposal, namespace, out)
	if err != nil {
		return err
	}
	if decided.Status.Phase == string(v1alpha1.PhaseRejected) {
		printRollbackRefusal(out, decided, namespace)
		return withExitCode(ExitDeclined, errSilent)
	}

	newName := decided.Status.RemediationActionRef.Name
	rollback, err := fetchRemediation(ctx, kubeconfig, newName, namespace)
	if err != nil {
		return err
	}
	printRollbackProposed(out, rollback, name, namespace)
	return nil
}

// rollbackRationale is the reason sent with the request: the user's, bounded, or a
// default. It is text for the approver to read; no decision reads it.
func rollbackRationale(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return defaultRollbackReason, nil
	}
	if len([]rune(reason)) > maxRollbackReason {
		return "", fmt.Errorf("--reason is too long: at most %d characters", maxRollbackReason)
	}
	return reason, nil
}

// proposalCreated is what `kubectl create -o json` prints for the new proposal.
type proposalCreated struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
}

// createRollbackProposal writes the proposal. Everything in it is the user's identity
// and the name of the action: the operator ignores the rest, and the CRD refuses steps.
func createRollbackProposal(
	ctx context.Context, kubeconfig, namespace string, target *remediationFull, user, rationale string,
) (string, error) {
	personaNamespace := target.Spec.PersonaRef.Namespace
	if personaNamespace == "" {
		personaNamespace = namespace
	}
	key, err := randomToken()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]any{
		"apiVersion": "dorgu.io/v1alpha1",
		"kind":       "RemediationProposal",
		"metadata": map[string]any{
			"generateName": rollbackGenerateName(target.Metadata.Name),
			"namespace":    namespace,
		},
		"spec": map[string]any{
			"proposer":       map[string]any{"username": user},
			"source":         dorguv1.ProposalSourceRollback,
			"idempotencyKey": key,
			"rollbackOf":     map[string]any{"name": target.Metadata.Name},
			"target": map[string]any{
				"kind": target.Spec.PersonaRef.Kind, "namespace": personaNamespace, "name": target.Spec.PersonaRef.Name,
			},
			"rationale": rationale,
		},
	})
	if err != nil {
		return "", err
	}

	cmd := kubectlCmd(ctx, kubeconfig, "create", "-f", "-", "-o", "json")
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", classifyWriteError(kubectlErrText(joinOutput(stdout.Bytes(), stderr.Bytes())), namespace)
	}
	var created proposalCreated
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil || created.Metadata.Name == "" {
		return "", errors.New("the proposal was created but kubectl did not say its name; " +
			"look for it with: kubectl get remediationproposal -n " + namespace)
	}
	return created.Metadata.Name, nil
}

// rollbackGenerateName is the prefix the API server completes into the proposal's name.
func rollbackGenerateName(action string) string {
	if len(action) > rollbackNameLimit {
		action = strings.TrimRight(action[:rollbackNameLimit], "-.")
	}
	return "rollback-" + action + "-"
}

// randomToken is a fresh idempotency key: a rollback request is never a retry of an
// earlier one, so it must not collide with one.
func randomToken() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not make an idempotency key: %w", err)
	}
	return "rollback-" + hex.EncodeToString(b[:]), nil
}

// reportRollbackCreateError explains a refused create: an admission denial in the
// rollback's own words, a missing proposal resource, or kubectl's own text.
func reportRollbackCreateError(out, errw io.Writer, err error, namespace string) error {
	var denied *admissionDeniedError
	if errors.As(err, &denied) {
		sentence := ""
		if s, ok := rollbackAdmissionReasons[denied.Token]; ok {
			sentence = strings.ReplaceAll(s, namespacePlaceholder, namespace)
		} else {
			sentence = denied.Error()
		}
		_, _ = fmt.Fprintln(errw, "✗ "+sentence)
		_, _ = fmt.Fprintln(out, "Nothing was requested and nothing was changed.")
		return withExitCode(ExitDeclined, errSilent)
	}
	text := err.Error()
	if strings.Contains(text, "the server doesn't have a resource type") {
		_, _ = fmt.Fprintln(errw, "✗ RemediationProposal CRD not found: the operator on this cluster is too old for rollback "+
			"(operator v0.14 or later), or its CRDs were not applied.")
		_, _ = fmt.Fprintln(out, operatorInstallHint)
		return errSilent
	}
	if strings.Contains(text, "forbidden") {
		_, _ = fmt.Fprintln(errw, "✗ Your account may not create proposals here: "+text)
		_, _ = fmt.Fprintln(out, "Asking for a rollback needs the dorgu-approver role in "+namespace+".")
		return withExitCode(ExitDeclined, errSilent)
	}
	return fmt.Errorf("could not request the rollback: %s", text)
}

// proposalStatus is the part of a RemediationProposal the command reads.
type proposalStatus struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Phase     string `json:"phase"`
		Rejection *struct {
			Stage  string `json:"stage"`
			Reason string `json:"reason"`
		} `json:"rejection"`
		RemediationActionRef *struct {
			Name string `json:"name"`
		} `json:"remediationActionRef"`
		Conditions []statusCondition `json:"conditions"`
	} `json:"status"`
}

// waitForProposal polls until the operator has decided the proposal.
func waitForProposal(ctx context.Context, kubeconfig, name, namespace string, w io.Writer) (*proposalStatus, error) {
	deadline := time.Now().Add(proposalWaitTimeout)
	_, _ = fmt.Fprintln(w, "Waiting for the operator's decision...")
	for {
		cmd := kubectlCmd(ctx, kubeconfig, "get", "remediationproposal", name, "-n", namespace, "-o", "json")
		raw, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("could not read proposal %s: %s", name, kubectlErrText(raw))
		}
		var p proposalStatus
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("could not read proposal %s: %w", name, err)
		}
		switch p.Status.Phase {
		case string(v1alpha1.PhaseRejected):
			return &p, nil
		case string(v1alpha1.PhaseConverted):
			if p.Status.RemediationActionRef != nil && p.Status.RemediationActionRef.Name != "" {
				return &p, nil
			}
		}
		if !time.Now().Before(deadline) {
			_, _ = fmt.Fprintf(w, "✗ The operator did not decide the rollback within %s, so nothing was applied.\n", proposalWaitTimeout)
			_, _ = fmt.Fprintf(w, "  Look at it with: kubectl get remediationproposal %s -n %s\n", name, namespace)
			_, _ = fmt.Fprintln(w, "  If it stays Received, the operator's intake may be closed: "+
				"check the ProposalIntakeReady condition on the ClusterPersona.")
			return nil, errSilent
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(proposalPollInterval):
		}
	}
}

// printRollbackRefusal says why the operator refused the rollback: the intake reason in
// plain words, or the gate stage and guardrail that refused the derived plan, and the
// operator's own message under it.
func printRollbackRefusal(w io.Writer, p *proposalStatus, namespace string) {
	stage, reason := "", ""
	if p.Status.Rejection != nil {
		stage, reason = p.Status.Rejection.Stage, p.Status.Rejection.Reason
	}
	_, _ = fmt.Fprintln(w)
	if sentence, ok := rollbackRefusals[reason]; ok && stage == v1alpha1.StageIntake {
		_, _ = fmt.Fprintln(w, "✗ "+strings.ReplaceAll(sentence, namespacePlaceholder, namespace))
	} else {
		_, _ = fmt.Fprintf(w, "✗ Dorgu's safety gate refused the rollback (%s, %s).\n", printable(stage), printable(reason))
	}
	if c := findCondition(p.Status.Conditions, v1alpha1.ConditionConverted); c != nil && strings.TrimSpace(c.Message) != "" {
		_, _ = fmt.Fprintln(w, "  "+plainInline(printable(strings.TrimSpace(c.Message))))
	}
	_, _ = fmt.Fprintln(w, "Nothing was changed.")
}

// printRollbackProposed shows the action the operator made and what to do next.
func printRollbackProposed(w io.Writer, rb *remediationFull, target, namespace string) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "✓ The operator derived a rollback of %s: %s (Pending approval).\n", target, rb.Metadata.Name)
	printRemediationDiff(w, rb)
	_, _ = fmt.Fprintf(w, "Nothing has been applied. You asked for it, and you may approve it yourself:\n")
	_, _ = fmt.Fprintf(w, "  dorgu remediation approve %s -n %s\n", rb.Metadata.Name, namespace)
	_, _ = fmt.Fprintf(w, "or decline it with: dorgu remediation reject %s -n %s\n", rb.Metadata.Name, namespace)
}

// printable drops control and invisible characters from text the operator wrote, so a
// message can never move the cursor or hide anything on the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}

// rollbackOrigin is the Origin line of a rollback action: which action it undoes and who
// asked. Both come from the operator (the annotation and the provenance it wrote), never
// from the spec's free text. Empty for any other action.
func rollbackOrigin(r *remediationFull) string {
	if r.Spec.Provenance == nil || r.Spec.Provenance.Source != dorguv1.ProposalSourceRollback {
		return ""
	}
	of := printable(r.Metadata.Annotations[dorguv1.AnnotationRollbackOf])
	if of == "" {
		of = "an earlier remediation"
	}
	origin := "Rollback of " + of
	if by := r.Spec.Provenance.RequestedBy; by != "" {
		origin += ", requested by " + displayUser(by)
	}
	return origin
}
