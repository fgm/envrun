// "unix" is a build tag rather than a GOOS, so the _unix filename suffix constrains nothing.
// Without it this file builds everywhere, and collides with exec_other.go.
//go:build unix

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// run replaces the envrun process with the command, and returns only if it cannot.
//
// There is no envrun left after execve: the command keeps the same PID,
// and its signals, exit status, standard streams and controlling terminal are its own,
// exactly as if it had read the environment file itself.
// That is what closes #35 and #1,
// and why the only statuses reachable here are envrun's own 125/126/127.
// The reasoning, and the options declined, are in ADR-001.
func run(e env, opaque []string, name string, args []string) int {
	// LookPath rather than letting execve resolve the name:
	// only Go's own lookup distinguishes "not found" from "found but not executable",
	// which is what 127 and 126 report.
	path, err := exec.LookPath(name)
	// Inverted deliberately: syscall.Exec returns only on failure,
	// so there is no happy path to end on, and err is non-nil at every line below.
	if err == nil {
		// argv[0] is the name as given, not the resolved path, as exec.Command passes it.
		err = syscall.Exec(path, append([]string{name}, args...), envList(e, opaque))
	}
	fail(fmt.Errorf("running %s: %w", name, err))
	return exitStatus(err)
}
