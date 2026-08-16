package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// realCleanroomOutput is the exact text `dorgu health` surfaced in the
// 2026-08-09 clean-room run (artifact h.out): five identical client-go klog
// lines in front of the one line that says what actually happened.
const realCleanroomOutput = `E0809 23:43:37.498989   72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: the server could not find the requested resource"
E0809 23:43:37.499844   72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: the server could not find the requested resource"
E0809 23:43:37.500651   72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: the server could not find the requested resource"
E0809 23:43:37.501613   72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: the server could not find the requested resource"
E0809 23:43:37.502413   72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: the server could not find the requested resource"
Error from server (NotFound): the server could not find the requested resource`

func TestKubectlErrText_StripsTheCleanroomNoise(t *testing.T) {
	t.Setenv(debugEnvVar, "")

	got := kubectlErrText([]byte(realCleanroomOutput))

	want := "Error from server (NotFound): the server could not find the requested resource"
	if got != want {
		t.Errorf("expected only the real error\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "memcache.go") {
		t.Error("klog noise survived filtering")
	}
}

func TestKubectlErrText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
		{
			name: "a plain kubectl error is untouched",
			in:   `Error from server (NotFound): deployments.apps "web" not found`,
			want: `Error from server (NotFound): deployments.apps "web" not found`,
		},
		{
			name: "trims surrounding whitespace",
			in:   "\n  error: no configuration has been provided  \n",
			want: "error: no configuration has been provided",
		},
		{
			name: "drops info and warning klog lines too",
			in: "I0809 23:43:37.100000   100 loader.go:395] Config loaded\n" +
				"W0809 23:43:37.200000   100 warnings.go:70] deprecated API\n" +
				"error: the server doesn't have a resource type \"remediationaction\"",
			want: `error: the server doesn't have a resource type "remediationaction"`,
		},
		{
			name: "keeps a multi-line kubectl error intact",
			in:   "error: unable to recognize \"x.yaml\":\nno matches for kind \"Foo\"",
			want: "error: unable to recognize \"x.yaml\":\nno matches for kind \"Foo\"",
		},
		{
			name: "a message that merely mentions a timestamp is not klog",
			in:   "error: certificate expired at E0809 23:43:37",
			want: "error: certificate expired at E0809 23:43:37",
		},
		{
			name: "output that is only klog is returned rather than swallowed",
			in:   `E0809 23:43:37.498989   72374 memcache.go:265] "Unhandled Error" err="boom"`,
			want: `E0809 23:43:37.498989   72374 memcache.go:265] "Unhandled Error" err="boom"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(debugEnvVar, "")
			if got := kubectlErrText([]byte(tt.in)); got != tt.want {
				t.Errorf("kubectlErrText()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestKubectlErrText_DebugKeepsEverything: the noise is filtered, not deleted.
// Anyone debugging kubectl itself has to be able to get it back.
func TestKubectlErrText_DebugKeepsEverything(t *testing.T) {
	t.Setenv(debugEnvVar, "1")

	got := kubectlErrText([]byte(realCleanroomOutput))
	if !strings.Contains(got, "memcache.go:265") {
		t.Error("DORGU_DEBUG must keep the raw kubectl output")
	}
	if strings.Count(got, "memcache.go:265") != 5 {
		t.Errorf("expected all 5 klog lines, got %d", strings.Count(got, "memcache.go:265"))
	}
}

// TestKubectlFailure_StripsNoise guards the highest-visibility F-14 path: an
// unreachable API server is the first thing `dorgu health` checks, and kubectl
// puts its klog output on stderr, which is exactly what kubectlFailure reads.
func TestKubectlFailure_StripsNoise(t *testing.T) {
	t.Setenv(debugEnvVar, "")

	err := &exec.ExitError{
		ProcessState: &os.ProcessState{},
		Stderr:       []byte(realCleanroomOutput),
	}

	got := kubectlFailure(err)

	want := "Error from server (NotFound): the server could not find the requested resource"
	if got != want {
		t.Errorf("expected only the real error\n got: %q\nwant: %q", got, want)
	}
}

// TestKubectlFailure_FallsBackToProcessError: with no stderr there is still an
// error, and it must not come back empty.
func TestKubectlFailure_FallsBackToProcessError(t *testing.T) {
	t.Setenv(debugEnvVar, "")

	if got := kubectlFailure(errors.New("exec: \"kubectl\": executable file not found in $PATH")); got == "" {
		t.Error("a failure with no stderr must still report something")
	}
}

// TestUnreadableIncidents_StripsNoise: `dorgu health` embeds kubectl's output in
// the reason it shows for an unreadable incident list.
func TestUnreadableIncidents_StripsNoise(t *testing.T) {
	t.Setenv(debugEnvVar, "")

	got := unreadableIncidents([]byte(realCleanroomOutput))

	if strings.Contains(got.Reason, "memcache.go") {
		t.Errorf("klog noise leaked into the health summary: %q", got.Reason)
	}
	if !strings.Contains(got.Reason, "Error from server (NotFound)") {
		t.Errorf("the real reason was lost: %q", got.Reason)
	}
}

// The operator-not-installed answer is a different, better message and must
// survive filtering unchanged.
func TestUnreadableIncidents_KeepsTheOperatorHint(t *testing.T) {
	t.Setenv(debugEnvVar, "")

	out := `E0809 23:43:37.678229   72385 memcache.go:265] "Unhandled Error" err="boom"
error: the server doesn't have a resource type "incidentmemory"`

	got := unreadableIncidents([]byte(out))

	if !strings.Contains(got.Reason, "the dorgu operator is not installed") {
		t.Errorf("filtering broke the operator-missing hint: %q", got.Reason)
	}
}
