package velocity

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakeFmtCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fmt-check recipe test requires a POSIX shell")
	}

	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is unavailable")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX shell is unavailable")
	}

	tests := []struct {
		name       string
		goimports  string
		gofumpt    string
		wantOK     bool
		wantOutput string
	}{
		{
			name:      "goimports error",
			goimports: "exit 23",
			gofumpt:   "exit 0",
		},
		{
			name:      "gofumpt error",
			goimports: "exit 0",
			gofumpt:   "exit 29",
		},
		{
			name:       "formatting drift",
			goimports:  "printf '%s\\n' drift.go\nexit 0",
			gofumpt:    "exit 0",
			wantOutput: "goimports would rewrite:",
		},
		{
			name:       "clean",
			goimports:  "exit 0",
			gofumpt:    "exit 0",
			wantOK:     true,
			wantOutput: "Formatting clean.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makefile := writeFmtCheckMakefile(t, dir)
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			writeTool(t, bin, "goimports", tt.goimports)
			writeTool(t, bin, "gofumpt", tt.gofumpt)
			writeTool(t, bin, "go", "exit 0")

			cmd := exec.CommandContext(t.Context(), makePath, "-f", makefile, "fmt-check") //nolint:gosec // G204: makePath is resolved with LookPath; arguments are test-controlled paths
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if (err == nil) != tt.wantOK {
				t.Fatalf("fmt-check error = %v, want success %t; output:\n%s", err, tt.wantOK, output)
			}
			if tt.wantOutput != "" && !strings.Contains(string(output), tt.wantOutput) {
				t.Fatalf("fmt-check output does not contain %q:\n%s", tt.wantOutput, output)
			}
			if !tt.wantOK && strings.Contains(string(output), "Formatting clean.") {
				t.Fatalf("failed fmt-check reported success:\n%s", output)
			}
		})
	}
}

func writeFmtCheckMakefile(t *testing.T, dir string) string {
	t.Helper()

	source, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	// The test runs the production recipe, without verify-tools, so formatter
	// failures are tested independently of version and installation checks.
	makefile := strings.Replace(string(source), "fmt-check: verify-tools", "fmt-check:", 1)
	if makefile == string(source) {
		t.Fatal("fmt-check prerequisite not found in Makefile")
	}

	path := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(path, []byte(makefile), 0o600); err != nil { //nolint:gosec // G703: path is built under t.TempDir()
		t.Fatal(err)
	}
	return path
}

func writeTool(t *testing.T, bin, name, body string) {
	t.Helper()

	path := filepath.Join(bin, name)
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // G306: temporary test tool must be executable
		t.Fatal(err)
	}
}
