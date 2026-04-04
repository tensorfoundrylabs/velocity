# Velocity Examples

Runnable examples demonstrating velocity's features. Each example is a standalone `main.go` you can read, run, and modify.

## Running

From this directory:

```bash
make run-basic              # or any target below
make run-all                # runs all examples sequentially
make help                   # list all targets
```

Or from the repo root:

```bash
go run ./examples/basic
go run ./examples/terminal-velocity
```

## Examples

### Getting Started

| Example | What it shows | Run |
|---------|--------------|-----|
| [basic](basic/) | Logger creation, log levels, typed fields, child loggers, `SetLevel`, `InfoDetailed`, presets | `make run-basic` |
| [json-logging](json-logging/) | Dual output (console + JSON file), `WithCaller`, custom time format, structured JSON | `make run-json` |
| [themes](themes/) | All four colour themes (Night Owl, Solarized, Dracula, Nord) with the same log entries | `make run-themes` |

### Output and Display

| Example | What it shows | Run |
|---------|--------------|-----|
| [pretty-output](pretty-output/) | Section, Box, Panel, Banner, Bullet, KeyValue, Table, Tree, SystemInfo, StatusFormatter | `make run-pretty` |
| [tables](tables/) | Table rendering with headers, rows, and ANSI-coloured cells (StatusFormatter) | `make run-tables` |
| [progress](progress/) | ProgressBar, all 5 Spinner styles, label changes, success/error stop messages | `make run-progress` |

### Integration

| Example | What it shows | Run |
|---------|--------------|-----|
| [slog-bridge](slog-bridge/) | `NewSlogLogger`, `slog.SetDefault`, `WithGroup`, `WithAttrs`, level filtering | `make run-slog` |
| [multi-writer](multi-writer/) | `AddWriter`/`RemoveWriter`, `FilteredWriter`, `WriterFunc` adapter | `make run-multi-writer` |
| [sampling](sampling/) | `CountSampler` for high-volume log reduction, before/after stats | `make run-sampling` |

### Showcase

| Example | What it shows | Run |
|---------|--------------|-----|
| [terminal-velocity](terminal-velocity/) | Full GPU cluster deployment simulator using banner, spinners, progress bars, trees, tables, child loggers, structured fields, error recovery | `make run-terminal-velocity` |

## Building

```bash
make build-all    # compiles all examples to examples/bin/
make clean        # removes compiled binaries
```
