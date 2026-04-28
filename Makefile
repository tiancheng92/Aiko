APP     := $(shell pwd)/build/bin/Aiko.app
BINARY  := $(APP)/Contents/MacOS/Aiko

.PHONY: build run dev clean

## build: compile and sign Aiko.app with local self-signed cert (stable csreq = persistent TCC permissions)
build:
	wails build -trimpath -ldflags="-s -w"
	codesign --force --deep --sign "Aiko" --identifier "com.xutiancheng.aiko" $(APP)
	rsync -a --delete $(APP)/ /Applications/Aiko.app/
	xattr -cr /Applications/Aiko.app
	@echo "✅ Build complete: $(APP)"

## run: build, install to /Applications, then launch
run: build
	open /Applications/Aiko.app

## dev: start wails dev server (hot-reload, no signing needed)
dev:
	wails dev

## clean: remove build artifacts
clean:
	rm -rf ./build/bin

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
