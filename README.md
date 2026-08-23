# envrun

[![GoDoc](https://pkg.go.dev/badge/github.com/fgm/envrun)](https://pkg.go.dev/github.com/fgm/envrun)
[![CI](https://github.com/fgm/envrun/actions/workflows/tests.yml/badge.svg)](https://github.com/fgm/envrun/actions/workflows/tests.yml)
[![codecov](https://codecov.io/gh/fgm/envrun/branch/main/graph/badge.svg?token=8YYX1B720M)](https://codecov.io/gh/fgm/envrun)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/fgm/envrun/badge)](https://securityscorecards.dev/viewer/?uri=github.com/fgm/envrun)

The `envrun` command allows running any command with default environment
variables taken from a file, copying its standard error and standard output to
its own standard error and standard output.

Variables already present in the environment override the one in the file.

The command may have arguments, and it will be looked up in the `$PATH` if its
name does not contain a `/`.

## Installing

Install from source, using a Go SDK: `go install github.com/fgm/envrun@latest`

Or, better, add as a tool to your project: `go get -tool github.com/fgm/envrun@latest`

Then use as such: `go tool github.com/fgm/envrun <myprogram>`


## Running
### Examples

- `envrun foo`: run `foo` with the environment defaults loaded from `.env` if it exists,
  or fail if it cannot be read.
- `envrun -f other.env env`: run the `env` command with the environment defaults
  loaded from `other.env`, or fail if it cannot be read.

To see that on a real file, `make demo` builds one from the parsing fixtures in
`testdata/` and runs `env` against it,
showing only the variables the file defines.
Those fixtures are what the test suite asserts against,
so the demo cannot drift from the documented behaviour.

### Exit status

`envrun` passes the command's own exit status through unchanged
whenever the command actually ran and exited.

For its own failures it follows the convention used by coreutils
`env`, `timeout` and `nohup`, which keeps them out of the range a command is likely to use:

- `125`: `envrun` itself failed, so the command's own status is unknown —
  the environment file could not be read, no command was given,
  or the command ran but its outcome could not be collected
- `126`: the command exists but could not be executed
- `127`: the command could not be found

Two cases remain ambiguous, and both are unavoidable:

- a command which itself exits `125`, `126` or `127` is indistinguishable from
  the cases above
- a command killed by a signal has no exit status of its own,
  so `envrun` reports `1`

Standard error tells the cases apart:
`envrun` is silent on success,
reports the command's own failure as `<command> exited with status <n>`,
and prefixes its own failures with `failed`.


### The file is not a shell script

`envrun` reads the file, it does not source it:

- **It rejects what it cannot carry**, where a shell would accept it: 
  values spanning several lines, an `export` prefix,
  and any name outside `[_A-Za-z][-._A-Za-z0-9]*`. 
  - One bad line fails the whole file,
    because running a command on configuration the operator meant to set is worse than not running it.
  - This half is loud — `envrun` exits `125` before the command starts,
    naming the line at fault.
    A file it cannot read at all, such as one holding a line over 64 KiB,
    fails the same way but without a position.
- **It does not expand anything**, where a shell would.
  - `$VAR`, `${VAR}` and `$(command)` are passed through as the literal characters they are,
    so a variable like this one reaches the command as that exact string.

```dotenv
DSN=postgres://${INSTANCE}/db
```

  - A shell sourcing the same file would substitute, or execute,
    and hand the command something else entirely.
  - **This half is silent**: both readers succeed, and the values simply differ.
    If a file is read both ways — sourced by a launcher,
    and passed to `envrun` elsewhere, the two diverge invisibly.
    Keep values plain, or read the file one way only.

Rows in the *inherited* environment that are not `name=value` are carried
through rather than dropped or rejected, since a command run without `envrun`
would see them. They are legal at the `execve` level, `envp` being a plain array
the kernel does not police, and two kinds occur:

- a row with no `=` at all, which no name can match, so nothing can read it;
- a row with an empty name, such as `=value`, which `getenv("")` does find.

They arrive after the pairs rather than in their original position, and Go
itself drops empty rows and collapses repeated names before `envrun` sees them,
so the environment is faithful in content but not byte-for-byte identical to
what a direct `exec` would deliver.

## Dependencies

`envrun` has none at build or run time: it uses only the standard library.
The authoritative check is the build information embedded in the binary,
which lists one `dep` line per linked module:

```console
$ go version -m $(which envrun)
/path/to/envrun: go1.27.0
	path	github.com/fgm/envrun
	mod	github.com/fgm/envrun	v0.0.0-...
	build	...
```

No `dep` lines means no dependencies.

The modules in `go.mod` belong to `staticcheck`, required by the `tool`
directive for linting. They are not linked into the command, but GitHub's
dependency graph reads `go.mod` rather than the build graph, so its SBOM
reports them anyway.

## Why ?

Many programs support reading their environment from a `.env` file, and many IDEs
support that feature in run configurations.

This command is provided for situations outside an IDE (e.g. CI/CD) and where the
program to be run does not include this feature.


## Support

- Non-security questions: use [Github issues](https://github.com/fgm/envrun/issues)
- Security questions or direct support: use https://osinet.fr/contact
