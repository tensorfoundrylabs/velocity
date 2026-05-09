package live

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressBar displays progress for long-running operations.
// Thread-safe for concurrent updates while logging continues.
type ProgressBar struct {
	started  time.Time
	lastDraw time.Time
	writer   io.Writer
	done     chan struct{}
	label    string
	total    int64
	current  int64
	width    int
	mu       sync.Mutex
	active   atomic.Bool
	noOp     bool
}

func NewProgressBar(w io.Writer, total int64, label string) *ProgressBar {
	if w == nil {
		return &ProgressBar{
			noOp:  true,
			total: total,
			done:  make(chan struct{}),
		}
	}

	if total < 0 {
		total = 0
	}

	pb := &ProgressBar{
		writer:  w,
		total:   total,
		label:   label,
		width:   40,
		started: time.Now(),
		done:    make(chan struct{}),
	}
	pb.active.Store(true)

	go pb.renderLoop()

	return pb
}

func (pb *ProgressBar) Update(current int64) {
	if pb == nil || pb.noOp {
		return
	}

	if current < 0 {
		current = 0
	}
	if current > pb.total {
		current = pb.total
	}

	pb.mu.Lock()
	pb.current = current
	pb.mu.Unlock()
}

func (pb *ProgressBar) Increment(delta int64) {
	if pb == nil || pb.noOp {
		return
	}

	pb.mu.Lock()
	pb.current += delta
	if pb.current < 0 {
		pb.current = 0
	}
	if pb.current > pb.total {
		pb.current = pb.total
	}
	pb.mu.Unlock()
}

func (pb *ProgressBar) SetLabel(label string) {
	if pb == nil || pb.noOp {
		return
	}

	pb.mu.Lock()
	pb.label = label
	pb.mu.Unlock()
}

func (pb *ProgressBar) Complete() {
	if pb == nil || pb.noOp {
		return
	}

	// CAS ensures only one caller closes done, preventing double-close panic.
	if !pb.active.CompareAndSwap(true, false) {
		return
	}

	pb.mu.Lock()
	pb.current = pb.total
	pb.mu.Unlock()

	pb.render()

	close(pb.done)

	if pb.writer != nil {
		_, _ = fmt.Fprintln(pb.writer)
	}
}

func (pb *ProgressBar) render() {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	var percent float64
	if pb.total > 0 {
		percent = float64(pb.current) / float64(pb.total) * 100
	}

	filled := min(int(float64(pb.width)*percent/100), pb.width)

	elapsed := time.Since(pb.started)

	var eta time.Duration
	if pb.current > 0 && pb.current < pb.total {
		rate := float64(pb.current) / elapsed.Seconds()
		remaining := float64(pb.total - pb.current)
		eta = time.Duration(remaining/rate) * time.Second
	}

	bar := strings.Builder{}

	bar.WriteString("\r\033[K")

	if pb.label != "" {
		bar.WriteString(pb.label)
		bar.WriteString(" ")
	}

	bar.WriteString("[")
	for i := range pb.width {
		if i < filled {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("] ")

	fmt.Fprintf(&bar, "%3.0f%% ", percent)

	fmt.Fprintf(&bar, "(%d/%d) ", pb.current, pb.total)

	if pb.current >= pb.total {
		fmt.Fprintf(&bar, "completed in %s", formatDuration(elapsed))
	} else if eta > 0 {
		fmt.Fprintf(&bar, "ETA: %s", formatDuration(eta))
	}

	_, _ = fmt.Fprint(pb.writer, bar.String())
	pb.lastDraw = time.Now()
}

func (pb *ProgressBar) renderLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pb.done:
			return
		case <-ticker.C:
			if pb.active.Load() {
				pb.render()
			}
		}
	}
}

// Spinner displays an animated spinner for indeterminate progress.
// Thread-safe and works concurrently with logging.
type Spinner struct {
	writer  io.Writer
	done    chan struct{}
	label   string
	frames  []string
	current int
	mu      sync.Mutex
	active  atomic.Bool
	noOp    bool
}

func NewSpinner(w io.Writer, label string) *Spinner {
	if w == nil {
		return &Spinner{
			noOp: true,
			done: make(chan struct{}),
		}
	}

	s := &Spinner{
		writer: w,
		label:  label,
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		done:   make(chan struct{}),
	}
	s.active.Store(true)

	s.start()

	return s
}

func (s *Spinner) SetLabel(label string) {
	if s == nil || s.noOp {
		return
	}

	s.mu.Lock()
	s.label = label
	s.mu.Unlock()
}

func (s *Spinner) Stop() {
	if s == nil || s.noOp {
		return
	}

	// CAS ensures only one caller closes done, preventing double-close panic.
	if !s.active.CompareAndSwap(true, false) {
		return
	}

	close(s.done)

	if s.writer != nil {
		_, _ = fmt.Fprint(s.writer, "\r\033[K")
	}
}

func (s *Spinner) StopWithMessage(message string) {
	if s == nil {
		fmt.Println(message)
		return
	}

	if s.noOp {
		return
	}

	s.Stop()
	if s.writer != nil {
		_, _ = fmt.Fprintln(s.writer, message)
	}
}

func (s *Spinner) StopWithSuccess(message string) {
	if s == nil {
		fmt.Printf("✅ %s\n", message)
		return
	}

	if s.noOp {
		return
	}

	s.Stop()
	if s.writer != nil {
		_, _ = fmt.Fprintf(s.writer, "✅ %s\n", message)
	}
}

func (s *Spinner) StopWithError(message string) {
	if s == nil {
		fmt.Printf("❌ %s\n", message)
		return
	}

	if s.noOp {
		return
	}

	s.Stop()
	if s.writer != nil {
		_, _ = fmt.Fprintf(s.writer, "❌ %s\n", message)
	}
}

func (s *Spinner) render() {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, _ = fmt.Fprint(s.writer, "\r\033[K")
	_, _ = fmt.Fprintf(s.writer, "%s %s", s.frames[s.current], s.label)

	s.current = (s.current + 1) % len(s.frames)
}

func (s *Spinner) start() {
	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		s.render()

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				if s.active.Load() {
					s.render()
				}
			}
		}
	}()
}

// SpinnerStyle selects the animation frame set.
type SpinnerStyle int

const (
	SpinnerStyleBraille SpinnerStyle = iota
	SpinnerStyleDots
	SpinnerStyleArrows
	SpinnerStyleBounce
	SpinnerStyleBar
)

func (s *Spinner) SetStyle(style SpinnerStyle) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch style {
	case SpinnerStyleBraille:
		s.frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	case SpinnerStyleDots:
		s.frames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	case SpinnerStyleArrows:
		s.frames = []string{"←", "↖", "↑", "↗", "→", "↘", "↓", "↙"}
	case SpinnerStyleBounce:
		s.frames = []string{"⠁", "⠂", "⠄", "⡀", "⢀", "⠠", "⠐", "⠈"}
	case SpinnerStyleBar:
		s.frames = []string{"|", "/", "-", "\\"}
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour //nolint:durationcheck // h is already a time.Duration from division
	m := d / time.Minute
	d -= m * time.Minute //nolint:durationcheck // m is already a time.Duration from division
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// MultiProgress manages multiple progress bars or spinners simultaneously.
type MultiProgress struct {
	lastDraw time.Time
	writer   io.Writer
	done     chan struct{}
	items    []ProgressItem
	mu       sync.Mutex
	active   atomic.Bool
}

// ProgressItem is implemented by types that can render themselves as a progress line.
type ProgressItem interface {
	Render() string
}

func NewMultiProgress(w io.Writer) *MultiProgress {
	mp := &MultiProgress{
		writer: w,
		items:  make([]ProgressItem, 0),
		done:   make(chan struct{}),
	}
	mp.active.Store(true)

	go mp.renderLoop()

	return mp
}

func (mp *MultiProgress) Add(item ProgressItem) {
	mp.mu.Lock()
	mp.items = append(mp.items, item)
	mp.mu.Unlock()
}

func (mp *MultiProgress) Remove(item ProgressItem) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for i, it := range mp.items {
		if it == item {
			mp.items = append(mp.items[:i], mp.items[i+1:]...)
			break
		}
	}
}

func (mp *MultiProgress) Stop() {
	// CAS ensures only one caller closes done, preventing double-close panic.
	if !mp.active.CompareAndSwap(true, false) {
		return
	}

	close(mp.done)

	mp.mu.Lock()
	for range mp.items {
		_, _ = fmt.Fprint(mp.writer, "\r\033[K\n")
	}
	mp.mu.Unlock()
}

func (mp *MultiProgress) render() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if len(mp.items) == 0 {
		return
	}

	// Move cursor to start of progress area for in-place updates.
	if mp.lastDraw.After(time.Time{}) {
		_, _ = fmt.Fprintf(mp.writer, "\033[%dA", len(mp.items))
	}

	for _, item := range mp.items {
		_, _ = fmt.Fprint(mp.writer, "\r\033[K")
		_, _ = fmt.Fprintln(mp.writer, item.Render())
	}

	mp.lastDraw = time.Now()
}

func (mp *MultiProgress) renderLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-mp.done:
			return
		case <-ticker.C:
			if mp.active.Load() {
				mp.render()
			}
		}
	}
}
