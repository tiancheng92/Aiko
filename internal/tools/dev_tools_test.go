package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatJSON(t *testing.T) {
	tool := &FormatJSONTool{}
	if tool.Name() != "format_json" {
		t.Fatalf("unexpected name: %s", tool.Name())
	}
	if tool.Permission() != PermPublic {
		t.Fatalf("expected PermPublic")
	}

	t.Run("pretty", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"action":"pretty","json_string":"{\"a\":1,\"b\":2}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "\n") {
			t.Errorf("expected indented output, got: %s", out)
		}
	})

	t.Run("minify", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"action":"minify","json_string":"{\n  \"a\": 1\n}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\n") {
			t.Errorf("expected single-line output, got: %s", out)
		}
	})

	t.Run("validate_valid", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"action":"validate","json_string":"{\"a\":1}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "有效") {
			t.Errorf("expected valid, got: %s", out)
		}
	})

	t.Run("validate_invalid", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"action":"validate","json_string":"{bad json}"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "invalid") && !strings.Contains(out, "error") && !strings.Contains(out, "错误") {
			t.Errorf("expected error message, got: %s", out)
		}
	})
}

func TestJSONToStruct(t *testing.T) {
	tool := &JSONToStructTool{}
	if tool.Name() != "json_to_struct" {
		t.Fatalf("unexpected name: %s", tool.Name())
	}

	t.Run("go", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"json_string":"{\"name\":\"Alice\",\"age\":30,\"active\":true}","language":"go","type_name":"User"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "type User struct") {
			t.Errorf("missing Go struct declaration, got: %s", out)
		}
		if !strings.Contains(out, `json:"name"`) {
			t.Errorf("missing json tag, got: %s", out)
		}
	})

	t.Run("typescript", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"json_string":"{\"name\":\"Alice\",\"age\":30,\"active\":true}","language":"typescript","type_name":"User"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "interface User") {
			t.Errorf("missing TS interface declaration, got: %s", out)
		}
	})

	t.Run("python", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"json_string":"{\"name\":\"Alice\",\"age\":30,\"active\":true}","language":"python","type_name":"User"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "@dataclass") {
			t.Errorf("missing @dataclass, got: %s", out)
		}
		if !strings.Contains(out, "class User") {
			t.Errorf("missing class User, got: %s", out)
		}
	})

	t.Run("rust", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"json_string":"{\"name\":\"Alice\",\"age\":30,\"active\":true}","language":"rust","type_name":"User"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "struct User") {
			t.Errorf("missing struct User, got: %s", out)
		}
		if !strings.Contains(out, "Serialize") {
			t.Errorf("missing Serialize derive, got: %s", out)
		}
	})

	t.Run("nested_object", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"json_string":"{\"user\":{\"name\":\"Bob\",\"age\":25},\"count\":3}","language":"go","type_name":"Root"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "type Root struct") {
			t.Errorf("missing Root struct, got: %s", out)
		}
		if !strings.Contains(out, "type User struct") {
			t.Errorf("missing nested User struct, got: %s", out)
		}
	})
}

func TestYAMLJSONConvert(t *testing.T) {
	tool := &YAMLJSONConvertTool{}
	if tool.Name() != "yaml_json_convert" {
		t.Fatalf("unexpected name: %s", tool.Name())
	}

	t.Run("yaml_to_json", func(t *testing.T) {
		yaml := "name: Alice\nage: 30\n"
		out, err := tool.InvokableRun(nil, `{"input":"`+yaml+`","direction":"yaml_to_json"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"name"`) || !strings.Contains(out, `"Alice"`) {
			t.Errorf("expected JSON output, got: %s", out)
		}
	})

	t.Run("json_to_yaml", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"input":"{\"name\":\"Alice\",\"age\":30}","direction":"json_to_yaml"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "name:") {
			t.Errorf("expected YAML output, got: %s", out)
		}
	})
}

func TestEncodeDecode(t *testing.T) {
	tool := &EncodeDecodeTool{}

	t.Run("base64_roundtrip", func(t *testing.T) {
		enc, err := tool.InvokableRun(nil, `{"text":"Hello, 世界","format":"base64","action":"encode"}`)
		if err != nil {
			t.Fatal(err)
		}
		dec, err := tool.InvokableRun(nil, `{"text":"`+enc+`","format":"base64","action":"decode"}`)
		if err != nil {
			t.Fatal(err)
		}
		if dec != "Hello, 世界" {
			t.Errorf("roundtrip failed: got %q", dec)
		}
	})

	t.Run("url_encode", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"text":"a b+c","format":"url","action":"encode"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "%") {
			t.Errorf("expected URL-encoded output, got: %s", out)
		}
	})

	t.Run("html_encode", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"text":"<b>bold</b>","format":"html","action":"encode"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "&lt;") {
			t.Errorf("expected HTML-escaped output, got: %s", out)
		}
	})
}

func TestHashText(t *testing.T) {
	tool := &HashTextTool{}
	// SHA256 of "hello" is a well-known value.
	out, err := tool.InvokableRun(nil, `{"text":"hello","algorithm":"sha256","encoding":"hex"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if out != want {
		t.Errorf("sha256(hello) = %q, want %q", out, want)
	}

	// MD5 of "hello".
	out, err = tool.InvokableRun(nil, `{"text":"hello","algorithm":"md5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("md5(hello) = %q", out)
	}
}

func TestGenerateUUID(t *testing.T) {
	tool := &GenerateUUIDTool{}

	t.Run("single_standard", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"count":1,"format":"standard"}`)
		if err != nil {
			t.Fatal(err)
		}
		// Standard UUID: 8-4-4-4-12
		if len(strings.Split(out, "-")) != 5 {
			t.Errorf("expected standard UUID format, got: %s", out)
		}
	})

	t.Run("batch", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"count":5}`)
		if err != nil {
			t.Fatal(err)
		}
		if len(strings.Split(out, "\n")) != 5 {
			t.Errorf("expected 5 UUIDs, got: %s", out)
		}
	})

	t.Run("no_dash", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"count":1,"format":"no_dash"}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "-") || len(out) != 32 {
			t.Errorf("expected 32-char no-dash UUID, got: %s", out)
		}
	})
}

func TestConvertTimestamp(t *testing.T) {
	tool := &ConvertTimestampTool{}

	t.Run("unix_to_datetime", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"value":"0","direction":"unix_to_datetime","timezone":"UTC"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "1970-01-01") {
			t.Errorf("expected epoch date, got: %s", out)
		}
	})

	t.Run("datetime_to_unix", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"value":"1970-01-01T00:00:00Z","direction":"datetime_to_unix"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "0") {
			t.Errorf("expected unix 0, got: %s", out)
		}
	})
}

func TestRegexTest(t *testing.T) {
	tool := &RegexTestTool{}

	t.Run("match", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"pattern":"\\d+","text":"abc 123 def"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "✓") {
			t.Errorf("expected match, got: %s", out)
		}
		if !strings.Contains(out, "123") {
			t.Errorf("expected matched substring 123, got: %s", out)
		}
	})

	t.Run("no_match", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"pattern":"\\d+","text":"abc"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "✗") {
			t.Errorf("expected no match, got: %s", out)
		}
	})

	t.Run("invalid_pattern", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"pattern":"[invalid","text":"abc"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "无效") {
			t.Errorf("expected invalid pattern error, got: %s", out)
		}
	})
}

func TestNumberBaseConvert(t *testing.T) {
	tool := &NumberBaseConvertTool{}

	cases := []struct {
		value, from, to, wantContains string
	}{
		{"255", "10", "16", "0xFF"},
		{"FF", "16", "10", "255"},
		{"255", "10", "2", "0b11111111"},
		{"11111111", "2", "10", "255"},
	}
	for _, c := range cases {
		input := fmt.Sprintf(`{"value":"%s","from":"%s","to":"%s"}`, c.value, c.from, c.to)
		out, err := tool.InvokableRun(nil, input)
		if err != nil {
			t.Fatalf("base convert %s(%s)→%s: %v", c.value, c.from, c.to, err)
		}
		if !strings.Contains(out, c.wantContains) {
			t.Errorf("base convert %s(%s)→%s: got %q, want contains %q", c.value, c.from, c.to, out, c.wantContains)
		}
	}
}

func TestConvertUnits(t *testing.T) {
	tool := &ConvertUnitsTool{}

	t.Run("km_to_miles", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"value":1,"from_unit":"km","to_unit":"mile"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "0.621") {
			t.Errorf("1 km = ~0.621 miles, got: %s", out)
		}
	})

	t.Run("celsius_to_fahrenheit", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"value":100,"from_unit":"C","to_unit":"F"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "212") {
			t.Errorf("100°C = 212°F, got: %s", out)
		}
	})

	t.Run("gb_to_mb", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"value":1,"from_unit":"GB","to_unit":"MB"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "1024") {
			t.Errorf("1 GB = 1024 MB, got: %s", out)
		}
	})
}

func TestConvertColor(t *testing.T) {
	tool := &ConvertColorTool{}

	t.Run("hex_to_all", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"input":"#FF0000","output":"all"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "rgb(255, 0, 0)") {
			t.Errorf("expected rgb(255, 0, 0), got: %s", out)
		}
		if !strings.Contains(out, "hsl(0") {
			t.Errorf("expected hsl(0,...), got: %s", out)
		}
	})

	t.Run("short_hex", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"input":"#F00","output":"hex"}`)
		if err != nil {
			t.Fatal(err)
		}
		if out != "#FF0000" {
			t.Errorf("expected #FF0000, got: %s", out)
		}
	})

	t.Run("rgb_to_hex", func(t *testing.T) {
		out, err := tool.InvokableRun(nil, `{"input":"rgb(255, 0, 0)","output":"hex"}`)
		if err != nil {
			t.Fatal(err)
		}
		if out != "#FF0000" {
			t.Errorf("expected #FF0000, got: %s", out)
		}
	})
}
