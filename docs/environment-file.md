# The environment file

Every line must be blank, a comment starting with `#`, or `name=value`
with a name matching `[_A-Za-z][-._A-Za-z0-9]*`.
Values are taken literally, with surrounding spaces and tabs trimmed.

Variables already present in the inherited environment
override the ones the file defines.

## It is not a shell script

`envrun` reads the file, it does not source it:

- **It rejects what it cannot carry**, where a shell would accept it:
  a line that is not `name=value` at all,
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

A line without `=` is refused here even though the *inherited* environment may hold one,
and it is carried through — see below. 
The rule for the file is simply that every line must declare a variable;
a line that declares none is refused rather than passed on,
because passing it on would leave the variable it was meant to set absent,
with nothing said about it.

If you have a use for carrying such a line from the file through to the command,
[open an issue](https://github.com/fgm/envrun/issues): 
this is a deliberate choice and not an oversight, so it needs a case rather than a patch.

## Rows envrun cannot represent

Rows in the *inherited* environment that are not `name=value` are carried
through rather than dropped or rejected, since a command run without `envrun`
would see them. They are legal at the `execve` level, `envp` being a plain array
the kernel does not police, and two kinds occur:

- a row with no `=` at all, which no name can match, so nothing can read it;
- a row with an empty name, such as `=value`, which `getenv("")` does find.

They arrive after the pairs rather than in their original position,
and Go itself drops empty rows and collapses repeated names before `envrun` sees them,
so the environment is faithful in content
but not byte-for-byte identical to what a direct `exec` would deliver.

The same shape of row in the *file* fails it instead,
which is not an inconsistency: what differs is the baseline.
An inherited row would have reached the command with or without `envrun`,
so carrying it is faithful.
A file line would have reached nothing at all, since nothing else reads the file — 
so there is no behaviour to preserve, only an intent that did not take effect,
and saying so is more useful than passing on a row no name can match.
