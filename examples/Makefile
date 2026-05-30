.DEFAULT_GOAL := help

.PHONY: run-basic run-json run-themes run-custom-theme run-pretty run-tables run-progress run-slog run-multi-writer run-sampling run-terminal-velocity run-notify run-secure run-ring-buffer run-status-items run-groups run-continuation run-hyperlinks run-component-logging run-all build-all clean help

SEPARATOR = @echo "" && echo "============================================================" && echo ""

## run-basic: Run the basic getting-started example
run-basic:
	go run ./basic

## run-json: Run the structured JSON logging example
run-json:
	go run ./json-logging

## run-themes: Run the theme showcase example
run-themes:
	go run ./themes

## run-custom-theme: Run the custom theme (Cyberpunk) example
run-custom-theme:
	go run ./custom-theme

## run-pretty: Run the pretty-output showcase example
run-pretty:
	go run ./pretty-output

## run-tables: Run the table rendering example
run-tables:
	go run ./tables

## run-progress: Run the progress bars and spinners example
run-progress:
	go run ./progress

## run-slog: Run the slog bridge example
run-slog:
	go run ./slog-bridge

## run-multi-writer: Run the multi-writer fan-out example
run-multi-writer:
	go run ./multi-writer

## run-sampling: Run the log sampling example
run-sampling:
	go run ./sampling

## run-terminal-velocity: Run the hero GPU cluster deploy simulator
run-terminal-velocity:
	go run ./terminal-velocity

## run-notify: Run the Notify channel example
run-notify:
	go run ./notify

## run-secure: Run the field-level redaction and <secure> tag example
run-secure:
	go run ./secure

## run-ring-buffer: Run the in-process log capture example
run-ring-buffer:
	go run ./ring-buffer

## run-status-items: Run the StatusItem and Logger.Status example
run-status-items:
	go run ./status-items

## run-groups: Run the Group count-headed block example
run-groups:
	go run ./groups

## run-continuation: Run the ContinuationBlock example
run-continuation:
	go run ./continuation

## run-hyperlinks: Run the OSC 8 hyperlink example
run-hyperlinks:
	go run ./hyperlinks

## run-component-logging: Run the component-aware inline-indicator example
run-component-logging:
	go run ./component-logging

## run-all: Run all examples sequentially with a separator between each
run-all:
	@echo "Running all velocity examples..."
	@echo ""
	@echo "============================================================"
	@echo "  EXAMPLE 1: basic"
	@echo "============================================================"
	go run ./basic
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 2: json-logging"
	@echo "============================================================"
	go run ./json-logging
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 3: themes"
	@echo "============================================================"
	go run ./themes
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 4: custom-theme"
	@echo "============================================================"
	go run ./custom-theme
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 5: pretty-output"
	@echo "============================================================"
	go run ./pretty-output
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 6: tables"
	@echo "============================================================"
	go run ./tables
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 7: progress (interactive)"
	@echo "============================================================"
	go run ./progress
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 8: slog-bridge"
	@echo "============================================================"
	go run ./slog-bridge
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 9: multi-writer"
	@echo "============================================================"
	go run ./multi-writer
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 10: sampling"
	@echo "============================================================"
	go run ./sampling
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 11: terminal-velocity (interactive)"
	@echo "============================================================"
	go run ./terminal-velocity
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 12: notify"
	@echo "============================================================"
	go run ./notify
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 13: secure"
	@echo "============================================================"
	go run ./secure
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 14: ring-buffer"
	@echo "============================================================"
	go run ./ring-buffer
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 15: status-items"
	@echo "============================================================"
	go run ./status-items
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 16: groups"
	@echo "============================================================"
	go run ./groups
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 17: continuation"
	@echo "============================================================"
	go run ./continuation
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 18: hyperlinks"
	@echo "============================================================"
	go run ./hyperlinks
	$(SEPARATOR)
	@echo "============================================================"
	@echo "  EXAMPLE 19: component-logging"
	@echo "============================================================"
	go run ./component-logging

## build-all: Compile all examples to binaries in examples/bin/
build-all:
	go build -o bin/basic              ./basic
	go build -o bin/json-logging       ./json-logging
	go build -o bin/themes             ./themes
	go build -o bin/custom-theme       ./custom-theme
	go build -o bin/pretty-output      ./pretty-output
	go build -o bin/tables             ./tables
	go build -o bin/progress           ./progress
	go build -o bin/slog-bridge        ./slog-bridge
	go build -o bin/multi-writer       ./multi-writer
	go build -o bin/sampling           ./sampling
	go build -o bin/terminal-velocity  ./terminal-velocity
	go build -o bin/notify             ./notify
	go build -o bin/secure             ./secure
	go build -o bin/ring-buffer        ./ring-buffer
	go build -o bin/status-items       ./status-items
	go build -o bin/groups             ./groups
	go build -o bin/continuation       ./continuation
	go build -o bin/hyperlinks          ./hyperlinks
	go build -o bin/component-logging  ./component-logging

## clean: Remove compiled example binaries
clean:
	go clean ./basic ./json-logging ./themes ./custom-theme ./pretty-output \
		./tables ./progress ./slog-bridge ./multi-writer ./sampling \
		./terminal-velocity ./notify ./secure ./ring-buffer ./status-items \
		./groups ./continuation ./hyperlinks ./component-logging
	-rm -rf bin

## help: Show this help message
help:
	@echo "Available targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
