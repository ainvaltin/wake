# wake

Utils to support errgroup pattern.

Extracted from the [httpsrv package](https://github.com/ainvaltin/httpsrv) as
these functions are not `httpsrv` specific but general "errgroup pattern" helpers.


## errgroup pattern

Simply put "errgroup pattern" is a organization of the service code so that all
subprocesses of the service are launched as members of the same
[errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup) and their lifetime is
controlled by the context of the group. This means that when one of the group
members exits with error the group's context gets cancelled and all other group
members get signal to (gracefully) exit too. Group reports the first error as
the reason service stopped.

Rules for a func starting a subprocess are:
 - every group member lifetime is controlled by group's context - when it gets
 cancelled the subprocess should exit (gracefully) ASAP;
 - subprocess always returns non-nil error (most of the time it would be
 `ctx.Err()` ie the subprocess exits because the context controlling it's lifetime
 has been cancelled).

See the [httpsrv example project](https://github.com/ainvaltin/httpsrv/examples/errgroup/)
for more.