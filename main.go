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
	"regexp"
	"strings"
	"syscall"
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

const (
	// CommentRx matches comment lines.
	CommentRx = `^[\s]*#`
	// NameRx is much tighter than Posix, which accepts anything but NUL and '=',
	// but laxer than shells, which do not accept dots. Names are assumed to be pre-trimmed.
	NameRx = `^[[:alpha:]][-._a-zA-Z0-9]*`
)

var (
	commentRx = regexp.MustCompile(CommentRx)
	nameRx    = regexp.MustCompile(NameRx)
)

type env map[string]string

func envFromEnv() env {
	e := make(env)
	for _, row := range os.Environ() {
		// Pairs in the environment are assumed to be valid.
		pair := strings.SplitN(row, "=", 2)
		k, v := pair[0], pair[1]
		e[k] = v
	}
	return e
}

func envFromReader(r io.Reader) env {
	e := make(env)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		row := scanner.Text()
		if commentRx.MatchString(row) {
			continue
		}
		parts := strings.SplitN(row, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k, v := parts[0], parts[1]
		k = strings.Trim(k, " \t")
		v = strings.Trim(v, " \t")
		if !nameRx.MatchString(k) {
			log.Printf(`rejected variable: "%s"`, k)
			continue
		}
		e[k] = v
	}
	return e
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
		return nil, nil, fmt.Errorf("failed parsing flags: %w", err)
	}
	if len(fs.Args()) == 0 {
		return nil, nil, errors.New("no command to run")
	}
	inFile, err := os.Open(*inName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed reading %s: %w", *inName, err)
	}
	return inFile, fs, nil
}

func run(env env, name string, args []string) error {
	fEnv := make([]string, 0, len(env))
	for k, v := range env {
		fEnv = append(fEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmd := exec.Command(name, args...)
	cmd.Env = fEnv
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed acquiring %s standard output: %w", name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed acquiring %s standard error: %w", name, err)
	}

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed starting %s: %w", name, err)
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	return cmd.Wait()
}

func main() {
	os.Exit(realMain(os.Args))
}

// realMain runs the command and returns the process exit status.
//
// It returns the child's own status where the child ran and exited,
// and 1 for any envrun-level failure.
// A child killed by a signal has no exit status of its own,
// so it reports 1, as documented in the README.
func realMain(args []string) int {
	rc, flags, err := openEnv(args)
	if err != nil {
		log.Print(err)
		return ExitEnvrun
	}
	defer func() {
		if err := rc.Close(); err != nil {
			log.Printf("failed closing environment file: %v", err)
		}
	}()

	e := envFromReader(rc).Merge(envFromEnv())
	toRun := flags.Args()
	// Length was checked during openEnv().
	name := toRun[0]

	err = run(e, name, toRun[1:])
	if err == nil {
		return 0
	}

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code := exit.ExitCode()
		if code < 0 {
			// A signalled command has no exit status of its own. See issue #35.
			log.Printf("%s was killed: %v", name, err)
			return 1
		}
		log.Printf("%s exited with status %d", name, code)
		return code
	}

	log.Printf("failed running %s: %v", name, err)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, iofs.ErrPermission), errors.Is(err, syscall.ENOEXEC):
		return ExitNotInvocable
	default:
		return ExitEnvrun
	}
}
