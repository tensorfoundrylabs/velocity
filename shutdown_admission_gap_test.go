package velocity

// Permanent coverage for the Terra-confirmation gap in F2 admission: the
// preemption window between a helper's family-close check and its final write
// exists WITHOUT any user callback (KeyValues/Bullet parked on the theme lock
// reproduced post-Close output 10/10). These tests REQUIRE the correct
// behaviour: Close blocks until an admitted operation completes, its output
// lands before Close returns, nothing lands after, and clean paths do not
// hang (every admission balanced).

import (
	"bufio"
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitForStack polls goroutine stacks until want appears — the deterministic
// "helper is parked inside preparation" handshake, no sleeps.
func waitForStack(t *testing.T, want string, deadline time.Duration) bool {
	t.Helper()
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		stacks := make([]byte, 1<<20)
		n := runtime.Stack(stacks, true)
		if strings.Contains(string(stacks[:n]), want) {
			return true
		}
		runtime.Gosched()
	}
	return false
}

// KeyValues and Bullet are admitted before preparation, so a helper parked
// inside Style()'s theme read is DRAINED by Close: Close blocks until the
// helper finishes, the output lands before Close returns, and nothing lands
// after. FORCE_COLOR is required so Style() actually reaches the theme read.
func TestClose_DrainsKeyValuesAndBulletPreparation(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	cases := []struct {
		name   string
		call   func(*Logger)
		marker string
	}{
		{"KeyValues", func(l *Logger) { l.KeyValues([]KeyValuePair{{Key: "k", Value: "late"}}) }, "late"},
		{"Bullet", func(l *Logger) { l.Bullet(0, "late-bullet") }, "late-bullet"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			l := New(WithConsoleOutput(&out), WithLevel(LevelDebug))

			// Hold the theme lock so the helper parks inside Style() AFTER
			// its admission — the confirmation's window, inverted.
			l.themes.mu.Lock()
			done := make(chan struct{})
			go func() {
				defer close(done)
				tc.call(l)
			}()
			if !waitForStack(t, "(*themeState).get", 5*time.Second) {
				l.themes.mu.Unlock()
				t.Fatal("helper never reached the theme read inside preparation")
			}

			closeDone := make(chan error, 1)
			go func() { closeDone <- l.Close() }()

			// The drain cannot progress while the helper holds its admission
			// parked on the theme lock. An early return here is the defect.
			select {
			case err := <-closeDone:
				l.themes.mu.Unlock()
				t.Fatalf("Close returned %v while an admitted helper was still preparing output", err)
			case <-time.After(150 * time.Millisecond):
			}

			// Let the helper finish: output lands, then Close returns.
			l.themes.mu.Unlock()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("helper never completed after the theme lock was released")
			}
			select {
			case err := <-closeDone:
				if err != nil {
					t.Errorf("Close returned error: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Close never returned after the admitted helper completed — admission not balanced")
			}

			if !strings.Contains(out.String(), tc.marker) {
				t.Errorf("drained helper output did not land before Close returned; got: %q", out.String())
			}
			before := out.Len()
			time.Sleep(20 * time.Millisecond) //nolint:staticcheck // detection window only; the assertion is on stability
			if after := out.Len(); after != before {
				t.Errorf("%d bytes written after Close returned", after-before)
			}
		})
	}
}

// Clean paths balance every admission: after helpers complete normally, Close
// returns promptly (a leaked in-flight count would hang the drain).
func TestClose_AdmissionBalancedOnCleanHelperPaths(t *testing.T) {
	var out bytes.Buffer
	l := New(WithConsoleOutput(&out), WithNotifyOutput(&out), WithLevel(LevelDebug))
	child := l.With(String("svc", "child"))

	l.KeyValues([]KeyValuePair{{Key: "k", Value: "v"}})
	l.Bullet(1, "bullet")
	l.NotifyLines("line")
	child.Notify("%s", "child-notify")
	l.NotifyBox(NewBox("t", "body", l.Style()))
	l.BannerLines("banner")

	done := make(chan error, 1)
	go func() { done <- l.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung on a clean path — an admission was not balanced")
	}
}

// sgStringer pauses Notify's Sprintf with channel handshakes (a caller's
// Stringer can block after admission).
type sgStringer struct {
	entered, release chan struct{}
	once             sync.Once
}

func (s *sgStringer) String() string {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return "late-notify"
}

// buildNotifyLogger returns a logger, its captured notify output and the mutex
// that serialises that output. Console-less loggers route Notify through the
// package fallback mutex — admission must exist there too.
func buildNotifyLogger(withConsole bool) (*Logger, *bytes.Buffer, *sync.Mutex) {
	var buf bytes.Buffer
	if withConsole {
		l := New(WithConsoleOutput(&buf), WithNotifyOutput(&buf), WithLevel(LevelDebug))
		return l, &buf, &l.consoleWriter.mu
	}
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil // no console writer at all
	cfg.NotifyOutput = &buf
	l := newFromConfig(cfg)
	return l, &buf, &notifyMu
}

// Every Notify variant, with and without a console writer: an admitted
// notification paused mid-flight (Stringer for Notify, destination mutex for
// the callback-free variants) is drained by Close — Close blocks, the output
// lands before Close returns, and post-close calls produce nothing.
func TestClose_DrainsNotifyFamily(t *testing.T) {
	t.Run("Notify_Stringer", func(t *testing.T) {
		for _, withConsole := range []bool{true, false} {
			name := map[bool]string{true: "with_console", false: "no_console_writer"}[withConsole]
			t.Run(name, func(t *testing.T) {
				l, buf, _ := buildNotifyLogger(withConsole)
				testAdmittedDrain(t, l, buf, func(l *Logger, entered, release, done chan struct{}) {
					go func() {
						defer close(done)
						l.Notify("%s", &sgStringer{entered: entered, release: release})
					}()
					<-entered
				}, "late-notify")
			})
		}
	})

	t.Run("NotifyLines_dest_mutex", func(t *testing.T) {
		for _, withConsole := range []bool{true, false} {
			name := map[bool]string{true: "with_console", false: "no_console_writer"}[withConsole]
			t.Run(name, func(t *testing.T) {
				l, buf, mu := buildNotifyLogger(withConsole)
				testAdmittedMutexDrain(t, l, buf, mu,
					func(l *Logger, done chan struct{}) {
						go func() {
							defer close(done)
							l.NotifyLines("late-lines")
						}()
					},
					"(*Logger).NotifyLines", "late-lines")
			})
		}
	})

	t.Run("NotifyBox_dest_mutex", func(t *testing.T) {
		for _, withConsole := range []bool{true, false} {
			name := map[bool]string{true: "with_console", false: "no_console_writer"}[withConsole]
			t.Run(name, func(t *testing.T) {
				l, buf, mu := buildNotifyLogger(withConsole)
				// Build the Box before the mutex is held: the closure's
				// Style() read would otherwise park on the same mutex (the
				// console case) before NotifyBox is entered.
				box := NewBox("t", "late-box", l.Style())
				testAdmittedMutexDrain(t, l, buf, mu,
					func(l *Logger, done chan struct{}) {
						go func() {
							defer close(done)
							l.NotifyBox(box)
						}()
					},
					"(*Logger).NotifyBox", "late-box")
			})
		}
	})

	t.Run("Child_notify_drained_by_parent_close", func(t *testing.T) {
		l, buf, _ := buildNotifyLogger(false)
		child := l.With(String("svc", "child"))
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			child.Notify("%s", &sgStringer{entered: entered, release: release})
		}()
		<-entered

		closeDone := make(chan error, 1)
		go func() { closeDone <- l.Close() }() // parent closes the family
		select {
		case err := <-closeDone:
			close(release)
			t.Fatalf("parent Close returned %v while a child's admitted Notify was in flight", err)
		case <-time.After(150 * time.Millisecond):
		}
		close(release)
		<-done
		select {
		case <-closeDone:
		case <-time.After(10 * time.Second):
			t.Fatal("parent Close never returned after the child's Notify completed")
		}
		if !strings.Contains(buf.String(), "late-notify") {
			t.Errorf("child Notify output missing: %q", buf.String())
		}
	})

	t.Run("Post_close_notify_produces_nothing", func(t *testing.T) {
		l, buf, _ := buildNotifyLogger(false)
		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		l.Notify("after %s", "close")
		l.NotifyLines("after-lines")
		l.NotifyBox(NewBox("t", "after-box", nil))
		if buf.Len() != 0 {
			t.Errorf("post-close notification output: %q", buf.String())
		}
	})
}

// sgPanicFlushSink panics on Flush, so Logger.Close's drain panics
// synchronously inside ConsoleWriter.Close. The family closeDone must still
// be published (deferred close), so concurrent family Closes complete rather
// than stranding on it.
type sgPanicFlushSink struct{ bytes.Buffer }

func (*sgPanicFlushSink) Flush() error { panic("sg: user flush panicked during close") }

// A panic anywhere in the synchronous drain (here: a caller-supplied buffered
// sink whose Flush panics inside ConsoleWriter.Close) must not leave
// concurrent family Closes blocked forever: the panicking caller unwinds and
// the deferred closeDone still publishes completion.
//
// Note: a NAMED writer whose Close panics does not reach this path — the
// MultiWriter worker calls it on its own goroutine, where an unrecovered
// panic is process-fatal by Go semantics regardless of closeFamily. That is
// pre-existing and orthogonal to the family-close contract tested here.
func TestClose_PanicDuringDrainDoesNotStrandFamily(t *testing.T) {
	sink := &sgPanicFlushSink{}
	l := New(WithConsoleOutput(sink), WithLevel(LevelDebug))
	child := l.With(String("svc", "child"))

	panicked := make(chan struct{})
	closeA := make(chan struct{})
	go func() {
		defer close(closeA)
		defer close(panicked)
		defer func() { _ = recover() }() //nolint:errcheck // observing the panic is the point
		_ = l.Close()
	}()
	<-panicked // first Close has unwound through the drain panic

	closeB := make(chan error, 1)
	go func() { closeB <- child.Close() }()
	select {
	case err := <-closeB:
		if err != nil {
			t.Errorf("concurrent family Close returned %v; want the zero recorded result after a drain panic", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent family Close stranded after a drain panic — closeDone was not published")
	}
	select {
	case <-closeA:
	case <-time.After(10 * time.Second):
		t.Fatal("first Close never completed its unwind")
	}
}

// foBufferedSink is a bufio middle layer over the underlying capture that
// signals every Flush completion — the point of the ordering test is that the
// final flush happens AFTER admitted family output, so the buffered layer is
// essential (an unbuffered capture cannot miss bytes).
type foBufferedSink struct {
	*bufio.Writer
	flushed chan struct{}
}

func (b *foBufferedSink) Flush() error {
	err := b.Writer.Flush()
	b.flushed <- struct{}{}
	return err
}

// The F2 order confirmation: when Notify and console logging share one
// buffered destination, an admitted notification paused mid-format must
// complete and reach the UNDERLYING sink before Close returns — the final
// flush may not run while the notification is still writing. Requires BOTH
// operation completion and emitted bytes; a flushed-too-early Close leaves
// the notification buffered forever.
func TestClose_AdmittedNotifyEmittedThroughSharedBufferedSink(t *testing.T) {
	t.Run("Notify_Stringer", func(t *testing.T) {
		var underlying bytes.Buffer
		buffered := &foBufferedSink{Writer: bufio.NewWriter(&underlying), flushed: make(chan struct{}, 8)}
		l := New(WithConsoleOutput(buffered), WithNotifyOutput(buffered), WithLevel(LevelDebug))
		l.Info("console-line") // sits in the same buffer until Close flushes

		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			l.Notify("%s", &sgStringer{entered: entered, release: release})
		}()
		<-entered

		closeDone := make(chan error, 1)
		go func() { closeDone <- l.Close() }()

		// While the admitted notification is paused, Close must neither flush
		// the shared destination nor return — either would prove the old
		// drain-before-family-output ordering.
		select {
		case err := <-closeDone:
			t.Fatalf("Close returned %v while an admitted Notify was still formatting", err)
		case <-buffered.flushed:
			t.Fatal("Close flushed the shared destination while an admitted Notify was still writing — its bytes would stay buffered past the final flush")
		case <-time.After(150 * time.Millisecond):
		}

		close(release)
		<-done
		select {
		case err := <-closeDone:
			if err != nil {
				t.Errorf("Close: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Close never returned after the admitted Notify completed")
		}

		out := underlying.String()
		if !strings.Contains(out, "late-notify") {
			t.Errorf("notification never reached the underlying sink before Close returned; underlying: %q", out)
		}
		if !strings.Contains(out, "console-line") {
			t.Errorf("console line never reached the underlying sink; underlying: %q", out)
		}
	})

	t.Run("NotifyLines_dest_mutex", func(t *testing.T) {
		var underlying bytes.Buffer
		buffered := &foBufferedSink{Writer: bufio.NewWriter(&underlying), flushed: make(chan struct{}, 8)}
		l := New(WithConsoleOutput(buffered), WithNotifyOutput(buffered), WithLevel(LevelDebug))

		// Park the admitted NotifyLines on the destination mutex (console
		// writer present, so Notify serialises on consoleWriter.mu).
		mu := &l.consoleWriter.mu
		mu.Lock()
		done := make(chan struct{})
		go func() {
			defer close(done)
			l.NotifyLines("late-lines")
		}()
		if !waitForStack(t, "(*Logger).NotifyLines", 5*time.Second) {
			mu.Unlock()
			t.Fatal("NotifyLines never reached its destination mutex")
		}

		closeDone := make(chan error, 1)
		go func() { closeDone <- l.Close() }()
		select {
		case err := <-closeDone:
			mu.Unlock()
			t.Fatalf("Close returned %v while an admitted NotifyLines was parked on the destination mutex", err)
		case <-buffered.flushed:
			mu.Unlock()
			t.Fatal("Close flushed the shared destination while an admitted NotifyLines was still writing")
		case <-time.After(150 * time.Millisecond):
		}

		mu.Unlock()
		<-done
		select {
		case <-closeDone:
		case <-time.After(10 * time.Second):
			t.Fatal("Close never returned after the parked NotifyLines completed")
		}
		if !strings.Contains(underlying.String(), "late-lines") {
			t.Errorf("NotifyLines output never reached the underlying sink; underlying: %q", underlying.String())
		}
	})
}

// testAdmittedDrain: start pauses inside formatting (entered handshake);
// Close must block until the release, then the output must have landed.
func testAdmittedDrain(t *testing.T, l *Logger, buf *bytes.Buffer, start func(*Logger, chan struct{}, chan struct{}, chan struct{}), want string) {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	start(l, entered, release, done)

	closeDone := make(chan error, 1)
	go func() { closeDone <- l.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned %v while an admitted Notify was still formatting", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	<-done
	select {
	case err := <-closeDone:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the admitted Notify completed")
	}
	if !strings.Contains(buf.String(), want) {
		t.Errorf("admitted Notify output did not land before Close returned: %q", buf.String())
	}
	before := buf.Len()
	time.Sleep(20 * time.Millisecond) //nolint:staticcheck // detection window only
	if after := buf.Len(); after != before {
		t.Errorf("%d bytes written after Close returned", after-before)
	}
}

// testAdmittedMutexDrain: the call parks on the destination mutex after
// admission; frame identifies it in the stack handshake.
func testAdmittedMutexDrain(t *testing.T, l *Logger, buf *bytes.Buffer, mu *sync.Mutex, start func(*Logger, chan struct{}), frame, want string) {
	t.Helper()
	mu.Lock()
	done := make(chan struct{})
	start(l, done)
	if !waitForStack(t, frame, 5*time.Second) {
		mu.Unlock()
		t.Fatalf("notification never reached its destination mutex (%s)", frame)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- l.Close() }()
	select {
	case err := <-closeDone:
		mu.Unlock()
		t.Fatalf("Close returned %v while an admitted notification was parked on the destination mutex", err)
	case <-time.After(150 * time.Millisecond):
	}

	mu.Unlock()
	<-done
	select {
	case <-closeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the parked notification completed")
	}
	if !strings.Contains(buf.String(), want) {
		t.Errorf("admitted notification output missing: %q", buf.String())
	}
}
