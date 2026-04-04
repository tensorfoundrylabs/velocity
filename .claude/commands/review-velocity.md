Launch an Opus agent to perform a comprehensive code review of the velocity logging library. Read EVERY non-test .go file. Run `make ready` first to confirm the build is clean, then run `go test -bench=. -benchmem -count=1 ./...` to get current benchmark numbers.

## Review checklist

Work through each section. For each finding, provide exact file:line and the triggering code path. If a section is clean, say so and move on. Do not manufacture findings.

### 1. Concurrency and thread safety

- Data races: any shared state accessed without synchronisation? Run through every struct field that could be written and read from different goroutines.
- Mutex correctness: are locks held for the minimum necessary duration? Is there any lock ordering that could deadlock?
- Atomic operations: correct memory ordering? Any load/store pairs that should be CAS?
- Channel safety: can any channel be double-closed? Are all sends non-blocking where they should be?
- Goroutine lifecycle: are all goroutines properly terminated on Close? Any leak paths?
- Entry ref counting: is Retain/Release balanced on every code path, including error paths?

### 2. Memory and allocations

- Hot path allocations: trace the path from `logger.Info("msg", fields...)` through to writer output. Flag any heap escape that could be eliminated.
- Pool correctness: are sync.Pool objects returned correctly? Any use-after-pool-return? Any pool objects that escape and are never returned?
- Buffer management: are pooled buffers sized correctly? Any unnecessary copies between buffers?
- String allocations: any string concatenation or fmt.Sprintf on hot paths that should use buffer writes?
- Slice growth: any append patterns that cause repeated growth? Should slices be pre-sized?

### 3. Correctness

- Nil safety: every public method handles nil receiver? Every pointer dereference guarded?
- Edge cases: empty strings, zero values, math.MinInt64, NaN/Infinity, typed nils, empty slices
- Error handling: are write errors handled consistently? Any silent data loss paths?
- Level filtering: is the level check correct for all paths (console, JSON, additional writers, slog handler)?
- Field rendering: are ALL FieldType values handled in EVERY switch statement? Cross-reference the enum against every switch.
- JSON output: valid JSON for all inputs? Proper escaping? Base64 correctness?

### 4. Performance

- Inlining: are hot-path functions within the inline budget (cost < 80)? Check `isEnabled`, `log`, field constructors.
- Writer contention: are mutexes held only during I/O, not during formatting?
- Ring buffer: is the CAS protocol correct? Bounded spins in both writer and flusher? Shutdown drains all data?
- ANSI caching: are theme colours pre-computed? Any per-entry ANSI code generation?
- Time formatting: using AppendFormat where possible? Any time.Format string allocations?
- Float formatting: using strconv.FormatFloat, not fmt.Sprintf?

### 5. API and integration

- slog.Handler: does it correctly implement the slog contract? Are WithAttrs/WithGroup immutable? Level mapping correct?
- Writer interface: is it simple and correct? Can external writers be added safely?
- Config validation: can invalid configs slip through and cause runtime panics?
- Child loggers (With/WithTemplate): do they correctly inherit all parent state?
- Context integration: thread-safe? Correct field accumulation?

### 6. What logging library users expect

- Structured output: JSON is valid, parseable, complete (timestamp, level, message, caller, fields)
- Caller info: when enabled, correct file:line in all output paths
- Sampling: works correctly, checked before allocation
- Graceful shutdown: Close drains all buffered entries, no data loss
- Dynamic level changes: SetLevel propagates correctly (note: child loggers snapshot)
- Thread safety under load: no panics, no corrupted output, no goroutine leaks under concurrent logging

### 7. Benchmark validation

Compare current benchmark numbers against these baseline expectations:
- Info with no fields: < 50 ns/op, 0 allocs
- Info with 5 pre-built fields: < 80 ns/op, 0 allocs
- Level check (disabled): < 5 ns/op, 0 allocs
- Entry pool round-trip: < 30 ns/op, 0 allocs
- JSONWriter 5 fields: < 1500 ns/op, <= 1 alloc
- ConsoleWriter 5 fields: < 1000 ns/op, <= 4 allocs
- Parallel Info: < 50 ns/op, 0 allocs

Flag any benchmark that has regressed beyond these thresholds.

## Output format

End with a summary table:

| Section | Status | Findings |
|---------|--------|----------|
| Concurrency | CLEAN / N issues | Brief description |
| Memory | CLEAN / N issues | Brief description |
| Correctness | CLEAN / N issues | Brief description |
| Performance | CLEAN / N issues | Brief description |
| API | CLEAN / N issues | Brief description |
| User expectations | CLEAN / N issues | Brief description |
| Benchmarks | PASS / REGRESSED | Details |
