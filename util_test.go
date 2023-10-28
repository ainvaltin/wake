package wake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func Test_WaitWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("wait returns nil before timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		res := make(chan error)
		go func() {
			res <- WaitWithTimeout(ctx, time.Second, func() error { return nil })
		}()
		// cancel the "group ctx", this triggers the wait call with timeout
		cancel()
		select {
		case err := <-res:
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("WaitWithTimeout didn't return within timeout")
		}
	})

	expectError := func(t *testing.T, err, expErr error) {
		t.Helper()
		if err != nil {
			if !errors.Is(err, expErr) {
				t.Errorf("expected error\n%v\nbut got\n%v", expErr, err)
			}
		} else {
			t.Errorf("got nil error while expected to get error: %v", expErr)
		}
	}

	t.Run("wait returns non-nil error before timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		expErr := fmt.Errorf("error from wait")
		res := make(chan error)
		go func() {
			res <- WaitWithTimeout(ctx, time.Second, func() error { return expErr })
		}()
		// cancel the "group ctx", this triggers the wait call with timeout
		cancel()
		select {
		case err := <-res:
			expectError(t, err, expErr)
		case <-time.After(500 * time.Millisecond):
			t.Error("WaitWithTimeout didn't return within timeout")
		}
	})

	t.Run("wait blocks longer than timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		res := make(chan error)
		go func() {
			res <- WaitWithTimeout(ctx, time.Second,
				func() error {
					time.Sleep(1500 * time.Millisecond)
					return nil
				})
		}()
		// cancel the "group ctx", this triggers the wait call with timeout
		cancel()
		select {
		case err := <-res:
			expectError(t, err, ErrWaitDeadlineExceeded)
		case <-time.After(1100 * time.Millisecond):
			t.Error("WaitWithTimeout didn't return within timeout")
		}
	})
}

func Test_ListenForQuitSignal(t *testing.T) {
	t.Parallel()

	t.Run("parent context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(700 * time.Millisecond)
			cancel()
		}()

		done := make(chan error, 1)
		go func() {
			done <- ListenForQuitSignal(ctx)
		}()

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected error %q, got %q", context.Canceled, err.Error())
			}
		case <-time.After(time.Second):
			t.Fatal("test didn't complete within timeout")
		}
	})

	t.Run("os.Interrupt", func(t *testing.T) {
		s, err := runTestCommand("TestSignalInterrupt")
		if err != nil {
			t.Fatalf("failed to run test: %v", err)
		}
		if s != `interrupt: received quit signal` {
			t.Errorf("unexpected return value:\n%s\n", s)
		}
	})

	t.Run("syscall.SIGTERM", func(t *testing.T) {
		s, err := runTestCommand("TestSignalSIGTERM")
		if err != nil {
			t.Fatalf("failed to run test: %v", err)
		}
		if s != `terminated: received quit signal` {
			t.Errorf("unexpected return value:\n%s\n", s)
		}
	})

	t.Run("syscall.SIGQUIT", func(t *testing.T) {
		s, err := runTestCommand("TestSignalSIGQUIT")
		if err != nil {
			t.Fatalf("failed to run test: %v", err)
		}
		if s != `quit: received quit signal` {
			t.Errorf("unexpected return value:\n%s\n", s)
		}
	})

	t.Run("syscall.SIGQUIT unexpected", func(t *testing.T) {
		s, err := runTestCommand("TestSignalSIGQUITunexpected")
		if err != nil {
			if err.Error() != `failed to run the command: exit status 2` {
				t.Errorf("unexpected error returned by Run:\n%v\n", err)
			}
		}
		if s != `` {
			t.Errorf("unexpected return value:\n%s\n", s)
		}
	})

	t.Run("syscall.SIGKILL", func(t *testing.T) {
		// SIGKILL can't be caught/recovered from - run will exit with error
		s, err := runTestCommand("TestSignalSIGKILL")
		if err != nil {
			if err.Error() != `failed to run the command: signal: killed` {
				t.Errorf("unexpected error returned by Run:\n%v\n", err)
			}
		}
		if s != `` {
			t.Errorf("unexpected return value:\n%s\n", s)
		}
	})
}

func runTestCommand(testName string) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = []string{"GO_TEST_PROCESS=1"}
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to run the command: %w", err)
	}

	return out.String(), nil
}

func sendSignalToItself(sig os.Signal) error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return fmt.Errorf("failed to find the process: %w", err)
	}

	if err := p.Signal(sig); err != nil {
		return fmt.Errorf("failed to send signal (%d) to the process: %w", sig, err)
	}

	return nil
}

func testListenForQuitSignal(sig os.Signal, listenFor ...os.Signal) {
	done := make(chan error, 1)
	go func() {
		done <- ListenForQuitSignal(context.Background(), listenFor...)
	}()
	// delay to allow the goroutine to register the signal handler
	time.Sleep(500 * time.Millisecond)

	if err := sendSignalToItself(sig); err != nil {
		fmt.Fprint(os.Stdout, err.Error())
		os.Exit(1)
	}

	select {
	case err := <-done:
		if err == nil {
			fmt.Print("unexpectedly got nil error")
		} else if !errors.Is(err, ErrReceivedQuitSignal) {
			fmt.Printf("unexpected error: %v", err)
		} else {
			fmt.Fprint(os.Stdout, err.Error())
		}
	case <-time.After(2 * time.Second):
		fmt.Print("test didn't complete within timeout")
	}
	os.Exit(0)
}

func TestSignalInterrupt(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}

	testListenForQuitSignal(os.Interrupt)
}

func TestSignalSIGTERM(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}

	testListenForQuitSignal(syscall.SIGTERM)
}

func TestSignalSIGQUIT(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	// by default we do not listen for SIGQUIT so send it as signal we want to catch
	testListenForQuitSignal(syscall.SIGQUIT, syscall.SIGQUIT)
}

func TestSignalSIGQUITunexpected(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	// by default we do not listen for SIGQUIT
	testListenForQuitSignal(syscall.SIGQUIT)
}

func TestSignalSIGKILL(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	// by default we do not listen for SIGKILL, ask for it
	testListenForQuitSignal(syscall.SIGKILL, syscall.SIGKILL)
}
