package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// P2-D11 (brief C4): the operator detects CPU throttling from metrics.k8s.io (the
// default) or from a Prometheus-compatible URL in the ClusterPersona, and says which
// in ClusterPersona status.cpuThrottlingDetection. With no source it is off, and the
// status says why. `dorgu health` shows that statement; it reads nothing else.
//
// An operator older than v0.14 writes no such field. That is "unknown", not "off":
// the detection may well exist (it does not before v0.14) but this CLI cannot see.

// States of cpuThrottlingStatus.State.
const (
	cpuThrottlingOn      = "on"
	cpuThrottlingOff     = "off"
	cpuThrottlingUnknown = "unknown"
)

// Reasons this CLI gives itself when the operator gave none (state unknown).
const (
	reasonOperatorPredatesCPUThrottling = "OperatorPredatesCPUThrottling"
	reasonNoClusterPersona              = "NoClusterPersona"
	reasonClusterPersonaUnreadable      = "ClusterPersonaUnreadable"
)

// cpuThrottlingStatus is the `cpuThrottling` object of `dorgu health --json`.
type cpuThrottlingStatus struct {
	// State is on, off or unknown.
	State string `json:"state"`
	// Source is metrics.k8s.io, prometheus or none, as the operator reported it.
	Source string `json:"source,omitempty"`
	// Reason is the operator's CamelCase reason, or one of this CLI's own when the
	// state is unknown.
	Reason string `json:"reason,omitempty"`
	// Message is the operator's own sentence, when it wrote one.
	Message string `json:"message,omitempty"`
	// Note is something this CLI adds: today, that the ClusterPersona CRD is older
	// than the operator, so the state was read from the condition the operator keeps
	// for that case.
	Note string `json:"note,omitempty"`
}

// staleCRDNote is shown when the status field was pruned by an old CRD.
const staleCRDNote = "The ClusterPersona CRD is older than the operator (helm upgrade does not update CRDs): " +
	"run `kubectl apply -f` on the CRDs from the operator's chart to see the full statement."

// cpuThrottlingConditionType is the condition the operator keeps beside the status
// field, because an old CRD prunes the field but retains conditions.
const cpuThrottlingConditionType = "CPUThrottlingDetection"

// cpuThrottlingReasons say each operator reason in a phrase, for the plain output. An
// unknown reason is shown as the token itself.
var cpuThrottlingReasons = map[string]string{
	"MetricsServerActive":               "reading metrics.k8s.io",
	"PrometheusActive":                  "reading the Prometheus URL in the ClusterPersona",
	"PrometheusUnreachable":             "Prometheus did not answer, so metrics.k8s.io is used when it is served",
	"PrometheusNoData":                  "Prometheus answered with no CPU series, so metrics.k8s.io is used when it is served",
	"PrometheusURLInvalid":              "the Prometheus URL in the ClusterPersona is not usable, so metrics.k8s.io is used when it is served",
	"NoMetricsSource":                   "no metrics source: metrics.k8s.io is not served and no Prometheus URL is set in the ClusterPersona",
	"MetricsServerDisabled":             "the metrics.k8s.io collector is switched off in the operator",
	"MetricsSourceLost":                 "the metrics source stopped answering",
	"MetricsReadFailed":                 "the metrics source could not be read",
	reasonOperatorPredatesCPUThrottling: "operator older than v0.14, which does not report it",
	reasonNoClusterPersona:              "there is no ClusterPersona to read it from",
	reasonClusterPersonaUnreadable:      "the ClusterPersona could not be read",
}

// fetchCPUThrottling reads the ClusterPersona and reports its CPU throttling
// statement. It never returns nil: a read that fails is "unknown".
func fetchCPUThrottling(ctx context.Context, kubeconfig string) *cpuThrottlingStatus {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if err != nil {
		msg := kubectlErrText(out)
		reason := reasonClusterPersonaUnreadable
		if strings.Contains(msg, "the server doesn't have a resource type") {
			reason = reasonNoClusterPersona
		}
		return &cpuThrottlingStatus{State: cpuThrottlingUnknown, Reason: reason, Message: msg}
	}
	return parseCPUThrottling(out)
}

// parseCPUThrottling reads `kubectl get clusterpersona -o json`. The first
// ClusterPersona that carries status.cpuThrottlingDetection decides. With none
// carrying it, the first that carries the CPUThrottlingDetection condition decides
// (a v0.14 operator behind a stale CRD), with a note saying so. With neither the
// state is unknown and the operator is taken to predate the feature.
func parseCPUThrottling(raw []byte) *cpuThrottlingStatus {
	var list struct {
		Items []struct {
			Status struct {
				CPUThrottlingDetection *struct {
					Enabled bool   `json:"enabled"`
					Source  string `json:"source"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"cpuThrottlingDetection"`
				Conditions []statusCondition `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return &cpuThrottlingStatus{
			State: cpuThrottlingUnknown, Reason: reasonClusterPersonaUnreadable,
			Message: "could not read the ClusterPersona list: " + err.Error(),
		}
	}
	if len(list.Items) == 0 {
		return &cpuThrottlingStatus{State: cpuThrottlingUnknown, Reason: reasonNoClusterPersona}
	}
	for _, item := range list.Items {
		d := item.Status.CPUThrottlingDetection
		if d == nil {
			continue
		}
		s := &cpuThrottlingStatus{Source: d.Source, Reason: d.Reason, Message: d.Message}
		switch {
		case d.Enabled && d.Source != "" && d.Source != "none":
			s.State = cpuThrottlingOn
		case !d.Enabled:
			s.State = cpuThrottlingOff
		default:
			// Enabled with no usable source contradicts itself; do not call it on.
			return &cpuThrottlingStatus{
				State: cpuThrottlingUnknown, Reason: reasonClusterPersonaUnreadable,
				Message: "the ClusterPersona says detection is enabled but names no source",
			}
		}
		return s
	}
	for _, item := range list.Items {
		if c := findCondition(item.Status.Conditions, cpuThrottlingConditionType); c != nil {
			return cpuThrottlingFromCondition(c)
		}
	}
	return &cpuThrottlingStatus{State: cpuThrottlingUnknown, Reason: reasonOperatorPredatesCPUThrottling}
}

// cpuThrottlingFromCondition is the statement read from the operator's fallback
// condition: True is on (the condition does not say from which source, so it is
// "unknown"), False is off, anything else is unknown.
func cpuThrottlingFromCondition(c *statusCondition) *cpuThrottlingStatus {
	s := &cpuThrottlingStatus{Reason: c.Reason, Message: c.Message, Note: staleCRDNote}
	switch c.Status {
	case "True":
		s.State, s.Source = cpuThrottlingOn, "unknown"
	case "False":
		s.State = cpuThrottlingOff
	default:
		s.State = cpuThrottlingUnknown
	}
	return s
}

// printCPUThrottling renders the one-line statement, and under it what the operator
// said about why when that is more than the source.
func printCPUThrottling(w io.Writer, s *cpuThrottlingStatus) {
	if s == nil {
		return
	}
	switch s.State {
	case cpuThrottlingOn:
		fmt.Fprintf(w, "CPU throttling detection: %s (source: %s)\n", output.Green("on"), plainInline(s.Source))
		if s.Reason != "" && s.Reason != "MetricsServerActive" && s.Reason != "PrometheusActive" {
			fmt.Fprintln(w, output.Dim("  "+cpuThrottlingReason(s)))
		}
	case cpuThrottlingOff:
		fmt.Fprintf(w, "CPU throttling detection: %s\n", output.Yellow("off"))
		fmt.Fprintln(w, output.Dim("  "+cpuThrottlingReason(s)))
	default:
		fmt.Fprintf(w, "CPU throttling detection: %s (%s)\n", output.Yellow("unknown"), cpuThrottlingReason(s))
	}
	if s.Note != "" {
		fmt.Fprintln(w, output.Dim("  "+plainInline(s.Note)))
	}
	fmt.Fprintln(w)
}

// cpuThrottlingReason is the phrase for the state's reason: ours when we know the
// token, else the token and the operator's own message (shown as plain text).
func cpuThrottlingReason(s *cpuThrottlingStatus) string {
	if phrase, ok := cpuThrottlingReasons[s.Reason]; ok {
		return phrase
	}
	text := plainInline(strings.TrimSpace(s.Message))
	switch {
	case s.Reason != "" && text != "":
		return plainInline(s.Reason) + ": " + text
	case s.Reason != "":
		return plainInline(s.Reason)
	case text != "":
		return text
	}
	return "no reason given"
}
