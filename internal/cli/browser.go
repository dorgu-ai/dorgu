package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// This file is the one place besides kubectl_cmd.go that starts a process, and
// it starts a browser, never kubectl. TestNoRawKubectlInvocations allows it by
// name, so any other exec.Command in the package still fails the build.

// browserCommand names the program that opens a URL on an operating system.
// It returns false where there is no known one.
func browserCommand(goos, url string) (name string, args []string, ok bool) {
	switch goos {
	case "darwin":
		return "open", []string{url}, true
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return "xdg-open", []string{url}, true
	}
	return "", nil, false
}

// openURL starts the system browser on url without waiting for it.
func openURL(url string) error {
	name, args, ok := browserCommand(runtime.GOOS, url)
	if !ok {
		return fmt.Errorf("no known way to open a browser on %s", runtime.GOOS)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not installed", name)
	}
	cmd := exec.Command(path, args...) // #nosec G204 -- fixed program, URL built by this process
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the child so it does not linger as a zombie for the life of the server.
	go func() { _ = cmd.Wait() }()
	return nil
}
