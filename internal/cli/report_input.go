package cli

import (
	"encoding/json"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dorguv1 "github.com/dorgu-ai/dorgu-operator/api/v1"
	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
)

// The report reads the cluster into these shapes and computes from them alone,
// so the computation is a pure function of its inputs and a fixed clock.

// reportRA is the part of a RemediationAction the report reads.
type reportRA struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		PlanSource  string `json:"planSource"`
		IncidentRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"incidentRef"`
		Steps []struct {
			Safety []stepSafety `json:"safety"`
		} `json:"steps"`
	} `json:"spec"`
	Status struct {
		Phase              string `json:"phase"`
		ApprovedAt         string `json:"approvedAt"`
		AppliedAt          string `json:"appliedAt"`
		VerificationResult string `json:"verificationResult"`
		Gate               *struct {
			Phase    string `json:"phase"`
			Result   string `json:"result"`
			Verdicts []struct {
				Stage     string `json:"stage"`
				Guardrail string `json:"guardrail"`
				Verdict   string `json:"verdict"`
			} `json:"verdicts"`
		} `json:"gate"`
		Conditions []reportCondition `json:"conditions"`
	} `json:"status"`
}

// reportCondition is a status condition with its time, which the report needs
// and the heal path's statusCondition does not carry.
type reportCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

// reportIncident is the part of an IncidentMemory the report reads.
type reportIncident struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Detection struct {
			FirstSeen string `json:"firstSeen"`
		} `json:"detection"`
		Resolution *struct {
			Outcome string `json:"outcome"`
		} `json:"resolution"`
	} `json:"spec"`
	Status struct {
		Phase      string            `json:"phase"`
		Conditions []reportCondition `json:"conditions"`
	} `json:"status"`
}

// reportEvent is a core/v1 Event, whichever API generation wrote it.
type reportEvent struct {
	Reason         string `json:"reason"`
	Type           string `json:"type"`
	FirstTimestamp string `json:"firstTimestamp"`
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	Count          int    `json:"count"`
	Series         *struct {
		Count int `json:"count"`
	} `json:"series"`
	InvolvedObject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"involvedObject"`
}

// reportInputs is everything the report is computed from, as parsed from kubectl.
type reportInputs struct {
	Remediations []reportRA
	Incidents    []reportIncident
	Events       []reportEvent

	// EventsRead is true when the Events were listed. An empty list that was read
	// is a measured zero; one that was not read is no data.
	EventsRead bool

	// Errors are the cluster reads that failed. A section computed from one of them
	// is no data, never zero.
	Errors []report.InputError

	// OperatorVersion is the operator's image tag, nil when it could not be read.
	OperatorVersion *string
}

// toReport converts what kubectl printed into the operator's own types, which is
// what pkg/report computes from. The mirrors are kept (rather than decoding into
// the operator types directly) because they read a timestamp that does not parse
// as "no time", exactly as the report always has, where the operator types would
// refuse the whole list.
func (in reportInputs) toReport() report.Inputs {
	out := report.Inputs{
		Remediations:    make([]dorguv1.RemediationAction, len(in.Remediations)),
		Incidents:       make([]dorguv1.IncidentMemory, len(in.Incidents)),
		Events:          make([]corev1.Event, len(in.Events)),
		EventsRead:      in.EventsRead,
		Errors:          append([]report.InputError(nil), in.Errors...),
		OperatorVersion: in.OperatorVersion,
	}
	for i := range in.Remediations {
		out.Remediations[i] = in.Remediations[i].toOperator()
	}
	for i := range in.Incidents {
		out.Incidents[i] = in.Incidents[i].toOperator()
	}
	for i := range in.Events {
		out.Events[i] = in.Events[i].toOperator()
	}
	return out
}

// reportTime reads an RFC3339 timestamp. The empty string and anything
// unparseable are "no time": the zero metav1.Time, never a date in year 1 that
// something could subtract from.
func reportTime(s string) metav1.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return metav1.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return metav1.Time{}
	}
	return metav1.NewTime(t)
}

func reportTimePtr(s string) *metav1.Time {
	t := reportTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func conditionsToOperator(in []reportCondition) []metav1.Condition {
	out := make([]metav1.Condition, len(in))
	for i, c := range in {
		out[i] = metav1.Condition{
			Type: c.Type, Status: metav1.ConditionStatus(c.Status), Reason: c.Reason,
			LastTransitionTime: reportTime(c.LastTransitionTime),
		}
	}
	return out
}

func (r reportRA) toOperator() dorguv1.RemediationAction {
	out := dorguv1.RemediationAction{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.Metadata.Name, Namespace: r.Metadata.Namespace,
			CreationTimestamp: reportTime(r.Metadata.CreationTimestamp),
		},
		Spec: dorguv1.RemediationActionSpec{
			PlanSource: r.Spec.PlanSource,
			IncidentRef: dorguv1.IncidentReference{
				Name: r.Spec.IncidentRef.Name, Namespace: r.Spec.IncidentRef.Namespace,
			},
		},
		Status: dorguv1.RemediationActionStatus{
			Phase:              r.Status.Phase,
			ApprovedAt:         reportTimePtr(r.Status.ApprovedAt),
			AppliedAt:          reportTimePtr(r.Status.AppliedAt),
			VerificationResult: r.Status.VerificationResult,
			Conditions:         conditionsToOperator(r.Status.Conditions),
		},
	}
	for _, step := range r.Spec.Steps {
		safety := make([]dorguv1.StepSafety, len(step.Safety))
		for i, s := range step.Safety {
			safety[i] = dorguv1.StepSafety{Rule: s.Rule, Verdict: s.Verdict}
		}
		out.Spec.Steps = append(out.Spec.Steps, dorguv1.RemediationStep{Safety: safety})
	}
	if g := r.Status.Gate; g != nil {
		gate := &dorguv1.GateStatus{Phase: g.Phase, Result: g.Result}
		for _, v := range g.Verdicts {
			gate.Verdicts = append(gate.Verdicts, dorguv1.GateVerdict{Stage: v.Stage, Guardrail: v.Guardrail, Verdict: v.Verdict})
		}
		out.Status.Gate = gate
	}
	return out
}

func (i reportIncident) toOperator() dorguv1.IncidentMemory {
	out := dorguv1.IncidentMemory{
		ObjectMeta: metav1.ObjectMeta{
			Name: i.Metadata.Name, Namespace: i.Metadata.Namespace,
			CreationTimestamp: reportTime(i.Metadata.CreationTimestamp),
		},
		Spec: dorguv1.IncidentMemorySpec{
			Detection: dorguv1.DetectionInfo{FirstSeen: reportTime(i.Spec.Detection.FirstSeen)},
		},
		Status: dorguv1.IncidentMemoryStatus{
			Phase:      i.Status.Phase,
			Conditions: conditionsToOperator(i.Status.Conditions),
		},
	}
	if i.Spec.Resolution != nil {
		out.Spec.Resolution = &dorguv1.ResolutionInfo{Outcome: i.Spec.Resolution.Outcome}
	}
	return out
}

func (e reportEvent) toOperator() corev1.Event {
	out := corev1.Event{
		Reason: e.Reason, Type: e.Type, Count: int32(e.Count), //nolint:gosec // an Event count never nears int32
		FirstTimestamp: reportTime(e.FirstTimestamp),
		LastTimestamp:  reportTime(e.LastTimestamp),
		InvolvedObject: corev1.ObjectReference{Kind: e.InvolvedObject.Kind, Name: e.InvolvedObject.Name},
	}
	if t := reportTime(e.EventTime); !t.IsZero() {
		out.EventTime = metav1.NewMicroTime(t.Time)
	}
	if e.Series != nil {
		out.Series = &corev1.EventSeries{Count: int32(e.Series.Count)} //nolint:gosec // as above
	}
	return out
}

// parseReportList decodes a kubectl list into items of T.
func parseReportList[T any](raw []byte) ([]T, error) {
	var list struct {
		Items []T `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
