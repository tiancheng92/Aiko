package tools

import (
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
		if !strings.Contains(out, "valid") {
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
