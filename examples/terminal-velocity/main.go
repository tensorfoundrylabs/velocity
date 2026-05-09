// Terminal Velocity - GPU cluster deploy simulator. This is the hero example
// for the velocity logging library. It walks through deploying Llama-3.1-70B
// across a 4-node GPU cluster, exercising banners, spinners, progress bars,
// structured fields, tree views, tables, and pretty output along the way.
//
// Node-3 has a disk space issue, which causes it to fail and trigger a
// recovery path. Real deployments are rarely clean happy-paths.
package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tensorfoundrylabs/velocity"
	"github.com/tensorfoundrylabs/velocity/pretty"
)

func main() {
	startTime := time.Now()

	// Night Owl gives us a dark, high-contrast palette that looks excellent on
	// any decent terminal. It's the default for a reason.
	log := velocity.NewWithOptions(
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelDebug),
	)
	defer func() { _ = log.Close() }()

	p := velocity.NewPrettyFromLogger(log)

	stageBanner(log)
	stageClusterDiscovery(log, p)
	stageDeploymentConfig(log, p)
	stagePreflightChecks(log, p)
	stageModelDistribution(log, p)
	failed := stageNodeDeployment(log, p)
	stageRecovery(log, p, failed)
	stageHealthVerification(log, p)
	stageSummary(log, p, startTime)
}

// stageBanner prints the title screen. Simple ASCII art keeps it portable
// across terminals that might not handle fancy Unicode block characters.
func stageBanner(log *velocity.Logger) {
	ascii := []string{
		"  ______                    _             __     ",
		" /_  __/__  _________ ___  (_)___  ____ _/ /     ",
		"  / / / _ \\/ ___/ __ `__ \\/ / __ \\/ __ `/ /      ",
		" / / /  __/ /  / / / / / / / / / / /_/ / /       ",
		"/_/  \\___/_/ _/_/ /_/ /_/_/_/_/_/\\__,_/_/        ",
		"| |  / /__  / /___  _____(_) /___  __            ",
		"| | / / _ \\/ / __ \\/ ___/ / __/ / / /            ",
		"| |/ /  __/ / /_/ / /__/ / /_/ /_/ /             ",
		"|___/\\___/_/\\____/\\___/_/\\__/\\__, /              ",
		"                            /____/               ",
	}

	banner := pretty.CreateBanner("Terminal Velocity", "0.1.0", "tensorfoundry.io", ascii)
	log.Banner(strings.Split(strings.TrimRight(banner, "\n"), "\n")...)
	log.Newline()
}

// stageClusterDiscovery scans for available GPU nodes and reports what it finds.
func stageClusterDiscovery(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Cluster Discovery")

	spinner := pretty.NewSpinner(os.Stdout, "Scanning network for GPU nodes...")
	time.Sleep(1200 * time.Millisecond)
	spinner.StopWithSuccess("Found 4 nodes with 16 GPUs total")

	p.SystemInfo(&velocity.SystemInfoData{
		Title:   "GPU Cluster",
		Version: "CUDA 13.0",
		Fields: []velocity.KeyValuePair{
			{Key: "Nodes", Value: "4"},
			{Key: "GPUs Total", Value: "8 x NVIDIA RTX Pro 6000 96GB"},
			{Key: "CUDA Version", Value: "13.0"},
			{Key: "Driver Version", Value: "580.95.05"},
			{Key: "Cluster Network", Value: "InfiniBand HDR 200Gb/s"},
			{Key: "Shared Storage", Value: "NFS /mnt/models (48TB)"},
		},
	})

	log.Info("Cluster discovery complete",
		velocity.Int("nodes", 4),
		velocity.Int("gpus", 8),
		velocity.String("cuda", "13.0"),
	)
	log.Newline()
}

// stageDeploymentConfig displays the model deployment configuration as a tree.
func stageDeploymentConfig(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Deployment Configuration")

	// Render the tree indented under the log line — the explicit "nest under
	// message column" path, using log.Render with a velocity.Tree Renderable.
	log.Info("Llama-3.1-70B Deployment Plan")
	log.Render(velocity.NewTree([]velocity.TreeItem{
		{Key: "Model", Value: "meta-llama/Llama-3.1-70B-Instruct"},
		{Key: "Replicas", Value: 4},
		{Key: "GPU Type", Value: "NVIDIA RTX Pro 6000 96GB"},
		{
			Key: "Parallelism",
			Children: []velocity.TreeItem{
				{Key: "Tensor Parallelism", Value: 2},
				{Key: "Pipeline Parallelism", Value: 1},
			},
		},
		{
			Key: "Quantisation",
			Children: []velocity.TreeItem{
				{Key: "Method", Value: "AWQ"},
				{Key: "Bits", Value: "4-bit"},
				{Key: "Group Size", Value: 128},
			},
		},
		{Key: "Max Batch Size", Value: 32},
		{Key: "Max Sequence Length", Value: 8192},
	}, velocity.ThemeNightOwl))

	log.Newline()
}

// stagePreflightChecks runs pre-flight validation across all nodes and reports results.
// Node-3 fails the disk space check, which foreshadows the deployment failure.
func stagePreflightChecks(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Pre-flight Checks")

	sf := log.Status()

	rows := [][]string{
		{"GPU Memory", "node-0", sf.Okay("OK"), "79.8 GB free"},
		{"GPU Memory", "node-1", sf.Okay("OK"), "79.8 GB free"},
		{"GPU Memory", "node-2", sf.Okay("OK"), "79.8 GB free"},
		{"GPU Memory", "node-3", sf.Okay("OK"), "79.8 GB free"},
		{"CUDA Version", "node-0", sf.Okay("OK"), "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-1", sf.Okay("OK"), "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-2", sf.Okay("OK"), "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-3", sf.Okay("OK"), "12.4 / driver 550.54.15"},
		{"Disk Space", "node-0", sf.Okay("OK"), "340 GB free"},
		{"Disk Space", "node-1", sf.Okay("OK"), "280 GB free"},
		{"Disk Space", "node-2", sf.Okay("OK"), "310 GB free"},
		{"Disk Space", "node-3", sf.Warn("WARN"), "18 GB free (need 35 GB)"},
		{"Network", "node-0", sf.Okay("OK"), "IB latency 1.2us"},
		{"Network", "node-1", sf.Okay("OK"), "IB latency 1.1us"},
		{"Network", "node-2", sf.Okay("OK"), "IB latency 1.3us"},
		{"Network", "node-3", sf.Okay("OK"), "IB latency 1.2us"},
	}

	p.Table(
		[]string{"Check", "Node", "Status", "Detail"},
		rows,
	)

	// Flag the disk issue immediately so the operator has a chance to notice.
	log.Warn("node-3 disk space is critically low; deploy will attempt but may fail",
		velocity.String("node", "node-3"),
		velocity.String("available", "18 GB"),
		velocity.String("required", "35 GB"),
	)

	log.Newline()
}

// stageModelDistribution downloads model weights and builds inference containers.
// This is the longest stage because it moves the most data.
func stageModelDistribution(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Model Distribution")

	// Child logger carries the stage context on every structured entry without
	// us having to repeat it on every log call.
	distLog := log.With(velocity.String("stage", "distribute"))

	const weightBytes int64 = 35_000 // units = MB (35 GB quantised)

	pb := pretty.NewProgressBar(os.Stdout, weightBytes, "Downloading model weights")

	// Drive the progress bar without logging mid-loop. Mixing log writes with
	// a progress bar on the same writer causes line-overwrite interleaving.
	var downloaded int64
	for downloaded < weightBytes {
		chunk := 700 + (downloaded/1000)%400 // speed varies a bit
		downloaded += chunk
		if downloaded > weightBytes {
			downloaded = weightBytes
		}
		pb.Update(downloaded)
		time.Sleep(18 * time.Millisecond)
	}

	pb.Complete()

	// Log the milestone after the bar has finished and emitted its newline.
	distLog.Info("model weights verified",
		velocity.Int64("size_mb", weightBytes),
		velocity.String("checksum", "sha256:a3f9...d12e"),
	)

	// Container build is quicker but still worth showing.
	cb := pretty.NewProgressBar(os.Stdout, 15, "Building inference containers")
	layers := []string{
		"base: nvcr.io/nvidia/pytorch:24.01",
		"layer: vllm==0.4.2",
		"layer: transformers==4.40.0",
		"layer: model weights (quantised)",
		"layer: serving config",
	}

	// Accumulate completed layers and log them after the bar is done so log
	// output does not interleave with the progress bar line.
	var completedLayers []string
	for i, layer := range layers {
		isLastLayer := i == len(layers)-1
		for step := range 3 {
			cb.Increment(1)
			isLastStep := isLastLayer && step == 2
			if isLastStep {
				// Complete immediately after the final increment so the render
				// goroutine sees the done signal before the ticker fires again.
				cb.Complete()
			} else {
				time.Sleep(120 * time.Millisecond)
			}
		}
		if !isLastLayer {
			completedLayers = append(completedLayers, layer)
		}
	}

	// Log the per-layer completions now that the bar has finished its line.
	for i, layer := range completedLayers {
		distLog.Debug("container layer complete",
			velocity.String("layer", layer),
			velocity.Int("index", i),
		)
	}

	distLog.Info("inference containers ready", velocity.String("image", "velocity/llama3-70b-awq:0.1.0"))
	log.Newline()
}

// stageNodeDeployment pushes the model to each node in turn.
// Returns the name of any node that failed, or an empty string for full success.
func stageNodeDeployment(log *velocity.Logger, p *velocity.Pretty) string {
	p.Section("Deploying to Nodes")

	nodes := []struct {
		name     string
		ip       string
		willFail bool
	}{
		{name: "node-0", ip: "10.0.1.10", willFail: false},
		{name: "node-1", ip: "10.0.1.11", willFail: false},
		{name: "node-2", ip: "10.0.1.12", willFail: false},
		{name: "node-3", ip: "10.0.1.13", willFail: true}, // pre-flight warned us
	}

	var failed string

	for _, node := range nodes {
		nodeLog := log.With(
			velocity.String("node", node.name),
			velocity.String("ip", node.ip),
		)

		spinner := pretty.NewSpinner(os.Stdout, fmt.Sprintf("Deploying to %s (%s)...", node.name, node.ip))
		time.Sleep(900 * time.Millisecond)

		if node.willFail {
			spinner.StopWithError(fmt.Sprintf("Deployment to %s failed", node.name))

			nodeLog.ErrorDetailed("container failed to start: insufficient disk space",
				velocity.String("error", "no space left on device"),
				velocity.String("disk_used", "93%"),
				velocity.String("disk_free", "18 GB"),
				velocity.String("required", "35 GB"),
				velocity.String("suggestion", "free space or add a volume"),
			)

			failed = node.name
		} else {
			spinner.StopWithSuccess(node.name + " ready, inference endpoint active")
			nodeLog.Info("node deployment successful",
				velocity.String("endpoint", "http://"+net.JoinHostPort(node.ip, "8080")+"/v1"),
				velocity.String("model", "llama-3.1-70b-awq"),
			)
		}
	}

	log.Newline()
	return failed
}

// stageRecovery handles the node-3 failure by redistributing its load to node-0.
// In a real system you would update the load balancer config; here we just
// log what would happen.
func stageRecovery(log *velocity.Logger, p *velocity.Pretty, failedNode string) {
	if failedNode == "" {
		return
	}

	p.Section("Recovery")

	recoveryLog := log.With(
		velocity.String("failed_node", failedNode),
		velocity.String("stage", "recovery"),
	)

	recoveryLog.Warn("initiating workload reallocation",
		velocity.String("from", failedNode),
		velocity.String("to", "node-0"),
		velocity.String("strategy", "single-node-overflow"),
	)

	spinner := pretty.NewSpinner(os.Stdout, fmt.Sprintf("Reallocating %s workload to node-0...", failedNode))
	time.Sleep(1400 * time.Millisecond)
	spinner.StopWithSuccess("Workload reallocated, node-0 running at 2x replicas")

	recoveryLog.Info("reallocation complete",
		velocity.String("node_0_replicas", "2"),
		velocity.String("lb_config", "updated"),
	)

	log.Newline()
}

// stageHealthVerification pings every endpoint and shows a summary table.
func stageHealthVerification(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Health Verification")

	sf := log.Status()

	rows := [][]string{
		{"node-0", "llama-3.1-70b-awq", sf.Okay("HEALTHY"), "38 ms", "http://10.0.1.10:8080/v1"},
		{"node-0*", "llama-3.1-70b-awq", sf.Info("RELOCATED"), "41 ms", "http://10.0.1.10:8080/v1 (replica 2)"},
		{"node-1", "llama-3.1-70b-awq", sf.Okay("HEALTHY"), "35 ms", "http://10.0.1.11:8080/v1"},
		{"node-2", "llama-3.1-70b-awq", sf.Okay("HEALTHY"), "37 ms", "http://10.0.1.12:8080/v1"},
		{"node-3", "-", sf.Fail("FAILED"), "-", "disk full, out of service"},
	}

	p.Table(
		[]string{"Node", "Model", "Status", "P50 Latency", "Endpoint"},
		rows,
	)

	log.Newline()
}

// stageSummary prints the final deployment summary box and the completion log line.
func stageSummary(log *velocity.Logger, p *velocity.Pretty, started time.Time) {
	elapsed := time.Since(started).Round(time.Second)

	content := fmt.Sprintf(
		"Model:        Llama-3.1-70B-Instruct (AWQ 4-bit)\n"+
			"Nodes:        4 provisioned, 3 serving directly\n"+
			"Replicas:     4 active (3 direct + 1 relocated to node-0)\n"+
			"GPUs in use:  12 of 16 (node-3 offline)\n"+
			"Endpoints:    3 nodes, 4 replica slots\n"+
			"Elapsed:      %s\n"+
			"\n"+
			"node-3 requires attention: free at least 17 GB of disk space,\n"+
			"then re-run: velocity deploy --node node-3",
		elapsed,
	)

	p.Box("Deployment Summary", content)
	log.Newline()

	log.Info("deployment complete",
		velocity.Int("nodes_total", 4),
		velocity.Int("nodes_healthy", 3),
		velocity.Int("gpus_total", 16),
		velocity.Int("gpus_active", 12),
		velocity.String("model", "llama-3.1-70b-awq"),
		velocity.String("status", "degraded-operational"),
		velocity.Duration("elapsed", elapsed),
	)

	p.Success("3/4 nodes healthy, inference stack operational. Address node-3 disk space to restore full capacity.")
}
