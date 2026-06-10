// Terminal Velocity — the flagship example for velocity v2.
//
// This simulates deploying Llama-3.1-70B across a 4-node GPU cluster and
// exercises every major v2 API in one coherent narrative:
//
//   - Branded ASCII banner
//   - Spinner (cluster scan), ProgressBar (weight download, container build)
//   - Tree (deployment plan), Table (preflight, health), SystemInfo, Box
//   - Logger.Status    — staged checklist transitions
//   - Logger.Group     — route registration block
//   - Logger.Continue  — inline server-listening block with hyperlinks
//   - velocity.Secure  — config secret field with trust-model demo
//   - velocity.Notify / NotifyBox — operator URL callout
//   - RingBufferWriter — in-process log capture; snapshot printed at the end
//
// Node-3 has a disk space issue, triggering a recovery path. Real
// deployments are rarely happy-paths.
package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
	"github.com/tensorfoundrylabs/velocity/v2/live"
)

// link is a TTY-aware wrapper for velocity.Hyperlink. OSC 8 sequences are only
// emitted when stdout is an actual terminal that supports them; plain text is
// returned otherwise so no control sequences reach pipes or log aggregators.
func link(uri, text string) string {
	if velocity.IsTerminalWriter(os.Stdout) && velocity.HyperlinksSupported() {
		return velocity.Hyperlink(uri, text)
	}
	return text
}

func main() {
	startTime := time.Now()

	// Ring buffer captures everything the logger writes for the end-of-run
	// diagnostic snapshot — the same pattern used by foundryos debug endpoints.
	ring := velocity.NewRingBufferWriter(256)

	log := velocity.New(
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelDebug),
	)
	log.AddWriter("ring", ring)
	defer func() { _ = log.Close() }()

	p := velocity.NewPrettyFromLogger(log)

	stageBanner(log)
	stageClusterDiscovery(log, p)
	stageDeploymentConfig(log, p)
	stageSecureConfig(log, p)
	stagePreflightChecks(log, p)
	stageRouteRegistration(log)
	stageModelDistribution(log, p)
	failed := stageNodeDeployment(log, p)
	stageRecovery(log, p, failed)
	stageHealthVerification(log, p)
	stageSummary(log, p, startTime, ring)
}

// --- Banner ---------------------------------------------------------------

// stageBanner prints the title screen. Plain ASCII keeps it portable
// across terminals that might not handle Unicode block art.
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

	banner := velocity.CreateBanner("Terminal Velocity", "2.0.0", "tensorfoundry.io", ascii)
	log.BannerLines(strings.Split(strings.TrimRight(banner, "\n"), "\n")...)
	log.Newline()
}

// --- Cluster discovery ----------------------------------------------------

func stageClusterDiscovery(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Cluster Discovery")

	spinner := live.NewSpinner(os.Stdout, "Scanning network for GPU nodes...")
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

	log.Info(
		"cluster discovery complete",
		velocity.Int("nodes", 4),
		velocity.Int("gpus", 16),
		velocity.String("cuda", "13.0"),
	)
	log.Newline()
}

// --- Deployment plan ------------------------------------------------------

func stageDeploymentConfig(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Deployment Configuration")

	log.Info("Llama-3.1-70B deployment plan")
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
	}, log.Style()))

	log.Newline()
}

// --- Secure config demo ---------------------------------------------------

// stageSecureConfig demonstrates the Secure field trust model. The console
// writer (TTY) shows plaintext; a JSON writer would redact. We also show
// <secure> tag scanning in the message string.
func stageSecureConfig(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Secure Configuration")

	// Secure("key", val) — plaintext on TTY, [REDACTED] on non-TTY / JSON.
	// This is the pattern for API keys, session tokens, and similar secrets
	// that operators need to see locally but must never reach a log aggregator.
	log.Info(
		"loading inference server config",
		velocity.Secure("api_key", "sk-live-7f3a9b2c4e1d8f60"),
		velocity.SecureURL("registry_dsn", "https://registry:s3cret@models.internal/v2"),
		velocity.String("model_path", "/mnt/models/llama-3.1-70b-awq"),
	)

	// <secure> tag scanning works in message strings — same TTY vs. non-TTY
	// divergence without needing a structured field.
	log.Info("mounted model checkpoint at <secure>/mnt/models/llama-3.1-70b-awq/shard-0</secure>")

	// Redacted is always hidden — not even trusted writers see the value.
	// Use it for fields you want present in the schema but never logged.
	log.Debug(
		"auth context attached",
		velocity.Redacted("bearer_token"),
		velocity.String("scope", "inference:read"),
	)

	log.Newline()
}

// --- Pre-flight checks ----------------------------------------------------

// stagePreflightChecks runs validation across all nodes. Node-3 fails the
// disk space check, foreshadowing the deployment failure later.
func stagePreflightChecks(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Pre-flight Checks")

	style := log.Style()
	okCell := style.Format(velocity.SlotStatusOK, "OK")
	warnCell := style.Format(velocity.SlotStatusWarn, "WARN")

	rows := [][]string{
		{"GPU Memory", "node-0", okCell, "79.8 GB free"},
		{"GPU Memory", "node-1", okCell, "79.8 GB free"},
		{"GPU Memory", "node-2", okCell, "79.8 GB free"},
		{"GPU Memory", "node-3", okCell, "79.8 GB free"},
		{"CUDA Version", "node-0", okCell, "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-1", okCell, "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-2", okCell, "12.4 / driver 550.54.15"},
		{"CUDA Version", "node-3", okCell, "12.4 / driver 550.54.15"},
		{"Disk Space", "node-0", okCell, "340 GB free"},
		{"Disk Space", "node-1", okCell, "280 GB free"},
		{"Disk Space", "node-2", okCell, "310 GB free"},
		{"Disk Space", "node-3", warnCell, "18 GB free (need 35 GB)"},
		{"Network", "node-0", okCell, "IB latency 1.2us"},
		{"Network", "node-1", okCell, "IB latency 1.1us"},
		{"Network", "node-2", okCell, "IB latency 1.3us"},
		{"Network", "node-3", okCell, "IB latency 1.2us"},
	}

	p.Table([]string{"Check", "Node", "Status", "Detail"}, rows)

	// Logger.Status uses StatusKind to produce a coloured badge in the console
	// and a structured "status" field in JSON — no raw ANSI needed at the call site.
	log.Status(
		velocity.LevelWarn, velocity.StatusWarn, "node-3 disk space critically low",
		velocity.String("node", "node-3"),
		velocity.String("available", "18 GB"),
		velocity.String("required", "35 GB"),
	)

	log.Newline()
}

// --- Route registration ---------------------------------------------------

// stageRouteRegistration shows Logger.Group for count-headed indented blocks.
// This is exactly the pattern used by olla's translator route registration.
func stageRouteRegistration(log *velocity.Logger) {
	log.Group(
		velocity.LevelInfo, "Registering inference API routes",
		velocity.GroupItem{Text: "POST /v1/chat/completions"},
		velocity.GroupItem{Text: "POST /v1/completions"},
		velocity.GroupItem{Text: "POST /v1/embeddings"},
		velocity.GroupItem{Text: "GET  /v1/models"},
		velocity.GroupItem{Text: "GET  /health"},
		velocity.GroupItem{Text: "GET  /metrics"},
	)

	log.Newline()

	// Continue places all lines under one timestamped INFO entry. OSC 8
	// hyperlinks are only emitted when stdout is a TTY that supports them;
	// plain URLs are used otherwise so no control sequences reach pipes.
	log.Continue(
		velocity.LevelInfo, "Inference server listening",
		"API:      "+link("http://10.0.1.10:8080/v1", "http://10.0.1.10:8080/v1"),
		"Metrics:  "+link("http://10.0.1.10:9090/metrics", "http://10.0.1.10:9090/metrics"),
		"Press Ctrl+C to stop",
	)

	log.Newline()
}

// --- Model distribution ---------------------------------------------------

// stageModelDistribution downloads model weights and builds inference containers.
func stageModelDistribution(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Model Distribution")

	distLog := log.With(velocity.String("stage", "distribute"))

	const weightBytes int64 = 35_000 // MB

	pb := live.NewProgressBar(os.Stdout, weightBytes, "Downloading model weights")

	var downloaded int64
	for downloaded < weightBytes {
		chunk := 700 + (downloaded/1000)%400
		downloaded += chunk
		if downloaded > weightBytes {
			downloaded = weightBytes
		}
		pb.Update(downloaded)
		time.Sleep(18 * time.Millisecond)
	}
	pb.Complete()

	distLog.Info(
		"model weights verified",
		velocity.Int64("size_mb", weightBytes),
		velocity.String("checksum", "sha256:a3f9...d12e"),
	)

	cb := live.NewProgressBar(os.Stdout, 15, "Building inference containers")
	layers := []string{
		"base: nvcr.io/nvidia/pytorch:24.01",
		"layer: vllm==0.4.2",
		"layer: transformers==4.40.0",
		"layer: model weights (quantised)",
		"layer: serving config",
	}

	var completedLayers []string
	for i, layer := range layers {
		isLastLayer := i == len(layers)-1
		for step := range 3 {
			cb.Increment(1)
			isLastStep := isLastLayer && step == 2
			if isLastStep {
				cb.Complete()
			} else {
				time.Sleep(120 * time.Millisecond)
			}
		}
		if !isLastLayer {
			completedLayers = append(completedLayers, layer)
		}
	}

	for i, layer := range completedLayers {
		distLog.Debug(
			"container layer complete",
			velocity.String("layer", layer),
			velocity.Int("index", i),
		)
	}

	distLog.Info("inference containers ready", velocity.String("image", "velocity/llama3-70b-awq:2.0.0"))
	log.Newline()
}

// --- Node deployment ------------------------------------------------------

// stageNodeDeployment pushes the model to each node. Returns the failed node
// name, or an empty string if all nodes succeeded.
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
		{name: "node-3", ip: "10.0.1.13", willFail: true},
	}

	var failed string

	for _, node := range nodes {
		spinner := live.NewSpinner(os.Stdout, fmt.Sprintf("Deploying to %s (%s)...", node.name, node.ip))
		time.Sleep(900 * time.Millisecond)

		if node.willFail {
			spinner.StopWithError(fmt.Sprintf("Deployment to %s failed", node.name))

			// StatusFail gives the operator an immediate visual signal without
			// the full tree layout of Detailed(). The fields still land in JSON.
			log.Status(
				velocity.LevelError, velocity.StatusFail, "container failed to start: insufficient disk space",
				velocity.String("node", node.name),
				velocity.String("error", "no space left on device"),
				velocity.String("disk_used", "93%"),
				velocity.String("disk_free", "18 GB"),
				velocity.String("required", "35 GB"),
			)

			failed = node.name
		} else {
			spinner.StopWithSuccess(node.name + " ready, inference endpoint active")

			// StatusOK produces a green badge on TTY; JSON gets status:"ok".
			log.Status(
				velocity.LevelInfo, velocity.StatusOK, node.name+" deployment successful",
				velocity.String("endpoint", "http://"+net.JoinHostPort(node.ip, "8080")+"/v1"),
				velocity.String("model", "llama-3.1-70b-awq"),
			)
		}
	}

	log.Newline()
	return failed
}

// --- Recovery -------------------------------------------------------------

func stageRecovery(log *velocity.Logger, p *velocity.Pretty, failedNode string) {
	if failedNode == "" {
		return
	}

	p.Section("Recovery")

	recoveryLog := log.With(
		velocity.String("failed_node", failedNode),
		velocity.String("stage", "recovery"),
	)

	log.Status(
		velocity.LevelWarn, velocity.StatusWarn, "initiating workload reallocation",
		velocity.String("from", failedNode),
		velocity.String("to", "node-0"),
		velocity.String("strategy", "single-node-overflow"),
	)

	spinner := live.NewSpinner(os.Stdout, fmt.Sprintf("Reallocating %s workload to node-0...", failedNode))
	time.Sleep(1400 * time.Millisecond)
	spinner.StopWithSuccess("Workload reallocated, node-0 running at 2x replicas")

	recoveryLog.Info(
		"reallocation complete",
		velocity.String("node_0_replicas", "2"),
		velocity.String("lb_config", "updated"),
	)

	log.Newline()
}

// --- Health verification --------------------------------------------------

func stageHealthVerification(log *velocity.Logger, p *velocity.Pretty) {
	p.Section("Health Verification")

	style := log.Style()
	healthyCell := style.Format(velocity.SlotStatusOK, "HEALTHY")
	relocatedCell := style.Format(velocity.SlotStatusInfo, "RELOCATED")
	failedCell := style.Format(velocity.SlotStatusFail, "FAILED")

	rows := [][]string{
		{"node-0", "llama-3.1-70b-awq", healthyCell, "38 ms", "http://10.0.1.10:8080/v1"},
		{"node-0*", "llama-3.1-70b-awq", relocatedCell, "41 ms", "http://10.0.1.10:8080/v1 (replica 2)"},
		{"node-1", "llama-3.1-70b-awq", healthyCell, "35 ms", "http://10.0.1.11:8080/v1"},
		{"node-2", "llama-3.1-70b-awq", healthyCell, "37 ms", "http://10.0.1.12:8080/v1"},
		{"node-3", "-", failedCell, "-", "disk full, out of service"},
	}

	p.Table([]string{"Node", "Model", "Status", "P50 Latency", "Endpoint"}, rows)

	// Status checklist gives the operator a quick scan-able summary of outcomes.
	log.Status(velocity.LevelInfo, velocity.StatusOK, "node-0 healthy", velocity.String("replicas", "2"))
	log.Status(velocity.LevelInfo, velocity.StatusOK, "node-1 healthy", velocity.String("replicas", "1"))
	log.Status(velocity.LevelInfo, velocity.StatusOK, "node-2 healthy", velocity.String("replicas", "1"))
	log.Status(velocity.LevelError, velocity.StatusFail, "node-3 out of service", velocity.String("reason", "disk full"))

	log.Newline()
}

// --- Summary --------------------------------------------------------------

// stageSummary prints the final deployment summary and ring buffer snapshot.
func stageSummary(log *velocity.Logger, p *velocity.Pretty, started time.Time, ring *velocity.RingBufferWriter) {
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

	log.Info(
		"deployment complete",
		velocity.Int("nodes_total", 4),
		velocity.Int("nodes_healthy", 3),
		velocity.Int("gpus_total", 16),
		velocity.Int("gpus_active", 12),
		velocity.String("model", "llama-3.1-70b-awq"),
		velocity.String("status", "degraded-operational"),
		velocity.Duration("elapsed", elapsed),
	)

	// NotifyBox goes to stderr (bypassing the structured pipeline) so the
	// operator sees it even when stdout is redirected to a log aggregator.
	// This is the alloy pattern: ephemeral operator messages that must not
	// get buried in log volume.
	dashURL := link("http://10.0.1.10:8080/v1/models", "http://10.0.1.10:8080/v1/models")
	log.NotifyBox(velocity.NewBox(
		"Deployment complete",
		fmt.Sprintf(
			"3/4 nodes operational. Inference stack is live.\n\n"+
				"  API: %s\n\n"+
				"Address node-3 disk space to restore full capacity.",
			dashURL,
		),
		log.Style(),
	))

	// Ring buffer snapshot — the last N entries the logger wrote. In a real
	// service this is served from an HTTP debug endpoint; here we print it so
	// the operator can see what was captured without re-reading stdout.
	snaps := ring.Snapshot(5)
	fmt.Printf("\n=== Ring buffer: last %d entries ===\n", len(snaps))
	for _, s := range snaps {
		fmt.Printf("  [%-5s] %s", s.Level, s.Message)
		for _, f := range s.Fields {
			fmt.Printf("  %s=%s", f.Key, f.Value)
		}
		fmt.Println()
	}

	stats := ring.Stats()
	fmt.Printf("Ring stats: capacity=%d fill=%d total=%d drops=%d\n",
		stats.Capacity, stats.Fill, stats.Total, stats.Drops)
}
