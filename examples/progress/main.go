// Progress bars and spinners example. Shows how the logger and the live
// widgets share one terminal through live.NewOutput: log records emitted
// DURING progress clear the live rows, write the record, then redraw, so
// nothing glues onto or clobbers the displays.
package main

import (
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
	"github.com/tensorfoundrylabs/velocity/v2/live"
)

func main() {
	// One coordinator for the logger and every widget below. Pass the SAME
	// object to WithConsoleOutput and the widget constructors.
	out := live.NewOutput(os.Stdout)
	log := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	log.Info("Starting deployment pipeline")

	// Show a progress bar simulating a dependency download. Total is the
	// number of packages we're pretending to fetch. Note the log mid-flight:
	// with the shared output it lands cleanly between progress redraws.
	pb := live.NewProgressBar(out, 10, "Downloading deps")
	for i := range int64(10) {
		time.Sleep(80 * time.Millisecond)
		pb.Increment(1)
		if i == 4 {
			pb.SetLabel("Resolving checksums")
			log.Info("halfway through the download")
		}
	}
	pb.Complete()

	log.Info("Dependencies resolved")

	// Cycle through all five spinner styles so you can see what each looks
	// like, logging while each one spins.
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
		s := live.NewSpinner(out, sp.label)
		s.SetStyle(sp.style)
		time.Sleep(300 * time.Millisecond)
		log.Info("step in flight", velocity.String("step", sp.label))

		time.Sleep(200 * time.Millisecond)
		if i == len(spinners)-1 {
			// Simulate a health check that fails, so we can show StopWithError too.
			s.StopWithError("Health check timed out")
		} else {
			s.StopWithSuccess(sp.success)
		}
	}

	// Second progress bar: simulating a rollback after the failed health
	// check, with a warning mid-rollback.
	log.Warn("Rolling back to previous version")
	rb := live.NewProgressBar(out, 5, "Rolling back")
	for i := range int64(5) {
		time.Sleep(100 * time.Millisecond)
		rb.Increment(1)
		if i == 2 {
			log.Warn("draining connections before swap")
		}
	}
	rb.Complete()

	log.Info("Rollback complete", velocity.String("version", "v1.9.3"))
	log.Info("pipeline finished")
}
