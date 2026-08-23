package main

import (
	"log"
	"os"
)

// Exit statuses for envrun's own failures.
//
// They follow the convention used by similar coreutils commands: env, timeout and nohup.
// The command's own status is passed through unchanged,
// so a caller can only confuse the two when the command itself exits 125 to 127.
const (
	// ExitEnvrun reports that envrun failed before the command could run.
	ExitEnvrun = 125
	// ExitNotInvocable reports that the command exists but could not be executed.
	ExitNotInvocable = 126
	// ExitNotFound reports that the command could not be found.
	ExitNotFound = 127
)

func main() {
	// No timestamp or file prefix:
	// these lines sit on the same standard error as the command's own diagnostics,
	// which carry none,
	// and a caller reading them is being told which process failed, not when.
	log.SetFlags(0)
	os.Exit(realMain(os.Args))
}
