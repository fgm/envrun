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

- If the command exits, `envrun` will return its exit status
- If the command is killed, `envrun` will return exit status 1


## Why ?

Many programs support reading their environment from a `.env` file, and many IDEs
support that feature in run configurations.

This command is provided for situations outside an IDE (e.g. CI/CD) and where the
program to be run does not include this feature.


## Support

- Non-security questions: use [Github issues](https://github.com/fgm/envrun/issues)
- Security questions or direct support: use https://osinet.fr/contact
