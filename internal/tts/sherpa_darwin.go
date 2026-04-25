//go:build darwin

package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// SherpaSpeaker 通过 sherpa-onnx 离线 TTS 实现 Speaker 接口。
// 模型在构造时加载一次，Speak 并发安全（底层 C 库线程安全）。
type SherpaSpeaker struct {
	tts      *sherpa.OfflineTts
	modelDir string
}

// NewSherpaSpeaker 从 modelDir 加载 VITS 模型并返回 SherpaSpeaker。
// modelDir 必须包含 model.onnx、lexicon.txt、tokens.txt。
func NewSherpaSpeaker(modelDir string) (*SherpaSpeaker, error) {
	cfg := sherpa.OfflineTtsConfig{}
	cfg.Model.Vits.Model = modelDir + "/model.onnx"
	cfg.Model.Vits.Lexicon = modelDir + "/lexicon.txt"
	cfg.Model.Vits.Tokens = modelDir + "/tokens.txt"
	cfg.Model.Vits.NoiseScale = 0.667
	cfg.Model.Vits.NoiseScaleW = 0.8
	cfg.Model.Vits.LengthScale = 1.0
	cfg.Model.NumThreads = 2
	cfg.MaxNumSentences = 1
	// 数字/日期规范化 FST（可选，文件不存在时 sherpa 会忽略）
	cfg.RuleFsts = modelDir + "/phone.fst," + modelDir + "/date.fst," + modelDir + "/number.fst"
	cfg.RuleFars = modelDir + "/rule.far"

	slog.Info("sherpa: 加载 TTS 模型", "dir", modelDir)
	t := sherpa.NewOfflineTts(&cfg)
	if t == nil {
		return nil, fmt.Errorf("sherpa: 无法从 %s 创建 OfflineTts", modelDir)
	}
	slog.Info("sherpa: 模型加载完成", "numSpeakers", t.NumSpeakers(), "sampleRate", t.SampleRate())
	return &SherpaSpeaker{tts: t, modelDir: modelDir}, nil
}

// Speak 将文本合成为 WAV 音频字节。
// voice 可为 "default"、"speaker-N"（N=0..173）或裸整数字符串。
// speed 控制语速（0.5–2.0；0 表示使用默认值 1.0）。
func (s *SherpaSpeaker) Speak(_ context.Context, text, voice string, speed float64) ([]byte, error) {
	if speed <= 0 {
		speed = 1.0
	}
	sid := s.voiceToSID(voice)
	slog.Info("sherpa: Speak", "sid", sid, "speed", speed, "字数", len([]rune(text)))

	gcfg := sherpa.GenerationConfig{
		Sid:   sid,
		Speed: float32(speed),
	}
	audio := s.tts.GenerateWithConfig(text, &gcfg, nil)
	if audio == nil || len(audio.Samples) == 0 {
		return nil, fmt.Errorf("sherpa: 生成结果为空")
	}
	wav := samplesToWAV(audio.Samples, audio.SampleRate)
	slog.Info("sherpa: 生成完成", "samples", len(audio.Samples), "wavBytes", len(wav))
	return wav, nil
}

// Voices 返回可用声线名称列表。
func (s *SherpaSpeaker) Voices(_ context.Context) ([]string, error) {
	n := s.tts.NumSpeakers()
	if n <= 0 {
		n = 1
	}
	limit := n
	if limit > 10 {
		limit = 10
	}
	voices := make([]string, 0, limit+1)
	voices = append(voices, "default")
	for i := 0; i < limit; i++ {
		voices = append(voices, fmt.Sprintf("speaker-%d", i))
	}
	return voices, nil
}

// voiceToSID 将声线名称转换为 sherpa 说话人 ID。
func (s *SherpaSpeaker) voiceToSID(voice string) int {
	switch {
	case voice == "" || voice == "default":
		return 0
	case strings.HasPrefix(voice, "speaker-"):
		n, err := strconv.Atoi(strings.TrimPrefix(voice, "speaker-"))
		if err == nil && n >= 0 {
			return n
		}
	default:
		if n, err := strconv.Atoi(voice); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// samplesToWAV 将 float32 PCM 采样编码为 16-bit PCM WAV 字节。
func samplesToWAV(samples []float32, sampleRate int) []byte {
	numChannels := uint16(1)
	bitsPerSample := uint16(16)
	blockAlign := numChannels * bitsPerSample / 8
	byteRate := uint32(sampleRate) * uint32(blockAlign)
	dataSize := uint32(len(samples)) * uint32(bitsPerSample/8)
	chunkSize := 36 + dataSize

	buf := &bytes.Buffer{}
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, chunkSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(buf, binary.LittleEndian, numChannels)
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, byteRate)
	binary.Write(buf, binary.LittleEndian, blockAlign)
	binary.Write(buf, binary.LittleEndian, bitsPerSample)
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, dataSize)
	for _, v := range samples {
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		binary.Write(buf, binary.LittleEndian, int16(v*32767))
	}
	return buf.Bytes()
}
