package cli

import (
	"encoding/json"
	"strings"
	"time"
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

// at is when the event happened: the newest timestamp it carries.
func (e reportEvent) at() (time.Time, bool) {
	for _, s := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if t, ok := parseReportTime(s); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// occurrences is how many times the event fired (Kubernetes folds repeats).
func (e reportEvent) occurrences() int {
	if e.Series != nil && e.Series.Count > 0 {
		return e.Series.Count
	}
	if e.Count > 0 {
		return e.Count
	}
	return 1
}

// reportInputs is everything the report is computed from.
type reportInputs struct {
	Remediations []reportRA
	Incidents    []reportIncident
	Events       []reportEvent

	// EventsRead is true when the Events were listed. An empty list that was read
	// is a measured zero; one that was not read is no data.
	EventsRead bool

	// Errors are the cluster reads that failed. A section computed from one of them
	// is no data (see buildReport), never zero.
	Errors []reportInputError

	// OperatorVersion is the operator's image tag, nil when it could not be read.
	OperatorVersion *string
}

// unreadable reports whether the named input failed to read.
func (in reportInputs) unreadable(input string) bool {
	for _, e := range in.Errors {
		if e.Input == input {
			return true
		}
	}
	return false
}

// parseReportTime reads an RFC3339 timestamp. The empty string and anything
// unparseable are "no time", never the zero time.
func parseReportTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (r *reportRA) condition(condType string) *reportCondition {
	for i := range r.Status.Conditions {
		if r.Status.Conditions[i].Type == condType {
			return &r.Status.Conditions[i]
		}
	}
	return nil
}

func (i *reportIncident) condition(condType string) *reportCondition {
	for k := range i.Status.Conditions {
		if i.Status.Conditions[k].Type == condType {
			return &i.Status.Conditions[k]
		}
	}
	return nil
}

func (r *reportRA) created() (time.Time, bool) {
	return parseReportTime(r.Metadata.CreationTimestamp)
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

// condTime is when a condition last changed.
func condTime(c *reportCondition) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	return parseReportTime(c.LastTransitionTime)
}
