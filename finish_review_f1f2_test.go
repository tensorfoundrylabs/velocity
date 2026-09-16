package velocity

// Permanent coverage for the two High findings from the independent finish
// review (docs/specs/logging-hardening-final-review.md), with deterministic
// handshakes instead of the reviewer's scratch-only scheduling hooks.
//
// F1: WriteReliable's acknowledgement must be tied to the processing of the
// barrier item itself (FIFO sentinel), never to aggregate counts a producer
// can publish out of order.
//
// F2: Close must drain admitted calls — a write cycle paused in formatting
// (blocking Stringer or Renderable callback) completes and lands BEFORE Close
// returns; nothing is written after.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- F2 -----------------------------------------------------------------------

type frStringer struct{ entered, release chan struct{} }

func (s frStringer) String() string { close(s.entered); <-s.release; return "late-record" }

type frRenderable struct{ entered, release chan struct{} }

func (s frRenderable) Render(w io.Writer) error {
	close(s.entered)
	<-s.release
	_, err := io.WriteString(w, "late-render\n")
	return err
}

// Close during in-flight formatting: the admitted call must be drained — its
// output lands before Close returns, and nothing lands after. Without the
// in-flight admission, Close completed while the blocked callback held the
// record, then the write landed post-Close (the reviewer's Close/Info and
// Close/Render probes).
func TestClose_DrainsInFlightFormatting(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, entered, release chan struct{}, out *bytes.Buffer, l *Logger, done chan struct{})
	}{
		{
			name: "Info blocking Stringer console",
			run: func(t *testing.T, entered, release chan struct{}, out *bytes.Buffer, l *Logger, done chan struct{}) {
				go func() {
					defer close(done)
					l.Info("message", Stringer("field", frStringer{entered: entered, release: release}))
				}()
				<-entered
			},
		},
		{
			name: "Render blocking callback",
			run: func(t *testing.T, entered, release chan struct{}, out *bytes.Buffer, l *Logger, done chan struct{}) {
				go func() {
					defer close(done)
					l.Render(frRenderable{entered: entered, release: release})
				}()
				<-entered
			},
		},
		{
			name: "JSON blocking Stringer",
			run: func(t *testing.T, entered, release chan struct{}, out *bytes.Buffer, l *Logger, done chan struct{}) {
				go func() {
					defer close(done)
					l.Info("message", Stringer("field", frStringer{entered: entered, release: release}))
				}()
				<-entered
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var l *Logger
			if tc.name == "JSON blocking Stringer" {
				// Real io.Discard: no console writer at all, so the Stringer is
				// formatted exactly once (a second writer would close the
				// entered channel twice).
				l = New(WithConsoleOutput(io.Discard), WithStructuredOutput(&out), WithLevel(LevelDebug))
			} else {
				l = New(WithConsoleOutput(&out), WithColour(false), WithLevel(LevelDebug))
			}

			entered := make(chan struct{})
			release := make(chan struct{})
			done := make(chan struct{})
			tc.run(t, entered, release, &out, l, done)

			// Formatting is in flight (admission taken, callback parked). Close
			// must not return until that cycle is drained.
			closed := make(chan struct{})
			go func() { _ = l.Close(); close(closed) }()

			select {
			case <-closed:
				close(release)
				<-done
				t.Fatal("Close returned while an admitted write cycle was still formatting")
			case <-time.After(150 * time.Millisecond):
				// Still blocked on the drain — correct.
			}

			// Release the callback: the record must land, then Close returns.
			close(release)
			<-done
			select {
			case <-closed:
			case <-time.After(10 * time.Second):
				t.Fatal("Close never returned after the in-flight cycle finished")
			}

			want := "late-record"
			if strings.Contains(tc.name, "Render") {
				want = "late-render"
			}
			if !strings.Contains(out.String(), want) {
				t.Errorf("drained admitted call did not land before Close returned; output: %q", out.String())
			}

			// No post-close growth.
			before := out.Len()
			time.Sleep(20 * time.Millisecond) //nolint:staticcheck // detection window only; the assertion is on stability
			if after := out.Len(); after != before {
				t.Errorf("%d bytes written after Close returned", after-before)
			}
		})
	}
}

// ---- F1 -----------------------------------------------------------------------

// frGatedFatalWriter records every entry; the fatal entry itself parks on a
// gate so the test can prove WriteReliable's barrier waits for THAT entry
// while concurrent ordinary senders keep the queue busy.
type frGatedFatalWriter struct {
	fatalMsg     string
	fatalEntered chan struct{}
	releaseFatal chan struct{}
	mu           sync.Mutex
	msgs         []string
}

func (w *frGatedFatalWriter) Write(e *Entry) error {
	if e.Message == w.fatalMsg {
		close(w.fatalEntered)
		<-w.releaseFatal
	}
	w.mu.Lock()
	w.msgs = append(w.msgs, e.Message)
	w.mu.Unlock()
	return nil
}

func (w *frGatedFatalWriter) Close() error { return nil }

// WriteReliable with concurrent ordinary senders: the barrier must hold while
// the reliable entry's own write is in progress. The finish review reproduced
// the counter-race variant with a scheduling hook (scratch-only); this test
// guards the invariant deterministically — under the aggregate-counter design
// a preempted ordinary sender could satisfy the wait with already-processed
// entries while the barrier item was still queued.
func TestWriteReliable_BarrierWaitsForOwnEntryAmidSenders(t *testing.T) {
	const fatalMsg = "fr-fatal-entry"
	gw := &frGatedFatalWriter{
		fatalMsg:     fatalMsg,
		fatalEntered: make(chan struct{}),
		releaseFatal: make(chan struct{}),
	}
	mw := NewMultiWriter()
	mw.AddWriter("sink", gw)

	// Concurrent ordinary senders keep the queue and the worker busy so any
	// producer-side accounting race has every opportunity to interleave.
	stop := make(chan struct{})
	var senders sync.WaitGroup
	for g := range 4 {
		senders.Add(1)
		go func(g int) {
			defer senders.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				e := GetEntry()
				e.SetLevel(LevelInfo)
				e.SetMessage(frOrdinaryMsg(g, i))
				e.Write()
				_ = mw.Write(e)
				e.Release()
				i++
			}
		}(g)
	}

	fatal := GetEntry()
	fatal.SetLevel(LevelFatal)
	fatal.SetMessage(fatalMsg)
	fatal.Write()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_ = mw.WriteReliable(fatal)
		fatal.Release()
	}()

	// The fatal entry's own write is in progress (parked in the sink).
	<-gw.fatalEntered

	select {
	case <-returned:
		t.Fatal("WriteReliable returned while its own entry was still being written — the barrier acknowledged something other than the barrier item")
	case <-time.After(150 * time.Millisecond):
	}

	// Let the fatal write finish: the FIFO sentinel behind it is processed
	// next, the barrier releases, and only then may WriteReliable return.
	close(gw.releaseFatal)
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("WriteReliable never returned after the fatal write completed")
	}

	close(stop)
	senders.Wait()
	_ = mw.Close()

	gw.mu.Lock()
	msgs := append([]string(nil), gw.msgs...)
	gw.mu.Unlock()
	fatalIdx := -1
	for i, m := range msgs {
		if m == fatalMsg {
			fatalIdx = i
			break
		}
	}
	if fatalIdx < 0 {
		t.Fatal("fatal entry was never delivered to the sink")
	}
}

func frOrdinaryMsg(g, i int) string {
	return fmt.Sprintf("fr-ordinary-%d-%d", g, i)
}
