# Installing envrun

The route decides whether the transparency `envrun` offers is felt at all:
`go run` and `go tool` put a `go` process between the caller and the command,
where a built binary does not.
See [Exit status and platform scope](exit-status.md) for what that costs.

Two routes avoid that process entirely, and they serve different readers:
**download a released binary** if you have no Go toolchain,
**build one from a pin** if you have one and want the version tied to a project.

## Downloading a released binary

Take the archive for your platform from the
[latest release](https://github.com/fgm/envrun/releases/latest),
unpack it, and put `envrun` somewhere on your `$PATH`.

Six platforms are published: Linux, macOS and Windows, each on `amd64` and `arm64`.
The WebAssembly ports compile but cannot start a process, so no artifact ships for them.

Each release carries, beside the archives:

- `checksums.txt`, a SHA-256 over every archive;
- one SPDX SBOM per archive, from [syft](https://github.com/anchore/syft);
- `envrun_<version>.sigstore.json`, the signed attestation of where the
  archives were built.

Nothing here is tied to a project, so a `go.mod` bump will not update it.
That is the trade against the next route.

## Verifying what you downloaded

The archives are signed through [Sigstore](https://www.sigstore.dev)
against GitHub's OIDC identity,
so there is no public key to fetch and no private key held anywhere.
The attestation names the repository, the workflow and the commit that produced the artifact.

With the [GitHub CLI](https://cli.github.com):

```console
$ gh attestation verify envrun_0.3.0_linux_amd64.tar.gz --owner fgm
```

It fails if the archive was modified, or was not built by this repository's
release workflow.

Without it, `checksums.txt` still catches a corrupted download
(`shasum -a 256 -c` on macOS):

```console
$ sha256sum --check --ignore-missing checksums.txt
```

A checksum proves only that the file matches the list, and says nothing about
who wrote the list. The attestation is what answers that.

## Building from a pin

Pin the version in your project, then build the binary from that pin:

```console
$ go get -tool github.com/fgm/envrun@latest
$ go build -o bin/ github.com/fgm/envrun
```

Then run `bin/envrun <myprogram>`.
As a make target the pin can never drift:

```make
bin/envrun: go.mod
	go build -o bin/ github.com/fgm/envrun
```

Bumping the pin rebuilds the binary on the next invocation.
Ignore **the binary**, `/bin/envrun`, rather than `bin/`,
which is often a tracked source directory.
`go build -o <dir>/` creates the directory, so nothing else is needed.

## Why not `go tool` or `go install`

Both work, and both cost something the recipe above does not:

- **`go tool github.com/fgm/envrun`** re-resolves `go.mod` on every run,
  so the version can never drift -
  but the `go` process stays between the caller and the command.
  It forwards four signals (`HUP`, `INT`, `QUIT`, `TERM`),
  and reports a command killed by any signal as exit 1.
  Ordinary non-zero statuses pass through untouched,
  so this is enough where only exit-status fidelity matters.
- **`go install github.com/fgm/envrun@latest`** gives a wrapper-free binary,
  but it pins at install time and nothing re-checks it afterwards,
  and `$GOBIN` is shared:
  one project's install changes which `envrun` every other project gets.
  Running it from inside a consuming module without `@latest`
  installs that module's pinned version, which fixes the pin but not the sharing.
- **`go run`** interposes and does not forward `SIGTERM` at all.

A download shares `go install`'s weakness - nothing re-checks it - and avoids
its sharing problem, since you decide where the binary lands.

## Dependencies

`envrun` has none at build or run time: it uses only the standard library.
The authoritative check is the build information embedded in the binary,
which lists one `dep` line per linked module:

```console
$ go version -m bin/envrun
bin/envrun: go1.27.0
	path	github.com/fgm/envrun
	mod	github.com/fgm/envrun	v0.2.0-...
	build	...
```

No `dep` lines means no dependencies.

The same command identifies a downloaded binary: since Go 1.24 the `mod` line
carries the tag it was built from, so a renamed or relocated artifact still
says which release it is.

Without a toolchain, the SBOM published beside each archive answers both questions instead.
It is generated from the binary's own build graph rather than from `go.mod`,
so its only entries are `envrun` itself, the Go standard library and the archive:
no third-party package appears.

The modules in `go.mod` belong to `staticcheck`, required by the `tool`
directive for linting. They are not linked into the command, but GitHub's
dependency graph reads `go.mod` rather than the build graph, so its SBOM
reports them anyway.
