MODEL_NAME := vits-icefall-zh-aishell3
MODEL_URL  := https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/$(MODEL_NAME).tar.bz2
VENDOR_DIR := vendor-sherpa

.PHONY: deps deps-model deps-check build dev

## deps: 下载 TTS 模型（首次使用前执行一次）
deps: deps-model

deps-model:
	@mkdir -p $(VENDOR_DIR)/model
	@if [ ! -f $(VENDOR_DIR)/model/model.onnx ]; then \
	    echo "正在下载 $(MODEL_NAME)..."; \
	    curl -SL -o /tmp/$(MODEL_NAME).tar.bz2 $(MODEL_URL); \
	    tar -xjf /tmp/$(MODEL_NAME).tar.bz2 -C /tmp; \
	    cp -r /tmp/$(MODEL_NAME)/. $(VENDOR_DIR)/model/; \
	    rm /tmp/$(MODEL_NAME).tar.bz2; \
	    echo "模型已下载到 $(VENDOR_DIR)/model/"; \
	else \
	    echo "模型已存在，跳过下载。"; \
	fi

deps-check:
	@test -f $(VENDOR_DIR)/model/model.onnx || (echo "错误：请先执行 make deps" && exit 1)
	@echo "依赖检查通过。"

## build: 构建 .app 并打包模型
build: deps-check
	wails build
	./scripts/bundle-sherpa.sh build/bin/Aiko.app

## dev: 启动开发模式（模型从 vendor-sherpa/model 直接读取）
dev: deps-check
	wails dev
