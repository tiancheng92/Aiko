#!/usr/bin/env bash
# bundle-sherpa.sh — 将 sherpa TTS 模型复制到 .app bundle。
# 用法：./scripts/bundle-sherpa.sh [path/to/Aiko.app]
set -e

APP="${1:-build/bin/Aiko.app}"
MODEL_SRC="vendor-sherpa/model"
MODEL_DST="$APP/Contents/Resources/sherpa/model"

if [ ! -f "$MODEL_SRC/model.onnx" ]; then
    echo "错误：$MODEL_SRC/model.onnx 不存在，请先执行 make deps" >&2
    exit 1
fi

echo "正在将 sherpa 模型打包到 $MODEL_DST ..."
mkdir -p "$MODEL_DST"
cp -r "$MODEL_SRC/." "$MODEL_DST/"
echo "完成。"
