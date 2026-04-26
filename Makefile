APP     := ./build/bin/Aiko.app
BINARY  := $(APP)/Contents/MacOS/Aiko

.PHONY: build run dev clean

## build: compile and ad-hoc sign Aiko.app (keeps TCC permissions across rebuilds)
build:
	wails build
	codesign --force --deep --sign "-" $(APP)
	@echo "✅ Build complete: $(APP)"

## run: build then launch
run: build
	open $(APP)

## dev: start wails dev server (hot-reload, no signing needed)
dev:
	wails dev

## clean: remove build artifacts
clean:
	rm -rf ./build/bin

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
