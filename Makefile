APP     := $(shell pwd)/build/bin/Aiko.app
BINARY  := $(APP)/Contents/MacOS/Aiko
VERSION := $(shell grep '"productVersion"' wails.json | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
DMG     := $(shell pwd)/build/bin/Aiko-$(VERSION).dmg

.PHONY: build run dev clean dmg

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

## dmg: build, sign, then package into a distributable DMG (requires: brew install create-dmg)
dmg: build
	create-dmg \
	  --volname "Aiko $(VERSION)" \
	  --window-pos 200 120 \
	  --window-size 600 400 \
	  --icon-size 128 \
	  --icon "Aiko.app" 150 200 \
	  --hide-extension "Aiko.app" \
	  --app-drop-link 450 200 \
	  "$(DMG)" \
	  "$(APP)"
	@echo "✅ DMG: $(DMG)"

## bump-patch: increment patch version (1.0.0 → 1.0.1), commit and tag
bump-patch:
	@$(MAKE) _bump PART=patch

## bump-minor: increment minor version (1.0.1 → 1.1.0), commit and tag
bump-minor:
	@$(MAKE) _bump PART=minor

## bump-major: increment major version (1.1.0 → 2.0.0), commit and tag
bump-major:
	@$(MAKE) _bump PART=major

_bump:
	@OLD="$(VERSION)"; \
	MAJOR=$$(echo $$OLD | cut -d. -f1); \
	MINOR=$$(echo $$OLD | cut -d. -f2); \
	PATCH=$$(echo $$OLD | cut -d. -f3); \
	if [ "$(PART)" = "major" ]; then MAJOR=$$((MAJOR+1)); MINOR=0; PATCH=0; \
	elif [ "$(PART)" = "minor" ]; then MINOR=$$((MINOR+1)); PATCH=0; \
	else PATCH=$$((PATCH+1)); fi; \
	NEW="$$MAJOR.$$MINOR.$$PATCH"; \
	sed -i '' "s/\"productVersion\": \"$$OLD\"/\"productVersion\": \"$$NEW\"/" wails.json; \
	git add wails.json; \
	git commit -m "chore: bump version $$OLD → $$NEW"; \
	git tag "v$$NEW"; \
	echo "✅ $$OLD → $$NEW (tag v$$NEW)"; \
	echo "   push with: git push origin main v$$NEW"

## clean: remove build artifacts
clean:
	rm -rf ./build/bin

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
