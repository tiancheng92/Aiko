package notify

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "Hello, this is a notification",
			want:  "Hello, this is a notification",
		},
		{
			name:  "strips tool-call tag",
			input: "Let me check that.\n\n<tool-call name=\"read_file\" args=\"eyJwYXRoIjoiL2V0Yy9ob3N0cyJ9\"></tool-call>\n\nThe file contains...",
			want:  "Let me check that.\n\n\n\nThe file contains...",
		},
		{
			name:  "strips skill-call tag",
			input: "Using a skill.\n\n<skill-call name=\"summarize\" args=\"e30=\"></skill-call>\n\nHere is the summary.",
			want:  "Using a skill.\n\n\n\nHere is the summary.",
		},
		{
			name:  "strips emotion tag",
			input: "[情绪:happy/0.95]\n今天天气真不错！",
			want:  "今天天气真不错！",
		},
		{
			name:  "strips emotion tag without newline",
			input: "[情绪:neutral/0.50]好的，我来帮你处理。",
			want:  "好的，我来帮你处理。",
		},
		{
			name:  "strips multiple patterns",
			input: "[情绪:excited/0.90]\n<tool-call name=\"search\" args=\"e30=\"></tool-call>\n<skill-call name=\"translate\" args=\"e30=\"></skill-call>\n结果如下：",
			want:  "\n\n结果如下：",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no patterns to strip",
			input: "任务执行成功：已整理今日日程。",
			want:  "任务执行成功：已整理今日日程。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitize(tt.input)
			if got != tt.want {
				t.Errorf("sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
