package live

import (
	"io"
	"sync"
	"testing"
)

// TestProgressBar_ConcurrentComplete verifies that simultaneous Complete calls
// do not panic due to double-close of the done channel.
func TestProgressBar_ConcurrentComplete(_ *testing.T) {
	pb := NewProgressBar(io.Discard, 100, "test")

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pb.Complete()
		}()
	}
	wg.Wait()
}

// TestSpinner_ConcurrentStop verifies that simultaneous Stop calls
// do not panic due to double-close of the done channel.
func TestSpinner_ConcurrentStop(_ *testing.T) {
	s := NewSpinner(io.Discard, "test")

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}

// TestMultiProgress_ConcurrentStop verifies that simultaneous Stop calls
// do not panic due to double-close of the done channel.
func TestMultiProgress_ConcurrentStop(_ *testing.T) {
	mp := NewMultiProgress(io.Discard)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mp.Stop()
		}()
	}
	wg.Wait()
}

// TestSpinner_SetStyle_NilReceiver verifies nil safety, consistent with other
// Spinner methods.
func TestSpinner_SetStyle_NilReceiver(_ *testing.T) {
	var s *Spinner
	s.SetStyle(SpinnerStyleDots)
}
