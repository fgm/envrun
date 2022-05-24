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
	// CommentRx matches comment lines
	CommentRx = `^[\s]*#`
	// NameRx is much tighter than Posix, which accepts anything but NUL and '=',
	// but laxer than shells, which do not accept dots. Names are assumed to be pre-trimmed.
	NameRx          = `^[[:alpha:]][-._a-zA-Z0-9]*`
	EnvKeyReplaceRx = `\$\{[^}]+\}`
	VarAndDefaultRx = `([^|]+)(?:\|(.+))?`
)

var (
	commentRx       = regexp.MustCompile(CommentRx)
	nameRx          = regexp.MustCompile(NameRx)
	envKeyReplaceRx = regexp.MustCompile(EnvKeyReplaceRx)
	varAndDefaultRx = regexp.MustCompile(VarAndDefaultRx)
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

// replaces ${ENV_KEY} keys in env with source env values
func (e env) ReplaceEnvKeys(source env) env {
	res := make(env, len(e))
	for k, v := range e {
		res[k] = envKeyReplaceRx.ReplaceAllStringFunc(v, func(part string) string {
			plen := len(part)
			if plen > 3 {
				envKeyAndDefault := part[2 : plen-1]
				mr := varAndDefaultRx.FindStringSubmatch(envKeyAndDefault)
				envKey := mr[1]
				if envPart, ok := source[envKey]; ok {
					return envPart
				}
				// default value?
				if len(mr) > 2 && mr[2] != "" {
					return mr[2]
				} else {
					log.Printf(`Replace variable failed, env key is unknown and no default value was defined: "%s"`, part)
					return part
				}
			} else {
				log.Printf(`Replacing the variable failed, env key is empty: "%s"`, part)
				return part
			}
		})
	}
	return res
}

type options = map[string]any

func parseArgs(args []string) (options, *flag.FlagSet, error) {
	opts := make(map[string]any)
	if len(args) < 2 {
		return nil, nil, errors.New("need at least a command to run")
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	inFile := fs.String("f", ".env", "The file from which to read the environment variables")
	overrideEnv := fs.Bool("o", false, "Do override existing environment variables with own values.\n"+
		"But you can still use the original environment variable names as variables inside the .env file.")
	if err := fs.Parse(args[1:]); err != nil {
		return opts, nil, fmt.Errorf("failed parsing flags: %w", err)
	}
	opts["<override_envs>"] = *overrideEnv
	opts["<env_file_name>"] = *inFile

	if len(fs.Args()) == 0 {
		return opts, nil, errors.New("no command to run")
	}

	return opts, fs, nil
}

func readCloser(opts options) (io.ReadCloser, error) {
	inFile, err := os.Open(opts["<env_file_name>"].(string))
	if err != nil {
		return nil, fmt.Errorf("failed reading %s: %v", opts["<env_file_name>"], err)
	}
	return inFile, nil
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

	opts, fs, err := parseArgs(os.Args)
	if err != nil {
		log.Fatal(err)
	}
	rc, err := readCloser(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		rc.Close()
		if err != nil {
			os.Exit(exitCode)
		}
	}()

	env := envFromReader(rc)
	osEnv := envFromEnv()
	if opts["<override_envs>"].(bool) {
		env = env.ReplaceEnvKeys(osEnv)
		env = osEnv.Merge(env)
	} else {
		env = env.Merge(osEnv)
		env = env.ReplaceEnvKeys(env)
	}
	toRun := fs.Args()
	// Length checked during readCloser().
	name := toRun[0]
	if err := run(env, name, toRun[1:]); err == nil {
		return
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {

		log.Printf("non-exit error running %s: %v", name, err)
		exitCode = 1
	}
	log.Printf("exit error running %s: %v", name, err)
	exitCode = exit.ExitCode()
}
