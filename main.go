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

// envFromReader parses an environment file.
//
// Every line must be blank, a comment, or name=value with a valid name; anything else fails the file.
// Skipping a malformed line would leave the command running on configuration the operator meant to set,
// which is worse than not running it at all.
//
// It also rejects the multiline quoted values other formats allow:
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
		e[k] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading environment: %w", err)
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

// realMain runs the command and returns the process exit status.
//
// It returns the command's own status where the command ran and exited,
// and ExitEnvrun, ExitNotInvocable or ExitNotFound where envrun itself failed.
// A command killed by a signal has no exit status of its own, so it reports 1,
// as documented in the README.
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

	fileEnv, err := envFromReader(rc)
	if err != nil {
		log.Print(err)
		return ExitEnvrun
	}
	e := fileEnv.Merge(envFromEnv())
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
