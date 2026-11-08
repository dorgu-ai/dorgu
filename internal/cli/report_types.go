package cli

// reportSchema names the JSON shape `dorgu report --json` prints. It changes only
// when a field is removed or its meaning changes; the golden test pins it.
const reportSchema = "dorgu.report/v1"

// The report never prints a zero as if it were a measurement. A ratio or a
// percentile that has no sample behind it is null with no_data true, a count is
// an integer only where the cluster could be read, and anything the cluster
// cannot tell us (A1, A3, A4, A8, A9, A10) is listed under not_measurable.

// reportDoc is the whole report.
type reportDoc struct {
	Schema      string        `json:"schema"`
	GeneratedAt string        `json:"generated_at"`
	Window      reportWindow  `json:"window"`
	Cluster     reportCluster `json:"cluster"`
	Sources     reportSources `json:"sources"`

	// HealAccounting is how every RemediationAction created in the window ended,
	// and which of them are clean heals under KPIS.md.
	HealAccounting reportHealAccounting `json:"heal_accounting"`

	TierA reportTierA `json:"tier_a"`
	TierQ reportTierQ `json:"tier_q"`

	// FirstRunClamps is what the guardrails narrowed before a human saw a plan.
	FirstRunClamps reportClamps `json:"first_run_clamps"`

	// CLIDeclines are refusals the CLI wrote as Events on the RemediationAction.
	CLIDeclines reportCLIDeclines `json:"cli_declines"`

	// NotMeasurable lists the KPIs a cluster cannot report, so their absence
	// reads as "not measured" and never as zero.
	NotMeasurable []reportNotMeasurable `json:"not_measurable"`
}

type reportWindow struct {
	Since string `json:"since"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type reportCluster struct {
	Context         string   `json:"context"`
	Namespace       string   `json:"namespace"`
	OperatorVersion *string  `json:"operator_version"`
	Personas        *int     `json:"application_personas"`
	SelfHealing     []string `json:"self_healing_modes"`
}

// reportSources says which cluster reads worked. A report built from a partial
// read says so rather than presenting the gap as zero.
type reportSources struct {
	RemediationActions reportSource `json:"remediation_actions"`
	Incidents          reportSource `json:"incidents"`
	Personas           reportSource `json:"personas"`
	Events             reportSource `json:"events"`
}

type reportSource struct {
	OK    bool   `json:"ok"`
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

// --- heal accounting ---

type reportHealAccounting struct {
	// Total is the RemediationActions created in the window.
	Total int `json:"total"`

	// Completed reached phase Completed. Clean is the subset KPIS.md counts as a
	// heal: HealCredited=True and the incident outcome resolved.
	Completed int `json:"completed"`
	Clean     int `json:"clean"`

	// SafeHeals is the north star (NS): clean heals whose approval-time gate run
	// passed (status.gate is an ApprovalRecheck that cleared).
	SafeHeals int `json:"safe_heals"`

	// Excluded is every RemediationAction that is not a clean heal, by why.
	Excluded map[string]int `json:"excluded"`

	// HealNotCredited breaks HealCredited=False down by its reason.
	HealNotCredited map[string]int `json:"heal_not_credited_by_reason"`
}

// Exclusion categories. Each RemediationAction lands in exactly one of Clean or
// these, so Total = Clean + the sum of Excluded.
const (
	excludedPartial         = "partial"
	excludedSelfResolved    = "self_resolved"
	excludedHealNotCredited = "heal_not_credited"
	excludedNoCreditRecord  = "no_credit_record"
	excludedOutcomeUnknown  = "outcome_unconfirmed"
	excludedRolledBack      = "rolled_back"
	excludedFailed          = "failed"
	excludedRejected        = "rejected"
	excludedExpired         = "expired"
	excludedAcknowledged    = "acknowledged"
	excludedRefusedAtGate   = "refused_by_gate"
	excludedInFlight        = "in_flight"
)

// allExclusions is every category, so the report lists all of them with their
// counts, zeros included: a zero here is a measurement.
var allExclusions = []string{
	excludedPartial, excludedSelfResolved, excludedHealNotCredited, excludedNoCreditRecord,
	excludedOutcomeUnknown, excludedRolledBack, excludedFailed, excludedRejected,
	excludedExpired, excludedAcknowledged, excludedRefusedAtGate, excludedInFlight,
}

// Refusal phases and the stage the CLI cannot read off the gate.
const (
	reportPhaseProposal = "proposal"
	reportPhaseApproval = "approval"
	reportPhaseApply    = "apply"
	reportStageTicket   = "Ticket"
	reportStageUnknown  = "Unknown"

	// reportNoReasonGiven counts a rejection that carries no RejectionReason.
	reportNoReasonGiven = "no-reason-given"

	// reportMinSampleForFlag and reportRefusalFlagShare: a refusal reason is
	// flagged (KPI Q6) when it is over 30% of proposals on at least ten of them.
	reportMinSampleForFlag = 10
	reportRefusalFlagShare = 0.30
)

// --- tier A ---

type reportTierA struct {
	// NorthStar mirrors heal_accounting.safe_heals so the headline number is
	// where KPIS.md says to look for it.
	NorthStar    reportNorthStar `json:"ns_safe_heals"`
	A2Active     reportActive    `json:"a2_active_cluster"`
	A5Depth      reportDepth     `json:"a5_review_depth"`
	A6Approval   reportApproval  `json:"a6_approval_rate"`
	A7External   reportNoData    `json:"a7_external_agent_share"`
	Reviewed     reportDecisions `json:"decisions"`
	ClusterNotes []string        `json:"notes"`
}

type reportNorthStar struct {
	SafeHeals int `json:"safe_heals"`
	CleanHeal int `json:"clean_heals"`
}

// reportActive is A2: a cluster is active in the window when at least one
// proposal was decided. Reviews done with `diff` leave no record, so a decision
// is the measurable proxy.
type reportActive struct {
	Active           bool `json:"active"`
	ProposalsDecided int  `json:"proposals_decided"`
}

type reportDepth struct {
	ProposalsDecided int      `json:"proposals_decided"`
	WindowDays       float64  `json:"window_days"`
	PerWeek          *float64 `json:"per_week"`
	NoData           bool     `json:"no_data"`
}

type reportApproval struct {
	Approved int      `json:"approved"`
	Rejected int      `json:"rejected"`
	Rate     *float64 `json:"rate"`
	NoData   bool     `json:"no_data"`
	// Band is "below" (under 30%, noisy proposals), "in" or "above" (over 80%,
	// rubber-stamping). Empty with no_data.
	Band string `json:"band,omitempty"`
}

type reportDecisions struct {
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
}

type reportNoData struct {
	NoData bool   `json:"no_data"`
	Reason string `json:"reason"`
}

// --- tier Q ---

type reportTierQ struct {
	Q1TimeToHeal reportTimeToHeal `json:"q1_time_to_heal"`
	Q2Precision  reportPrecision  `json:"q2_proposal_precision"`
	Q3LLM        reportLLM        `json:"q3_llm_efficiency"`
	Q6Refusals   reportRefusals   `json:"q6_refusal_mix"`
}

type reportTimeToHeal struct {
	DetectToProposal   reportDistribution `json:"detect_to_proposal"`
	ProposalToDecision reportDistribution `json:"proposal_to_decision"`
	DecisionToApplied  reportDistribution `json:"decision_to_applied"`
	AppliedToVerified  reportDistribution `json:"applied_to_verified"`
}

// reportDistribution is p50/p90 of one stage in seconds. N is the number of
// RemediationActions that had both timestamps.
type reportDistribution struct {
	N          int      `json:"n"`
	P50Seconds *float64 `json:"p50_seconds"`
	P90Seconds *float64 `json:"p90_seconds"`
	NoData     bool     `json:"no_data"`
}

type reportPrecision struct {
	Decided int      `json:"decided"`
	Wrong   int      `json:"wrong"`
	Rate    *float64 `json:"rate"`
	NoData  bool     `json:"no_data"`
	// RejectionsByReason counts rejected proposals by the enum `reject --reason`
	// writes, plus no-reason-given for a rejection that carries none.
	RejectionsByReason map[string]int `json:"rejections_by_reason"`
}

type reportLLM struct {
	NoData bool   `json:"no_data"`
	Reason string `json:"reason"`
	// PlanSources counts RemediationActions by how the plan was produced.
	PlanSources map[string]int `json:"plan_sources"`
}

type reportRefusals struct {
	// Proposals is the denominator: RemediationActions created plus proposals
	// the gate refused before one was written.
	Proposals int             `json:"proposals"`
	NoData    bool            `json:"no_data"`
	Rows      []reportRefusal `json:"rows"`
}

type reportRefusal struct {
	Phase  string `json:"phase"`
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	// ShareOfProposals is Count over Proposals.
	ShareOfProposals *float64 `json:"share_of_proposals"`
	// Flagged marks a reason above 30% of proposals on a sample of at least ten.
	Flagged bool `json:"flagged"`
}

// --- clamps and CLI declines ---

type reportClamps struct {
	// ProposalsWithClamp is RemediationActions with at least one step whose
	// safety carries a clamped verdict (spec.steps[].safety): the first-run
	// clamp, which status.gate does not show because it records the seal.
	ProposalsWithClamp int `json:"proposals_with_clamp"`
	// StepVerdicts counts spec.steps[].safety entries by verdict.
	StepVerdicts map[string]int `json:"step_verdicts"`
	// ClampsByRule counts clamped entries by the guardrail that clamped.
	ClampsByRule map[string]int `json:"clamps_by_rule"`
	// GateClamps counts Clamp verdicts in status.gate by guardrail.
	GateClamps map[string]int `json:"gate_clamps_by_guardrail"`
	NoData     bool           `json:"no_data"`
}

type reportCLIDeclines struct {
	NoData   bool           `json:"no_data"`
	ByReason map[string]int `json:"by_reason"`
	Note     string         `json:"note"`
}

type reportNotMeasurable struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
