// Progress bars and spinners example. This shows how velocity's progress
// primitives look in a real build/deploy context, cycling through all spinner
// styles and showing a progress bar with dynamic label updates.
package main

import (
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
	"github.com/tensorfoundrylabs/velocity/v2/live"
)

func main() {
	log := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(os.Stdout))
	log.Info("Starting deployment pipeline")

	// Show a progress bar simulating a dependency download.
	// Total is the number of packages we're pretending to fetch.
	pb := live.NewProgressBar(os.Stdout, 10, "Downloading deps")
	for i := range int64(10) {
		time.Sleep(80 * time.Millisecond)
		pb.Increment(1)
		if i == 4 {
			pb.SetLabel("Resolving checksums")
		}
	}
	pb.Complete()

	log.Info("Dependencies resolved")

	// Cycle through all five spinner styles so you can see what each looks like.
	// Each one runs for about half a second, which is enough to see a few frames.
	spinners := []struct {
		style   live.SpinnerStyle
		label   string
		success string
	}{
		{live.SpinnerStyleBraille, "Compiling (braille)...", "Compiled"},
		{live.SpinnerStyleDots, "Linking (dots)...", "Linked"},
		{live.SpinnerStyleArrows, "Packaging (arrows)...", "Packaged"},
		{live.SpinnerStyleBounce, "Pushing image (bounce)...", "Image pushed"},
		{live.SpinnerStyleBar, "Health check (bar)...", ""},
	}

	for i, sp := range spinners {
		s := live.NewSpinner(os.Stdout, sp.label)
		s.SetStyle(sp.style)
		time.Sleep(500 * time.Millisecond)

		if i == len(spinners)-1 {
			// Simulate a health check that fails, so we can show StopWithError too.
			s.StopWithError("Health check timed out")
		} else {
			s.StopWithSuccess(sp.success)
		}
	}

	// Second progress bar: simulating a rollback after the failed health check.
	log.Warn("Rolling back to previous version")
	rb := live.NewProgressBar(os.Stdout, 5, "Rolling back")
	for range int64(5) {
		time.Sleep(100 * time.Millisecond)
		rb.Increment(1)
	}
	rb.Complete()

	log.Info("Rollback complete", velocity.String("version", "v1.9.3"))
	log.Info("pipeline finished")
}
