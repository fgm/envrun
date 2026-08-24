package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"log"
	"maps"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// envFromEnv parses environment rows as os.Environ returns them,
// returning the pairs it can represent and, separately, the rows it cannot.
//
// envp is a plain array at the execve level:
// the kernel enforces neither shape nor uniqueness,
// so any parent can hand us a row that is not name=value,
// and indexing a split on the assumption that it is one used to panic.
// Two kinds arrive, and they are not the same thing despite being handled alike:
//
//   - a row without "=", which no name can match,
//     since getenv compares a name against the text before the "=";
//   - a row with an empty name, such as "=value", which getenv("") does find.
//
// Both are passed through rather than dropped,
// because a command run without envrun would see them.
// Only the first is genuinely unreadable.
//
// Dedup keeps the first occurrence, as getenv does when scanning envp.
// It never fires on os.Environ output, which syscall.copyenv has already deduped,
// but the rows are the caller's to supply,
// and the map must not impose the opposite rule on them.
func envFromEnv(rows []string) (env, []string) {
	e := make(env)
	var opaque []string
	for _, row := range rows {
		k, v, found := strings.Cut(row, "=")
		if !found || k == "" {
			opaque = append(opaque, row)
			continue
		}
		if _, seen := e[k]; seen {
			continue
		}
		e[k] = v
	}
	return e, opaque
}

// envFromReader parses an environment file.
//
// Every line must be blank, a comment, or name=value with a valid name;
// anything else fails the file.
// Passing it on would leave the command running without the configuration
// the operator meant to set, which is worse than not running it at all.
//
// This is the opposite of what envFromEnv does with the same shape of row,
// and the difference is the baseline rather than the format:
//
//   - A row inherited from the parent would have reached the command anyway,
//     so carrying it through is the faithful act.
//   - A line in the file would have reached nothing without envrun,
//     since nothing else reads the file,
//     so there is no behaviour to preserve — only an intent that did not take effect.
//
// It also rejects values containing NUL, which cannot be passed to a command at all,
// and the multiline quoted values other formats allow:
// envrun does not interpret quotes, so a value spanning lines cannot be represented,
// and keeping only its first line would be corruption rather than an omission.
//
// Problems are reported by line number rather than content,
// so a malformed line holding a secret does not reach the logs.
//
// A repeated name keeps its last value rather than failing,
// matching both the usual expectation for this format and Merge, where the later map wins.
// It is the one silent overwriting left here, and worth a warning once a verbose mode can carry one:
// see issue #3.
func envFromReader(r io.Reader) (env, error) {
	e := make(env)
	var problems []string
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		row := scanner.Text()
		if strings.TrimSpace(row) == "" || commentRx.MatchString(row) {
			continue
		}
		k, v, found := strings.Cut(row, "=")
		if !found {
			problems = append(problems, fmt.Sprintf("line %d: not a name=value pair", line))
			continue
		}
		k = strings.Trim(k, " \t")
		v = strings.Trim(v, " \t")
		if !nameRx.MatchString(k) {
			problems = append(problems, fmt.Sprintf("line %d: invalid name %q", line, k))
			continue
		}
		// A NUL cannot reach a command: exec rejects it before execve,
		// and execve takes NUL-terminated strings in any case.
		// Letting it through already failed, and already exited 125.
		// What catching it here adds is the line number,
		// and an error attributed to the file rather than to the command.
		if strings.ContainsRune(v, 0) {
			problems = append(problems, fmt.Sprintf("line %d: value contains NUL", line))
			continue
		}
		e[k] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading the environment file: %w", err)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid environment file: %s", strings.Join(problems, "; "))
	}
	return e, nil
}

// Merge combines two env maps.
//
// If keys overlap, the newer one in the argument map overwrites the value found in the receiver map,
// as in PHP array_merge.
func (e env) Merge(f env) env {
	res := make(env, len(e)+len(f))
	maps.Copy(res, e)
	maps.Copy(res, f)
	return res
}

// openEnv parses the command-line flags and opens the environment file.
func openEnv(args []string) (io.ReadCloser, *flag.FlagSet, error) {
	if len(args) < 2 {
		return nil, nil, errors.New("need at least a command to run")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	inName := fs.String("f", ".env", "The file from which to read the environment variables")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, nil, fmt.Errorf("parsing flags: %w", err)
	}
	if len(fs.Args()) == 0 {
		return nil, nil, errors.New("no command to run")
	}
	inFile, err := os.Open(*inName)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", *inName, err)
	}
	return inFile, fs, nil
}

// envList renders the environment as execve takes it: a plain array of rows.
//
// The rows envrun could not represent follow the pairs
// rather than keeping their original position,
// since a map has no order to preserve them in. See envFromEnv.
func envList(e env, opaque []string) []string {
	rows := make([]string, 0, len(e)+len(opaque))
	for k, v := range e {
		rows = append(rows, fmt.Sprintf("%s=%s", k, v))
	}
	return append(rows, opaque...)
}

// exitStatus classifies a failure of envrun's own into the status the README documents.
//
// It never sees a command's own status:
// on *nix the command replaces envrun and reports for itself,
// and everywhere else exec_other.go reads the status off Wait before reaching here.
//
// EISDIR needs its own case because errors.Is(EISDIR, fs.ErrPermission) is false,
// and so does ENOEXEC;
// without them a directory or a shebang-less script would report 125
// where both the pre-exec envrun and bash report 126.
func exitStatus(err error) int {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, iofs.ErrPermission),
		errors.Is(err, syscall.EISDIR),
		errors.Is(err, syscall.ENOEXEC):
		return ExitNotInvocable
	default:
		return ExitEnvrun
	}
}

// fail reports a failure of envrun's own, naming envrun as the process that failed.
//
// Every such line begins with the same three words,
// and only envrun's own failures use them,
// which is what lets a caller attribute a 125, 126 or 127:
// the status is envrun's exactly when this line is present.
// A command exiting 125 itself is then still distinguishable,
// which the status alone can never be.
// Errors reaching here carry context only,
// so the words are written in one place rather than at each call site,
// where a message would eventually be phrased differently.
func fail(err error) {
	log.Printf("envrun failed: %v", err)
}

// note reports something envrun saw without failing at it.
//
// Deliberately not a fail line:
// envrun did not fail,
// and saying so would break the attribution above precisely where a caller needs it.
// It still names envrun,
// since nothing else on standard error should be taken for the command's own output.
func note(format string, args ...any) {
	log.Printf("envrun: "+format, args...)
}

// realMain runs the command and returns the process exit status.
//
// On *nix it returns only where envrun itself failed,
// since a command that could be started replaces envrun and reports for itself;
// on Windows envrun stays the parent and passes the command's status through.
// See ADR-001.
func realMain(args []string) int {
	rc, flags, err := openEnv(args)
	if err != nil {
		fail(err)
		return ExitEnvrun
	}

	fileEnv, rErr := envFromReader(rc)
	// Closed here rather than deferred:
	// syscall.Exec runs no deferred function,
	// and on *nix nothing below this point returns.
	if err := rc.Close(); err != nil {
		note("could not close the environment file: %v", err)
	}
	if rErr != nil {
		fail(rErr)
		return ExitEnvrun
	}

	inherited, opaque := envFromEnv(os.Environ())
	e := fileEnv.Merge(inherited)
	toRun := flags.Args()
	// Length was checked during openEnv().
	return run(e, opaque, toRun[0], toRun[1:])
}
