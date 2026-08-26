package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/api/resource"
)

// CR-03 — `dorgu health` reported 1689% CPU saturation on a cluster where 25%
// was requested and 1% was in use. These fixtures are that cluster's shape: a
// small node pool, a modest scheduled workload, and a backlog of pods no node
// will take.

// twoNodeListFixture is 2 x 1930m / 3654Mi allocatable, which is what an EKS
// t3.medium pair reports.
const twoNodeListFixture = `{"items":[
  {"metadata":{"name":"ip-10-0-1-10"},"status":{"allocatable":{"cpu":"1930m","memory":"3654Mi","pods":"17"}}},
  {"metadata":{"name":"ip-10-0-2-20"},"status":{"allocatable":{"cpu":"1930m","memory":"3654Mi","pods":"17"}}}
]}`

// saturationPodListFixture holds three scheduled pods claiming 950m in total,
// and three unschedulable ones asking for 32 CPUs each. Summing all six against
// 3860m allocatable is the 1689% defect (here it would read 2512%); summing the
// scheduled three is 25%.
const saturationPodListFixture = `{"items":[
  {"metadata":{"name":"web-1"},"spec":{"nodeName":"ip-10-0-1-10",
    "containers":[{"resources":{"requests":{"cpu":"250m","memory":"256Mi"}}}]},
   "status":{"phase":"Running"}},
  {"metadata":{"name":"api-1"},"spec":{"nodeName":"ip-10-0-2-20",
    "containers":[{"resources":{"requests":{"cpu":"500m","memory":"512Mi"}}}]},
   "status":{"phase":"Running"}},
  {"metadata":{"name":"job-1"},"spec":{"nodeName":"ip-10-0-1-10",
    "containers":[{"resources":{"requests":{"cpu":"15m","memory":"32Mi"}}}],
    "initContainers":[{"resources":{"requests":{"cpu":"200m","memory":"64Mi"}}}]},
   "status":{"phase":"Running"}},
  {"metadata":{"name":"too-big-1"},"spec":{"nodeName":"",
    "containers":[{"resources":{"requests":{"cpu":"32","memory":"64Gi"}}}]},
   "status":{"phase":"Pending"}},
  {"metadata":{"name":"too-big-2"},"spec":{"nodeName":"",
    "containers":[{"resources":{"requests":{"cpu":"32","memory":"64Gi"}}}]},
   "status":{"phase":"Pending"}},
  {"metadata":{"name":"too-big-3"},"spec":{"nodeName":"",
    "containers":[{"resources":{"requests":{"cpu":"32","memory":"64Gi"}}}]},
   "status":{"phase":"Pending"}}
]}`

// nodeMetricsFixture is metrics-server reporting a nearly idle cluster: the "1%
// used" half of the finding.
const nodeMetricsFixture = `{"kind":"NodeMetricsList","items":[
  {"metadata":{"name":"ip-10-0-1-10"},"usage":{"cpu":"21m","memory":"512Mi"}},
  {"metadata":{"name":"ip-10-0-2-20"},"usage":{"cpu":"17m","memory":"498Mi"}}
]}`

// The finding, end to end. Unschedulable pods are excluded, so the number is
// believable, and the same three pods are reported as the real problem they are.
func TestSaturationExcludesUnscheduledPods(t *testing.T) {
	capacity, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)
	claims, err := parsePodClaims([]byte(saturationPodListFixture))
	require.NoError(t, err)
	usage, err := parseNodeUsage([]byte(nodeMetricsFixture))
	require.NoError(t, err)

	sat := buildSaturation(capacity, claims, usage, nil)

	require.NotNil(t, sat.CPU)
	assert.Equal(t, "3860m", sat.CPU.Allocatable)
	// 250m + 500m + max(15m, 200m) = 950m over 3860m.
	assert.Equal(t, "950m", sat.CPU.Requested)
	assert.Equal(t, "25%", sat.CPU.RequestedPercent, "the three unschedulable pods claim nothing")
	assert.Equal(t, "38m", sat.CPU.Used)
	assert.Equal(t, "1%", sat.CPU.UsedPercent)

	assert.Equal(t, 3, sat.ScheduledPods)
	assert.Equal(t, 3, sat.UnscheduledPods)
	assert.Equal(t, 2, sat.Nodes)

	// Had they been counted: 950m + 96000m = 96950m over 3860m, i.e. 2512%.
	assert.NotEqual(t, "2512%", sat.CPU.RequestedPercent)
}

// The init container floors the pod's claim rather than adding to it, because
// init containers run before the app containers rather than alongside them.
func TestPodRequestsInitContainerFloorsRatherThanAdds(t *testing.T) {
	claims, err := parsePodClaims([]byte(`{"items":[
      {"spec":{"nodeName":"n1",
        "containers":[{"resources":{"requests":{"cpu":"15m","memory":"32Mi"}}}],
        "initContainers":[{"resources":{"requests":{"cpu":"200m","memory":"64Mi"}}}]},
       "status":{"phase":"Running"}}]}`))
	require.NoError(t, err)

	assert.Equal(t, int64(200), claims.CPU.MilliValue(), "max(15m, 200m), not 215m")
	assert.Equal(t, int64(64*1024*1024), claims.Memory.Value())
}

// Terminal pods have released their allocation. The field selector should keep
// them out, but a cluster that ignores it must not skew the figure.
func TestPodClaimsSkipsTerminalPods(t *testing.T) {
	claims, err := parsePodClaims([]byte(`{"items":[
      {"spec":{"nodeName":"n1","containers":[{"resources":{"requests":{"cpu":"100m"}}}]},
       "status":{"phase":"Running"}},
      {"spec":{"nodeName":"n1","containers":[{"resources":{"requests":{"cpu":"4"}}}]},
       "status":{"phase":"Succeeded"}},
      {"spec":{"nodeName":"n1","containers":[{"resources":{"requests":{"cpu":"8"}}}]},
       "status":{"phase":"Failed"}}]}`))
	require.NoError(t, err)

	assert.Equal(t, 1, claims.Scheduled)
	assert.Equal(t, 0, claims.Unscheduled, "a completed pod is not an unscheduled one")
	assert.Equal(t, int64(100), claims.CPU.MilliValue())
}

// A container with no requests requests nothing. That is a real answer, not a
// parse failure, and it must not stop the section reporting.
func TestPodClaimsToleratesMissingAndMalformedRequests(t *testing.T) {
	claims, err := parsePodClaims([]byte(`{"items":[
      {"spec":{"nodeName":"n1","containers":[{}]},"status":{"phase":"Running"}},
      {"spec":{"nodeName":"n1","containers":[{"resources":{"requests":{"cpu":"not-a-quantity"}}}]},
       "status":{"phase":"Running"}},
      {"spec":{"nodeName":"n1","containers":[{"resources":{"requests":{"cpu":"300m"}}}]},
       "status":{"phase":"Running"}}]}`))
	require.NoError(t, err)

	assert.Equal(t, 3, claims.Scheduled)
	assert.Equal(t, int64(300), claims.CPU.MilliValue())
}

func TestParseNodeCapacity(t *testing.T) {
	c, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)
	assert.Equal(t, 2, c.Nodes)
	assert.Equal(t, int64(3860), c.CPU.MilliValue())
	assert.Equal(t, int64(2*3654*1024*1024), c.Memory.Value())

	_, err = parseNodeCapacity([]byte(`not json`))
	assert.Error(t, err)
}

func TestParseNodeUsageErrors(t *testing.T) {
	_, err := parseNodeUsage([]byte(`not json`))
	assert.Error(t, err)

	_, err = parseNodeUsage([]byte(`{"items":[]}`))
	assert.Error(t, err, "no nodes reported is not a usage of zero")
}

// --- rendering ---

func renderSaturation(t *testing.T, s *resourceSaturation) string {
	t.Helper()
	var buf bytes.Buffer
	printResourceSaturation(&buf, s)
	return buf.String()
}

func TestPrintResourceSaturationSeparatesRequestedFromUsed(t *testing.T) {
	capacity, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)
	claims, err := parsePodClaims([]byte(saturationPodListFixture))
	require.NoError(t, err)
	usage, err := parseNodeUsage([]byte(nodeMetricsFixture))
	require.NoError(t, err)

	out := renderSaturation(t, buildSaturation(capacity, claims, usage, nil))

	assert.Contains(t, out, "REQUESTED")
	assert.Contains(t, out, "USED")
	assert.Contains(t, out, "950m / 3860m (25%)")
	assert.Contains(t, out, "38m / 3860m (1%)")
	assert.Contains(t, out, "3 pods are not scheduled")
	assert.Contains(t, out, "2 node(s), 3 scheduled pod(s)")
	assert.NotContains(t, out, "1689")

	// The old single line is gone: it is the thing that conflated the two.
	assert.NotContains(t, out, "requests / allocatable")
}

// Without metrics-server there is no used figure, and the output says which of
// "zero" and "not measured" it means.
func TestPrintResourceSaturationNamesWhyUsedIsMissing(t *testing.T) {
	capacity, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)
	claims, err := parsePodClaims([]byte(saturationPodListFixture))
	require.NoError(t, err)

	sat := buildSaturation(capacity, claims, nil,
		assertableError("metrics-server did not answer: the server could not find the requested resource"))
	out := renderSaturation(t, sat)

	assert.Contains(t, out, "n/a (metrics-server did not answer")
	assert.Contains(t, out, "950m / 3860m (25%)", "requested still reports without metrics-server")
	assert.NotContains(t, out, "(0m /", "an unmeasured value is never rendered as zero")
}

// F-09's invariant, carried forward onto the new shape: no rendered figure may
// have an empty operand.
func TestPrintResourceSaturationNeverPrintsAnEmptyOperand(t *testing.T) {
	cases := []*resourceSaturation{
		buildSaturation(nodeCapacity{}, podClaims{}, nil, nil),
		buildSaturation(nodeCapacity{CPU: resource.MustParse("4"), Nodes: 1}, podClaims{}, nil, nil),
		{CPU: &saturationDetail{Allocatable: "3860m", Requested: "0m", RequestedPercent: "0%"}},
	}
	for _, s := range cases {
		for _, line := range strings.Split(renderSaturation(t, s), "\n") {
			assert.NotContains(t, line, "( /")
			assert.NotContains(t, line, "/ )")
			assert.NotContains(t, line, "()")
		}
	}
}

// An idle cluster reports a real zero. "n/a" would be a claim that nothing was
// measured, which is different.
func TestPrintResourceSaturationIdleClusterReportsZero(t *testing.T) {
	capacity, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)

	out := renderSaturation(t, buildSaturation(capacity, podClaims{},
		&nodeUsage{}, nil))

	assert.Contains(t, out, "0m / 3860m (0%)")
	assert.NotContains(t, out, "n/a")
}

// A cluster with no readable allocatable prints no saturation section at all,
// rather than a section full of "n/a".
func TestPrintResourceSaturationSilentWithoutAllocatable(t *testing.T) {
	assert.Empty(t, renderSaturation(t, buildSaturation(nodeCapacity{}, podClaims{}, nil, nil)))
	assert.Empty(t, renderSaturation(t, nil))
}

// Above the threshold the output says the cluster is booked out, so the reader
// does not have to compare two numbers the old line had already merged.
func TestPrintResourceSaturationWarnsWhenBookedOut(t *testing.T) {
	capacity := nodeCapacity{CPU: resource.MustParse("4"), Nodes: 1}
	claims := podClaims{CPU: resource.MustParse("3800m"), Scheduled: 9}

	out := renderSaturation(t, buildSaturation(capacity, claims, nil, nil))
	assert.Contains(t, out, "CPU requests are at 95% of allocatable")

	quiet := renderSaturation(t, buildSaturation(capacity,
		podClaims{CPU: resource.MustParse("1000m"), Scheduled: 3}, nil, nil))
	assert.NotContains(t, quiet, "of allocatable; new pods may not schedule")
}

func TestFormatMemory(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"512", "512"},
		{"4Ki", "4Ki"},
		{"256Mi", "256Mi"},
		{"1536Mi", "1.5Gi"},
		{"7308Mi", "7.1Gi"},
		{"64Gi", "64.0Gi"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatMemory(resource.MustParse(tt.in)), tt.in)
	}
}

func TestFormatCPUAndPercentOf(t *testing.T) {
	assert.Equal(t, "3860m", formatCPU(resource.MustParse("3860m")))
	assert.Equal(t, "4000m", formatCPU(resource.MustParse("4")))
	assert.Equal(t, "0m", formatCPU(resource.Quantity{}))

	assert.Equal(t, "25%", percentOf(950, 3860))
	assert.Equal(t, "0%", percentOf(0, 3860))
	assert.Equal(t, "n/a", percentOf(950, 0), "a zero denominator is unknown, not 0%")
	assert.Equal(t, "n/a", percentOf(950, -1))
}

func TestPercentValue(t *testing.T) {
	pct, ok := percentValue("95%")
	assert.True(t, ok)
	assert.Equal(t, 95, pct)

	_, ok = percentValue("n/a")
	assert.False(t, ok, "n/a is not a number and must not compare as one")
}

func TestPluralPods(t *testing.T) {
	assert.Equal(t, "1 pod is", pluralPods(1))
	assert.Equal(t, "it is", isAre(1))
	assert.Equal(t, "3 pods are", pluralPods(3))
	assert.Equal(t, "they are", isAre(3))
}

// assertableError is a fixed error for the "why is used missing" path.
type assertableError string

func (e assertableError) Error() string { return string(e) }

// --- end to end through kubectl ---

// writeFakeKubectlSaturation installs a fake kubectl that answers the three
// calls the collector makes: nodes, pods, and the metrics-server raw endpoint.
// An empty metrics fixture makes the raw call fail, which is what a cluster
// without metrics-server does. Cannot be used with t.Parallel.
func writeFakeKubectlSaturation(t *testing.T, nodes, pods, metrics string) (podArgs string) {
	t.Helper()
	dir := t.TempDir()

	write := func(base, content string) string {
		p := filepath.Join(dir, base)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}
	nodesFile := write("nodes.json", nodes)
	podsFile := write("pods.json", pods)
	metricsFile := write("metrics.json", metrics)
	podArgs = filepath.Join(dir, "pod-args.log")

	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *--raw*) if [ -s " + metricsFile + " ]; then cat " + metricsFile + "; exit 0; fi\n" +
		"    echo 'Error from server (NotFound): the server could not find the requested resource' >&2; exit 1 ;;\n" +
		"  *pods*) echo \"$@\" > " + podArgs + "; cat " + podsFile + "; exit 0 ;;\n" +
		"  *nodes*) cat " + nodesFile + "; exit 0 ;;\n" +
		"esac\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return podArgs
}

func TestCollectResourceSaturation(t *testing.T) {
	podArgs := writeFakeKubectlSaturation(t,
		twoNodeListFixture, saturationPodListFixture, nodeMetricsFixture)

	sat, err := collectResourceSaturation(t.Context(), "", []byte(twoNodeListFixture))
	require.NoError(t, err)
	require.NotNil(t, sat.CPU)

	assert.Equal(t, "25%", sat.CPU.RequestedPercent)
	assert.Equal(t, "1%", sat.CPU.UsedPercent)
	assert.Equal(t, 3, sat.UnscheduledPods)

	// Terminal pods are excluded by the API server rather than fetched and
	// thrown away, because on a cluster with a large Job history that is most of
	// the response.
	args := readPatchLog(t, podArgs)
	assert.Contains(t, args, "--all-namespaces")
	assert.Contains(t, args, "status.phase!=Succeeded,status.phase!=Failed")
}

// metrics-server is not installed by default, so its absence must degrade the
// used figure rather than fail the section.
func TestCollectResourceSaturationWithoutMetricsServer(t *testing.T) {
	writeFakeKubectlSaturation(t, twoNodeListFixture, saturationPodListFixture, "")

	sat, err := collectResourceSaturation(t.Context(), "", []byte(twoNodeListFixture))
	require.NoError(t, err)
	require.NotNil(t, sat.CPU)

	assert.Equal(t, "25%", sat.CPU.RequestedPercent, "requested does not need metrics-server")
	assert.Empty(t, sat.CPU.Used)
	assert.Contains(t, sat.UsedUnavailable, "metrics-server did not answer")
}

// An unreadable pod list means there is no requested figure, and saying so beats
// reporting saturation computed over no pods as a quiet 0%.
func TestCollectResourceSaturationFailsWhenPodsUnreadable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(
		"#!/bin/sh\necho 'Error from server (Forbidden): pods is forbidden' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := collectResourceSaturation(t.Context(), "", []byte(twoNodeListFixture))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not list pods")
}
