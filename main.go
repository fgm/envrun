package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/fgm/envrun/env"
)

// parseArgs parses the command line, returning the environment file to read and
// the command to run.
//
// It resolves nothing and opens nothing: finding the file is env.Load's job,
// and -f names the one candidate it is to consider.
//
// stdW is used by -h, errW by the error paths.
func parseArgs(args []string, stdW, errW io.Writer) (string, []string, error) {
	if len(args) < 2 {
		return "", nil, errors.New("need at least a command to run")
	}

	// The set is named for the command rather than for args[0],
	// so usage carries a stable name instead of the path the binary was
	// reached through, which under `go run` is a build-cache directory.
	fs := flag.NewFlagSet(env.AppName, flag.ContinueOnError)
	// Discarded so that flag's own reporting does not land beside envrun's.
	// It reports a rejected flag and returns the error, where envrun needs the
	// line attributed; the message survives in the error, and is written below.
	fs.SetOutput(io.Discard)
	inName := fs.String("f", env.DefaultPath, "The file from which to read the environment variables")

	// Declared rather than left to flag.
	// Declaring keeps flag from writing usage of its own, which it would send to one destination,
	// for both a help request and a parse failure — and those need different ones.
	showHelp := fs.Bool("h", false, "Print this message on standard output and exit")
	fs.BoolVar(showHelp, "help", false, "Alias for -h")

	// Prepend to flag's own rendering rather than replacing it:
	// PrintDefaults cannot know about the command and arguments.
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"Usage: envrun [-f file] command [argument ...]\n"+
				"       envrun -h\n\nThe flags are:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		// Reported here rather than by realMain, because usage follows the
		// message and only this function holds a set that can render it.
		fail(fmt.Errorf("parsing flags: %w", err))
		fs.SetOutput(errW)
		fs.Usage()
		return "", nil, errReported
	}

	// An exclusive flag says what envrun does instead of running a command,
	// so it resolves before the operand check below,
	// and before realMain reads the environment: neither applies to it.
	//
	// -h dominates: where it appears, every other flag and the operand are
	// ignored, which is what sort, curl and python3 do and what a caller
	// asking what this program is will least be surprised by.
	if *showHelp {
		fs.SetOutput(stdW)
		fs.Usage()
		return "", nil, flag.ErrHelp
	}

	if len(fs.Args()) == 0 {
		return "", nil, errors.New("no command to run")
	}
	return *inName, fs.Args(), nil
}

// errReported marks a failure whose message has already reached the caller,
// so realMain returns its status without saying it a second time.
//
// Named for the property rather than the case: flag reports a rejected command
// line itself, and anything else that reports before returning can reuse this.
var errReported = errors.New("already reported")

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

// A line starting with failPrefix means the status is envrun's; notePrefix means it is not.
// docs/exit-status.md builds attribution on the difference.
const (
	failPrefix = env.AppName + " failed: "
	notePrefix = env.AppName + ": "
)

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
	log.Printf(failPrefix+"%v", err)
}

// note reports something envrun saw without failing at it.
//
// Deliberately not a fail line:
// envrun did not fail,
// and saying so would break the attribution above precisely where a caller needs it.
// It still names envrun,
// since nothing else on standard error should be taken for the command's own output.
func note(format string, args ...any) {
	log.Printf(notePrefix+format, args...)
}

// reportNotes prints the non-fatal findings a [env.Result] carries.
//
// Named rather than inlined in realMain so that a test can hand it a note:
// the only one envrun raises needs a Close to fail, which no portable test can
// provoke, so an end-to-end case cannot reach this loop.
func reportNotes(notes []env.Note) {
	for _, n := range notes {
		note("%s", n)
	}
}

// realMain runs the command and returns the process exit status.
//
// On *nix it returns only where envrun itself failed,
// since a command that could be started replaces envrun and reports for itself;
// on Windows envrun stays the parent and passes the command's status through.
// See ADR-001.
//
// The presenting is all here, because the library writes nothing:
// env.Load hands back what it saw, and this is the only party that knows the
// output contract the README documents. See ADR-002.
func realMain(args []string) int {
	path, toRun, err := parseArgs(args, os.Stdout, os.Stderr)
	switch {
	// Help is not a failure: it wrote what was asked for and nothing ran.
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errReported):
		return ExitEnvrun
	case err != nil:
		fail(err)
		return ExitEnvrun
	}

	res, err := env.Load(path)
	// Reported before the error is acted on: a Result can carry findings even
	// where none of them is what failed.
	reportNotes(res.Notes)
	if err != nil {
		fail(err)
		return ExitEnvrun
	}

	// The inherited environment wins over the file, so it is the argument.
	inherited, opaque := env.FromEnviron(os.Environ())
	return run(res.Env.Merge(inherited), opaque, toRun[0], toRun[1:])
}
