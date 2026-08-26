// Package env reads an environment file and hands back what it declared.
//
// It exists because envrun has two callers wanting opposite things.
// The command must not put the variables into its own process:
// what it needs is the set to hand to the command it runs,
// which under ADR-001 becomes that command's environment across execve.
// A Go program importing this package wants the opposite —
// the variables in its own process, with no wrapper process at all.
//
// So there are two entry points over one core:
// [Load] resolves and reads, applying nothing, and [Apply] applies what it read.
//
// This package writes no output, and takes no logger:
// everything it observed is returned, so presentation belongs to the caller,
// which is the only party that knows the format it needs.
// [Result.Path] names the file it used, [Result.Env] holds what that file declared,
// and a rejected file yields a [ParseError] whose problems are reachable one by one.
// A library writing text under an application's slog.JSONHandler
// would interleave non-JSON lines into a JSON stream.
//
// It is deliberately not a general .env package.
// The file format is narrow — see [ParseError] for what it refuses and why —
// and a feature is not adopted here on the grounds that other .env libraries carry it.
// See docs/adr/002-splitting-the-command-from-the-library.md.
package env
