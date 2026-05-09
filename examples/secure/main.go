// Secure example demonstrates field-level redaction and <secure> tag scanning.
//
// Run on a TTY:
//
//	go run ./examples/secure
//
// Pipe through cat to simulate a non-TTY (redacts automatically):
//
//	go run ./examples/secure | cat
package main

import (
	"fmt"
	"os"

	velocity "github.com/tensorfoundrylabs/velocity"
)

func main() {
	// ---- setup ---------------------------------------------------------------
	// One JSON log file (always untrusted — secure fields are redacted there).
	jsonFile, err := os.CreateTemp("", "velocity-secure-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "temp file: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(jsonFile.Name()) }()
	defer func() { _ = jsonFile.Close() }()

	// Console logger. TTY gets plaintext; pipe/file gets redacted automatically.
	log := velocity.New(velocity.WithDevelopment())
	log.AddWriter("json-file", velocity.NewJSONWriter(jsonFile))

	// A second JSON writer opted into trust — receives plaintext for audit log.
	auditFile, _ := os.CreateTemp("", "velocity-audit-*.json")
	defer func() { _ = os.Remove(auditFile.Name()) }()
	defer func() { _ = auditFile.Close() }()
	log.AddWriter("audit", velocity.NewJSONWriter(auditFile), velocity.WriterTrusted())

	fmt.Println("--- Secure field constructors ---")

	// Secure(key, value): shows plaintext on TTY console, [REDACTED] in JSON log.
	log.Info("user authenticated",
		velocity.Secure("session_token", "tok_abc123def456"),
	)

	// SecureURL(key, url): redacts the password portion of the URL everywhere
	// except trusted writers. The host and path are preserved in the redacted form.
	log.Info("database connected",
		velocity.SecureURL("dsn", "postgres://app:s3cretP4ss@db.internal:5432/mydb"),
	)

	// Redacted(key): permanently hidden — no plaintext stored, not even on trusted writers.
	log.Info("request received",
		velocity.Redacted("api_key"),
	)

	// Truncated(key, val, maxLen): shows a safe prefix, appends '…' when clipped.
	// Useful for bearer tokens where the prefix identifies the token type.
	log.Info("bearer presented",
		velocity.Truncated("token", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig", 20),
	)

	fmt.Println()
	fmt.Println("--- <secure> tag in message ---")

	// The <secure>...</secure> tag form works for unstructured message strings.
	// On TTY console: markers stripped, content shown.
	// On non-TTY / JSON: markers and content replaced with [REDACTED].
	log.Info("connecting to cache at <secure>redis://admin:hunter2@cache.internal:6379</secure>")
	log.Warn("auth failed for <secure>user@domain.com</secure>; locking account")

	fmt.Println()
	fmt.Println("--- Multi-writer trust divergence ---")

	// Mixed trusted + untrusted writers on the same log call.
	log.Info("session created",
		velocity.Secure("session", "sess_XyZ789"),
		velocity.String("user_id", "u_42"),
	)

	_ = log.Close()

	// ---- print JSON outputs for visual inspection ----------------------------
	fmt.Println()
	fmt.Println("--- JSON log (untrusted, should be redacted) ---")
	printFile(jsonFile)

	fmt.Println()
	fmt.Println("--- Audit log (trusted, should show plaintext) ---")
	printFile(auditFile)
}

func printFile(f *os.File) {
	if _, err := f.Seek(0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "seek: %v\n", err)
		return
	}
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	_, _ = os.Stdout.Write(buf[:n])
}
