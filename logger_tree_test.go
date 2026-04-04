package velocity

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTree_WithIndent(t *testing.T) {
	// Calculate the expected indentation dynamically based on the template
	// Use UTC to ensure consistent timestamp length across all environments
	calculateIndent := func() string {
		template := TemplateDefault
		entry := GetEntry()
		defer entry.Release()
		// Use a fixed UTC time to ensure consistent timestamp formatting
		entry.SetTime(time.Date(2025, 12, 19, 10, 0, 0, 0, time.UTC))
		entry.SetLevel(LevelInfo)

		prefixWidth := template.CalculatePrefixWidth(entry)
		return strings.Repeat(" ", prefixWidth)
	}

	indent := calculateIndent()

	tests := []struct {
		name      string
		label     string
		items     []TreeItem
		useIndent bool
		expected  string
	}{
		{
			name:  "without indentation - label with timestamp",
			label: "Routes",
			items: []TreeItem{
				{Key: "API", Value: "/v1"},
				{Key: "Health", Value: "/health"},
			},
			useIndent: false,
			expected:  "[INFO] Routes\n├─ API: /v1\n└─ Health: /health\n",
		},
		{
			name:  "with indentation - label with timestamp",
			label: "Routes",
			items: []TreeItem{
				{Key: "API", Value: "/v1"},
				{Key: "Health", Value: "/health"},
			},
			useIndent: true,
			expected:  "[INFO] Routes\n" + indent + "├─ API: /v1\n" + indent + "└─ Health: /health\n",
		},
		{
			name:  "nested items with indentation",
			label: "Services",
			items: []TreeItem{
				{
					Key:   "API",
					Value: "v1",
					Children: []TreeItem{
						{Key: "Auth", Value: "enabled"},
						{Key: "Rate Limit", Value: "1000 req/s"},
					},
				},
				{Key: "Health", Value: "ok"},
			},
			useIndent: true,
			expected:  "[INFO] Services\n" + indent + "├─ API: v1\n" + indent + "│   ├─ Auth: enabled\n" + indent + "│   └─ Rate Limit: 1000 req/s\n" + indent + "└─ Health: ok\n",
		},
		{
			name:  "nil logger",
			label: "Test",
			items: []TreeItem{
				{Key: "Item", Value: "value"},
			},
			useIndent: true,
			expected:  "[INFO] Test\n" + indent + "└─ Item: value\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a pipe so we get an os.File which enables ConsoleWriter creation
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			defer func() { _ = w.Close() }()

			// Read output in background
			var buf bytes.Buffer
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = io.Copy(&buf, r)
			}()

			// Create logger with UTC timezone for consistent timestamp formatting
			logger := NewWithConfig(&Config{
				ConsoleOutput:   w,
				ConsoleLevel:    LevelInfo,
				ConsoleTheme:    nil, // Disable colors for consistent output
				DisplayTimezone: time.UTC,
			})

			// Call TreeWithIndent
			logger.TreeWithIndent(tt.label, tt.items, tt.useIndent)

			// Close writer and wait for reader to finish
			_ = w.Close()
			<-done

			// Check output
			got := buf.String()

			// Strip timestamp (first 21 chars: "2025-12-19T05:05:36Z ") from each line for comparison
			// This makes the test timezone-independent
			gotLines := strings.Split(got, "\n")
			expectedLines := strings.Split(tt.expected, "\n")

			if len(gotLines) != len(expectedLines) {
				t.Errorf("TreeWithIndent() line count mismatch\nGot %d lines:\n%s\nExpected %d lines:\n%s",
					len(gotLines), got, len(expectedLines), tt.expected)
				return
			}

			for i, gotLine := range gotLines {
				expectedLine := expectedLines[i]

				// For non-empty lines, strip the timestamp prefix (first 21 chars)
				if len(gotLine) >= 21 && strings.Contains(gotLine, " [INFO] ") {
					gotLine = gotLine[21:] // Skip "2025-12-19T05:05:36Z "
				}

				if gotLine != expectedLine {
					t.Errorf("TreeWithIndent() line %d mismatch\nGot:      %q\nExpected: %q\nFull output:\n%s",
						i, gotLine, expectedLine, got)
				}
			}
		})
	}
}

func TestTree_BackwardCompatibility(t *testing.T) {
	// Calculate the expected indentation dynamically
	// Use UTC to ensure consistent timestamp length across all environments
	template := TemplateDefault
	entry := GetEntry()
	defer entry.Release()
	// Use a fixed UTC time to ensure consistent timestamp formatting
	entry.SetTime(time.Date(2025, 12, 19, 10, 0, 0, 0, time.UTC))
	entry.SetLevel(LevelInfo)

	prefixWidth := template.CalculatePrefixWidth(entry)
	expectedIndent := strings.Repeat(" ", prefixWidth) + "├─"

	// Create a pipe so we get an os.File which enables ConsoleWriter creation
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	// Read output in background
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, r)
	}()

	logger := NewWithConfig(&Config{
		ConsoleOutput:   w,
		ConsoleLevel:    LevelInfo,
		ConsoleTheme:    nil, // Disable colors for consistent output
		DisplayTimezone: time.UTC,
	})

	items := []TreeItem{
		{Key: "API", Value: "/v1"},
		{Key: "Health", Value: "/health"},
	}

	// Call Tree() which should internally call TreeWithIndent with indent=true
	logger.Tree("Routes", items)

	// Close writer and wait for reader to finish
	_ = w.Close()
	<-done

	// Should have indentation (spaces) by default
	got := buf.String()
	if !strings.Contains(got, expectedIndent) {
		t.Errorf("Tree() should use indentation by default for backward compatibility\nExpected to contain: %q\nGot:\n%s", expectedIndent, got)
	}

	// Should have the label printed
	if !strings.Contains(got, "Routes") {
		t.Errorf("Tree() should print the label\nGot:\n%s", got)
	}
}

func TestTree_WithNilLogger(_ *testing.T) {
	// Test with nil logger - should use stdout

	items := []TreeItem{
		{Key: "Test", Value: "value"},
	}

	// This should not panic
	var nilLogger *Logger
	nilLogger.TreeWithIndent("Label", items, true)
}
