#!/usr/bin/env python3
"""
kokoro_tts.py — Kokoro-82M ONNX TTS 子进程脚本。

用法：
  python3 kokoro_tts.py --voice zf_xiaobei --speed 1.0 \
                        --model-dir /path/to/models --text "你好世界"

输出：WAV 字节流写入 stdout；所有日志/错误写入 stderr。
"""

import argparse
import os
import sys


def is_mostly_chinese(text: str, threshold: float = 0.3) -> bool:
    """判断文本是否以中文为主（中文字符占比超过阈值）。"""
    if not text:
        return False
    chinese = sum(1 for c in text if '一' <= c <= '鿿')
    return chinese / len(text) >= threshold


def phonemize(text: str, kokoro) -> str:
    """将文本转换为音素字符串。中文使用 misaki ZHG2P，其余使用内置 phonemizer。"""
    if is_mostly_chinese(text):
        # 抑制 jieba 初始化日志，避免污染 stdout
        import logging
        logging.getLogger('jieba').setLevel(logging.ERROR)
        import jieba
        jieba.setLogLevel(logging.ERROR)
        from misaki import zh
        g2p = zh.ZHG2P()
        phonemes = g2p(text)
    else:
        return kokoro.tokenizer.phonemize(text, lang='en-us')

    # 过滤不在词表中的字符
    vocab = kokoro.tokenizer.vocab
    return ''.join(c for c in phonemes if c in vocab)


def main() -> None:
    parser = argparse.ArgumentParser(description='Kokoro-82M TTS')
    parser.add_argument('--voice',     default='zf_xiaobei', help='声线名称')
    parser.add_argument('--speed',     type=float, default=1.0, help='语速 (0.5-2.0)')
    parser.add_argument('--model-dir', required=True, help='models 目录（含 .onnx 和 .bin）')
    parser.add_argument('--text',      default='', help='合成文本（留空则从 stdin 读取）')
    args = parser.parse_args()

    text = args.text.strip() if args.text else sys.stdin.read().strip()
    if not text:
        print('kokoro_tts: empty input', file=sys.stderr)
        sys.exit(1)

    onnx_path  = os.path.join(args.model_dir, 'kokoro-v1.0.onnx')
    voices_path = os.path.join(args.model_dir, 'voices-v1.0.bin')
    for p in (onnx_path, voices_path):
        if not os.path.exists(p):
            print(f'kokoro_tts: kokoro model not found: {p}', file=sys.stderr)
            sys.exit(1)

    from kokoro_onnx import Kokoro
    kokoro = Kokoro(onnx_path, voices_path)

    phonemes = phonemize(text, kokoro)
    samples, sample_rate = kokoro.create(
        phonemes,
        voice=args.voice,
        speed=args.speed,
        lang='en-us',
        is_phonemes=True,
    )

    import soundfile as sf
    import io
    buf = io.BytesIO()
    sf.write(buf, samples, sample_rate, format='WAV')
    sys.stdout.buffer.write(buf.getvalue())


if __name__ == '__main__':
    main()
