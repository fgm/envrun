// Neither this file's name nor exec_unix.go's constrains anything:
// "_other" and "_unix" are not GOOS values, unlike "_windows".
// Both therefore say so in a //go:build line,
// and between them they cover every platform.

//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/fgm/envrun/env"
)

// run starts the command as a child process and reports the status it exits with.
//
// This is the path for platforms with no fork/exec pair to hand control over with,
// which in practice means Windows:
// CreateProcess always makes a new process with a new PID,
// so the shape of exec_unix.go cannot be expressed there and never will be.
// Supervision is the only available shape rather than a fallback,
// which is why the transparency v0.2.0 promises is *nix-only.
// What supervision could still gain on Windows —
// an allowlist of console control events, and job objects with kill-on-close —
// is issue #44.
//
// The remaining platforms it covers, js/wasm and wasip1, cannot start a process at all.
// They reach the same code and fail at cmd.Run(), reporting 125 with the reason,
// which is what a wrapper that cannot wrap should do.
func run(e env.Vars, opaque []string, name string, args []string) int {
	cmd := exec.Command(name, args...)
	cmd.Env = e.Environ(opaque)
	// Assigned rather than piped through copying goroutines:
	// the command then has envrun's own handles, which is what closes #1 here,
	// and Wait can no longer close a pipe ahead of the copy draining it.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	err := cmd.Run()
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code := exit.ExitCode()
		if code < 0 {
			// A command that died of a signal has no exit status of its own.
			note("%s was killed: %v", name, err)
			return 1
		}
		note("%s exited with status %d", name, code)
		return code
	}
	fail(fmt.Errorf("running %s: %w", name, err))
	return exitStatus(err)
}
