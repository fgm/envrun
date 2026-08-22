# envrun

[![GoDoc](https://pkg.go.dev/badge/github.com/fgm/envrun)](https://pkg.go.dev/github.com/fgm/envrun)
[![Go Report Card](https://goreportcard.com/badge/github.com/fgm/envrun)](https://goreportcard.com/report/github.com/fgm/envrun)
[![github](https://github.com/fgm/container/actions/workflows/workflow.yml/badge.svg)](https://github.com/fgm/container/actions/workflows/workflow.yml)
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
- `envrun -f .env.demo env`: run the `env` command with the environment defaults
  loaded from `.env.demo` or fail if it cannot be read

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


## Why ?

Many programs support reading their environment from a `.env` file, and many IDEs
support that feature in run configurations.

This command is provided for situations outside an IDE (e.g. CI/CD) and where the
program to be run does not include this feature.


## Support

- Non-security questions: use [Github issues](https://github.com/fgm/envrun/issues)
- Security questions or direct support: use https://osinet.fr/contact
