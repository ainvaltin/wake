package wake

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

/*
ErrReceivedQuitSignal is returned by [ListenForQuitSignal] when it receives one of the
quit signals it listened for.
*/
var ErrReceivedQuitSignal = errors.New("received quit signal")

/*
ListenForQuitSignal is meant to be used with [errgroup] - as one group member this func causes the
group context to be cancelled when quit signal is sent.
Benefit using it over [signal.NotifyContext] is that signal.NotifyContext returns [context.Cancelled]
no matter whether the signal was sent or parent ctx was cancelled, ListenForQuitSignal returns
[ErrReceivedQuitSignal] for the former case (use [errors.Is] to check for it as it might be wrapped
inside another error describing the signal).

	g.Go(func() error { return wake.ListenForQuitSignal(ctx) })

When no signals (the sig parameter) is provided (as in above example) it listens for [os.Interrupt]
and [syscall.SIGTERM].

If differentiation between cancellation cases is not a concern then following func is equivalent to
the previous example:

	g.Go(func() error {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		return ctx.Err()
	})

[errgroup]: https://pkg.go.dev/golang.org/x/sync/errgroup
*/
func ListenForQuitSignal(ctx context.Context, sig ...os.Signal) error {
	if len(sig) == 0 {
		sig = append(sig, os.Interrupt, syscall.SIGTERM)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, sig...)
	defer signal.Stop(c)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s := <-c:
		return fmt.Errorf("%s: %w", s, ErrReceivedQuitSignal)
	}
}

/*
ErrWaitDeadlineExceeded is returned by [WaitWithTimeout] when the function it waits for to complete
doesn't finish within given timeout.
*/
var ErrWaitDeadlineExceeded = errors.New("wait func didn't complete within timeout")

/*
WaitWithTimeout is a helper function to support wait with timeout when using "errgroup pattern".

  - "ctx" is context of the waitgroup, it's being cancelled signals to start wait with timeout;
  - "timeout" is the duration for how long to wait for the "wait" func to return before returning [ErrWaitDeadlineExceeded] error;
  - "wait" is the Wait function of the group.

The "wait" function will be called once the "ctx" is cancelled (it's Done chan is closed).
When the "wait" function returns before timeout is reached error returned by it is returned by WaitWithTimeout.
When timeout is reached before the "wait" function finishes WaitWithTimeout returns [ErrWaitDeadlineExceeded].

WaitWithTimeout mustn't be a member of the group, it would be used instead of "plain group.Wait()" call.
Example of using WaitWithTimeout to return from the "main run function" (which presumably will stop the service) even when
all subprocesses haven't gracefully shut down after one second has elapsed since receiving the quit signal:

	func run(ctx context.Context, cfg Configuration) error {
		g, ctx := errgroup.WithContext(ctx)

		g.Go(func() error { return wake.ListenForQuitSignal(ctx) })

		g.Go(func() error {
			s := &service{cfg: cfg}
			return httpsrv.Run(ctx, cfg.HttpServer(s.endpoints()))
		})

		return wake.WaitWithTimeout(ctx, time.Second, g.Wait)
	}

Keep in mind that when timeout is reached the group members that haven't stopped will keep doing whatever they do,
it is just that we do not wait for them to finish anymore!

To use this function with [sync.WaitGroup] just wrap the g.Wait() call, ie

	wake.WaitWithTimeout(ctx, time.Second, func() error { g.Wait(); return nil })
*/
func WaitWithTimeout(ctx context.Context, timeout time.Duration, wait func() error) error {
	<-ctx.Done()

	rec := make(chan error, 1)

	go func() { rec <- wait() }()

	select {
	case err := <-rec:
		return err
	case <-time.After(timeout):
		return ErrWaitDeadlineExceeded
	}
}
