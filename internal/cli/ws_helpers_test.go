package cli

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleWSConnectError_DNSFailure(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := handleWSConnectError(
		fmt.Errorf("dial tcp: lookup operator.dorgu-system.svc.cluster.local: no such host"),
		"ws://operator.dorgu-system.svc.cluster.local:9090/ws",
	)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to operator")
	assert.Contains(t, out, "port-forward")
}

func TestHandleWSConnectError_ConnectionRefused(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := handleWSConnectError(
		fmt.Errorf("dial tcp 127.0.0.1:9090: connection refused"),
		"ws://localhost:9090/ws",
	)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Error(t, err)
	assert.Contains(t, out, "websocket.enabled=true")
}

func TestHandleWSConnectError_Timeout(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := handleWSConnectError(
		fmt.Errorf("dial tcp: i/o timeout"),
		"ws://localhost:9090/ws",
	)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Error(t, err)
	assert.Contains(t, out, "port-forward")
}

func TestHandleWSConnectError_Default(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := handleWSConnectError(
		fmt.Errorf("some unknown websocket error"),
		"ws://localhost:9090/ws",
	)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Error(t, err)
	assert.Contains(t, out, "websocket.enabled=true")
}
