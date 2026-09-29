# Dev container for envrun

This folder adds an optional [Dev Container](https://containers.dev) so envrun
can be developed inside a reproducible, editor-agnostic environment. It is
purely additive: the repo remains a plain Go project, and `go`/`make`/your
native IDE keep working unchanged whether or not you use this.

## What it sets up

- A **single container** (no Docker Compose, no extra services): envrun is a
  CLI with no runtime dependencies, so the environment is one container.
- A **prebuilt Go image** (`mcr.microsoft.com/devcontainers/go`) with the Go
  toolchain baked in via the registry pull. We deliberately use this rather than
  the base image plus the Go *Feature*: a Feature fetches the toolchain from
  `go.googlesource.com` at build time, which can fail behind a TLS-inspecting
  corporate proxy when the container's trust store lacks the proxy CA. A
  prebuilt image needs no such fetch, so it builds reliably in both plain and
  proxied environments.
- A **lifecycle hook** (`postCreateCommand`) that runs `go mod download` and
  then `make demo` - which dogfoods envrun by running it on this repo's own
  `.env.demo`. Creating the container demonstrates the tool working.

`make lint` (`go tool staticcheck ./...`), `make test`, and `make build` all
work in-container with no extra configuration.

## Opening it (three interchangeable ways)

Any tool that implements the Dev Container spec can drive this same file:

- **VS Code** (Dev Containers extension): open the repo, then "Reopen in
  Container".
- **JetBrains / GoLand** (Gateway or the Dev Containers action): open the
  `devcontainer.json`; the IDE backend runs *inside* the container and a thin
  client attaches. The container can be local or remote - "remote" just means a
  separate environment, not necessarily a separate machine.
- **CLI**: `devcontainer up --workspace-folder .` (from `@devcontainers/cli`),
  then `devcontainer exec --workspace-folder . make test`.

## Try the dogfood by hand

Inside the container (or natively):

```sh
make demo          # envrun injects .env.demo into `env`, sorted
make lint          # go tool staticcheck ./...
make test          # go test -race ./...
```
