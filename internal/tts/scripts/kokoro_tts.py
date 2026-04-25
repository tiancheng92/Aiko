#!/usr/bin/env python3
"""
kokoro_tts.py — Kokoro-82M ONNX TTS 子进程脚本。

用法：
  python3 kokoro_tts.py --voice zf_xiaobei --speed 1.0 \
                        --model-dir /path/to/models --text "你好世界"

输出：WAV 字节流写入 stdout；所有日志/错误写入 stderr。

特性：
  - 预处理：剥离 emoji 及装饰性非语音字符，避免送入 G2P 产生乱码
  - 按句子边界分段合成，模型可正确推断句级韵律
  - 中英混排：英文段用英文 phonemizer，中文段用 misaki ZHG2P，分别处理后拼接
  - 逗号/顿号停顿 200ms，句间停顿 350ms，段落间停顿 600ms
"""

import argparse
import os
import re
import sys
import unicodedata

# 句尾标点（中英文 + 波浪号 + 省略号 + 换行）
_SENT_END = re.compile(r'(?<=[。！？!?\n～…])\s*')
# 逗号/顿号/分号（中英文）
_COMMA = re.compile(r'(?<=[，,、；;])\s*')
# 段落边界（一个或多个换行）
_PARA_BREAK = re.compile(r'\n+')
# 连续英文单词段
_EN_SPAN = re.compile(r"[A-Za-z][A-Za-z0-9'\-\.]*(?:\s+[A-Za-z][A-Za-z0-9'\-\.]*)*")

# 静音时长（秒）
COMMA_PAUSE = 0.20
SENTENCE_PAUSE = 0.35
PARAGRAPH_PAUSE = 0.60


def strip_nonspeech(text):
    """
    剥离 emoji 及装饰性非语音字符，保留中文、英文、数字和基本标点。
    返回清理后的字符串。
    """
    result = []
    for ch in text:
        cat = unicodedata.category(ch)
        cp = ord(ch)
        # 保留：中文（CJK 统一汉字）
        if 0x4E00 <= cp <= 0x9FFF or 0x3400 <= cp <= 0x4DBF:
            result.append(ch)
        # 保留：ASCII 可打印字符（含英文字母、数字、标准标点）
        elif 0x20 <= cp <= 0x7E:
            result.append(ch)
        # 保留：中文常用标点
        elif ch in '。，！？、；：""''（）【】《》…～\n':
            result.append(ch)
        # 保留：日文假名（部分声线会用到）
        elif 0x3040 <= cp <= 0x30FF:
            result.append(ch)
        # 其余（emoji、装饰符号、全角杂项）→ 替换为空格以保持词边界
        elif cat.startswith('Z') or cat.startswith('C'):
            result.append(' ')
        else:
            result.append(' ')
    return re.sub(r' {2,}', ' ', ''.join(result)).strip()


def split_zh_en(text):
    """将混合文本拆分为 [(segment, lang)] 列表，lang 为 'zh' 或 'en'。"""
    result = []
    pos = 0
    for m in _EN_SPAN.finditer(text):
        if m.start() > pos:
            zh_part = text[pos:m.start()]
            if zh_part.strip():
                result.append((zh_part, 'zh'))
        result.append((m.group(), 'en'))
        pos = m.end()
    if pos < len(text):
        rest = text[pos:]
        if rest.strip():
            result.append((rest, 'zh'))
    return result if result else [(text, 'zh')]


def make_g2p():
    """懒加载 misaki ZHG2P，抑制 jieba 初始化日志。"""
    import logging
    logging.getLogger('jieba').setLevel(logging.ERROR)
    import jieba
    jieba.setLogLevel(logging.ERROR)
    from misaki import zh
    return zh.ZHG2P()


def phonemize_clause(clause, kokoro, g2p_cache):
    """
    将子句转换为 [(phonemes, voice_override)] 列表。
    中英混排时分段 phonemize，但统一使用调用方指定声线合成。
    """
    segments = split_zh_en(clause)
    if all(lang == 'en' for _, lang in segments):
        return [(kokoro.tokenizer.phonemize(clause, lang='en-us'), None)]
    if not g2p_cache:
        g2p_cache.append(make_g2p())
    g2p = g2p_cache[0]
    vocab = kokoro.tokenizer.vocab
    result = []
    for seg, lang in segments:
        if lang == 'en':
            ph = kokoro.tokenizer.phonemize(seg, lang='en-us')
            if ph.strip():
                result.append((ph, None))
        else:
            ph = g2p(seg)
            ph = ''.join(c for c in ph if c in vocab)
            if ph.strip():
                result.append((ph, None))
    return result


def split_segments(text):
    """
    将文本拆分为 (clause, pause_seconds) 列表。
    层级：段落（换行）→ 句子（句尾标点）→ 逗号子句。
    """
    result = []
    paragraphs = [p.strip() for p in _PARA_BREAK.split(text) if p.strip()]
    num_paras = len(paragraphs)
    for pi, para in enumerate(paragraphs):
        is_last_para = (pi == num_paras - 1)
        sentences = [s.strip() for s in _SENT_END.split(para) if s.strip()]
        num_sents = len(sentences)
        for si, sent in enumerate(sentences):
            is_last_sent = (si == num_sents - 1)
            sent_pause = (PARAGRAPH_PAUSE if not is_last_para else 0.0) if is_last_sent else SENTENCE_PAUSE
            clauses = [c.strip() for c in _COMMA.split(sent) if c.strip()]
            num_clauses = len(clauses)
            for ci, clause in enumerate(clauses):
                is_last_clause = (ci == num_clauses - 1)
                pause = sent_pause if is_last_clause else COMMA_PAUSE
                result.append((clause, pause))
    return result if result else [(text.strip(), 0.0)]


def make_silence(sample_rate, duration):
    """生成指定时长的静音 numpy 数组。"""
    import numpy as np
    return np.zeros(int(sample_rate * duration), dtype=np.float32)


def main():
    parser = argparse.ArgumentParser(description='Kokoro-82M TTS')
    parser.add_argument('--voice',     default='zf_xiaobei')
    parser.add_argument('--speed',     type=float, default=1.0)
    parser.add_argument('--model-dir', required=True)
    parser.add_argument('--text',      default='')
    args = parser.parse_args()

    raw_text = args.text.strip() if args.text else sys.stdin.read().strip()
    if not raw_text:
        print('kokoro_tts: empty input', file=sys.stderr)
        sys.exit(1)

    text = strip_nonspeech(raw_text)
    if not text:
        print('kokoro_tts: no speakable content after stripping', file=sys.stderr)
        sys.exit(1)

    onnx_path   = os.path.join(args.model_dir, 'kokoro-v1.0.onnx')
    voices_path = os.path.join(args.model_dir, 'voices-v1.0.bin')
    for p in (onnx_path, voices_path):
        if not os.path.exists(p):
            print(f'kokoro_tts: model not found: {p}', file=sys.stderr)
            sys.exit(1)

    import numpy as np
    from kokoro_onnx import Kokoro
    kokoro = Kokoro(onnx_path, voices_path)

    segments = split_segments(text)
    g2p_cache = []
    all_samples = []
    sample_rate = None

    for clause, pause in segments:
        ph_parts = phonemize_clause(clause, kokoro, g2p_cache)
        for phonemes, _ in ph_parts:
            if not phonemes.strip():
                continue
            samples, sr = kokoro.create(
                phonemes,
                voice=args.voice,
                speed=args.speed,
                lang='en-us',
                is_phonemes=True,
            )
            if sample_rate is None:
                sample_rate = sr
            all_samples.append(samples)
        if pause > 0 and sample_rate:
            all_samples.append(make_silence(sample_rate, pause))

    if not all_samples:
        print('kokoro_tts: no audio generated', file=sys.stderr)
        sys.exit(1)

    import soundfile as sf
    import io
    combined = np.concatenate(all_samples)
    buf = io.BytesIO()
    sf.write(buf, combined, sample_rate, format='WAV')
    sys.stdout.buffer.write(buf.getvalue())


if __name__ == '__main__':
    main()
