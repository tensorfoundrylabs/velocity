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

### Foundation

| Example | What it shows | Run |
|---------|--------------|-----|
| [basic](basic/) | Logger creation, log levels, typed fields, child loggers, `SetLevel`, `Detailed()`, `WithDevelopment()` | `make run-basic` |
| [themes](themes/) | All five built-in themes (Night Owl, Solarized, Dracula, Nord, Mono) with `Theme.Format` and `Theme.Wrap` for semantic style slots | `make run-themes` |
| [custom-theme](custom-theme/) | Define your own colour palette with `NewTheme` + `ThemeOption`, custom `StyleSlot` values, theme flowing through loggers, tables, and status output | `make run-custom-theme` |

### Output Formats

| Example | What it shows | Run |
|---------|--------------|-----|
| [json-logging](json-logging/) | Dual output: coloured console for humans, newline-delimited JSON for aggregators; `WithCaller`, custom time format | `make run-json` |
| [multi-writer](multi-writer/) | `AddWriter`/`RemoveWriter`, `FilteredWriter`, `WriterFunc`, `WriterTrusted()` for the Phase 4 trust model | `make run-multi-writer` |

### Pretty Output

| Example | What it shows | Run |
|---------|--------------|-----|
| [pretty-output](pretty-output/) | Section, Box, Panel, Banner, Bullet, KeyValue, Table, Tree, SystemInfo via the `Pretty` facade | `make run-pretty` |
| [tables](tables/) | `NewTable` with ANSI-coloured cells, `log.Table()` convenience for indented rendering, auto-sized columns | `make run-tables` |
| [progress](progress/) | `live.NewProgressBar`, all five `SpinnerStyle` variants, label changes, success and error stop messages | `make run-progress` |
| [terminal-velocity](terminal-velocity/) | Hero example: GPU cluster deployment simulator using banners, spinners, progress bars, trees, tables, child loggers, error recovery | `make run-terminal-velocity` |

### Structured Features

| Example | What it shows | Run |
|---------|--------------|-----|
| [sampling](sampling/) | `CountSampler` for high-volume log reduction; first-N pass-through then every-Mth sampling | `make run-sampling` |
| [slog-bridge](slog-bridge/) | `slogbridge.NewLogger`, `slog.SetDefault`, `WithGroup`, level filtering; incremental adoption path | `make run-slog` |

### v2 New

| Example | What it shows | Run |
|---------|--------------|-----|
| [notify](notify/) | `Logger.Notify`, `NotifyLines`, `NotifyBox` for ephemeral operator output bypassing the structured pipeline | `make run-notify` |
| [secure](secure/) | `Secure`, `SecureURL`, `Redacted`, `Truncated` field constructors; `<secure>` tag scanning; TTY vs JSON divergence; trusted writers for audit logs | `make run-secure` |
| [ring-buffer](ring-buffer/) | `RingBufferWriter` for in-process log capture: `Snapshot` (HTTP debug endpoint pattern) and `Subscribe` (live tail pattern) | `make run-ring-buffer` |
| [status-items](status-items/) | `Logger.Status` with all six `StatusKind` values (`OK`, `Fail`, `Warn`, `Info`, `Pending`, `Skipped`); standalone `NewStatusItem` with `log.Render` | `make run-status-items` |
| [groups](groups/) | `Logger.Group` for count-headed indented blocks; explicit markers; empty group | `make run-groups` |
| [continuation](continuation/) | `Logger.Continue` for multi-line output anchored to one structured entry; hyperlinks inside continuation lines | `make run-continuation` |
| [hyperlinks](hyperlinks/) | `Hyperlink` OSC 8 helper; `HyperlinksSupported` detection; all three fallback modes; composing with `Theme.Format`; embedding in Box, Table, and ContinuationBlock | `make run-hyperlinks` |

## Building

```bash
make build-all    # compiles all examples to examples/bin/
make clean        # removes compiled binaries
```
