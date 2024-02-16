package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
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

// Merge combines two env maps. If keys overlap, the newer one in the argument
// map overwrites the value found in the receiver map, as in PHP array_merge.
func (e env) Merge(f env) env {
	res := make(env, len(e)+len(f))
	for k, v := range e {
		res[k] = v
	}
	for k, v := range f {
		res[k] = v
	}
	return res
}

func readCloser(args []string) (io.ReadCloser, *flag.FlagSet, error) {
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
		return nil, fs, fmt.Errorf("failed reading %s: %v", *inName, err)
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
		return fmt.Errorf("failed acquiring %s standard output: %v", name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed acquiring %s standard error: %v", name, err)
	}

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed starting %s: %v", name, err)
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	return cmd.Wait()
}

func main() {
	var (
		err      error
		exitCode int
	)
	rc, fs, err := readCloser(os.Args)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		errClose := rc.Close()
		if err != nil || errClose != nil {
			os.Exit(exitCode)
		}
	}()

	env := envFromReader(rc)
	env = env.Merge(envFromEnv())
	toRun := fs.Args()
	// Length was checked during readCloser().
	name := toRun[0]
	if err := run(env, name, toRun[1:]); err == nil {
		return
	}
	var exit *exec.ExitError
	ok := errors.As(err, &exit)
	if !ok {
		log.Printf("non-exit error running %s: %v", name, err)
		exitCode = 1
	}
	log.Printf("exit error running %s: %v", name, err)
	exitCode = exit.ExitCode()
}
