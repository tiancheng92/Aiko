# Developer Utility Tools — Design Spec

**Date:** 2026-05-10  
**Status:** Approved

## Overview

Add 12 developer utility tools to Aiko's built-in tool registry. All tools are stateless pure-computation or lightweight-network tools. The clipboard workflow (e.g. "把剪切板里的 JSON 转成 Go struct") is handled transparently by the ReAct agent chaining `read_clipboard` → target tool; no per-tool clipboard logic is needed.

## Architecture

### File organisation

- **New file:** `internal/tools/dev_tools.go` — all 12 tools in one file (high cohesion, all dev-utility domain)
- **Modified:** `internal/tools/registry.go` — append new tools to `All()`

### Dependencies

| Library | Status |
|---------|--------|
| `gopkg.in/yaml.v3` | Already in `go.mod` (indirect) |
| `github.com/google/uuid` | Already in `go.mod` |
| `crypto/md5`, `crypto/sha1`, `crypto/sha256`, `crypto/sha256`, `crypto/sha512` | stdlib |
| `encoding/base64`, `net/url`, `html` | stdlib |
| `regexp`, `strconv`, `math` | stdlib |
| `net/http` (exchange rate only) | stdlib |

No new `go get` required.

### Permission levels

- `PermPublic` — 11 pure-computation tools (no network, no side effects)
- `PermProtected` — `get_exchange_rate` (external HTTP call to open.er-api.com)

---

## Tool Specifications

### 1. `format_json`

**Desc:** JSON 格式化、压缩或语法校验。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `json_string` | string | yes | — | Raw JSON input |
| `action` | enum | no | `pretty` | `pretty` / `minify` / `validate` |
| `indent` | int | no | 2 | Spaces per indent level (pretty only) |

**Returns:**  
- `pretty`: indented JSON string  
- `minify`: single-line JSON  
- `validate`: `"valid JSON"` or error message with line/column

---

### 2. `json_to_struct`

**Desc:** JSON 转多语言结构体定义。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `json_string` | string | yes | — | Valid JSON object or array |
| `language` | enum | yes | — | `go` / `typescript` / `python` / `rust` |
| `type_name` | string | no | `Root` | Root type/struct name |

**Generation rules:**

| Language | Style |
|----------|-------|
| Go | PascalCase fields + `json:"..."` tag; nested objects as inline named structs |
| TypeScript | `interface`; nested types inline; `null` → `T \| null`; optional fields as `field?: T` if value was null |
| Python | `@dataclass` with type hints; `Optional[T]` for null; nested classes inline above parent |
| Rust | `#[derive(Debug, Serialize, Deserialize)]` struct; snake_case fields with `#[serde(rename = "...")]` if original is camelCase |

Type inference: JSON number with no decimal → `int`/`Int`/`int`/`i64`; with decimal → `float64`/`number`/`float`/`f64`.

---

### 3. `yaml_json_convert`

**Desc:** YAML ↔ JSON 互转。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `input` | string | yes | — | YAML or JSON content |
| `direction` | enum | yes | — | `yaml_to_json` / `json_to_yaml` |
| `pretty` | bool | no | true | Pretty-print JSON output |

---

### 4. `encode_decode`

**Desc:** Base64 / URL / HTML 编解码。  
**Permission:** PermPublic

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `text` | string | yes | Input text |
| `format` | enum | yes | `base64` / `base64url` / `url` / `html` |
| `action` | enum | yes | `encode` / `decode` |

---

### 5. `hash_text`

**Desc:** 文本哈希计算。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `text` | string | yes | — | Input to hash |
| `algorithm` | enum | yes | — | `md5` / `sha1` / `sha256` / `sha512` |
| `encoding` | enum | no | `hex` | `hex` / `base64` |

---

### 6. `generate_uuid`

**Desc:** 批量生成 UUID v4。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `count` | int | no | 1 | 1–100 |
| `format` | enum | no | `standard` | `standard` (xxxxxxxx-xxxx-…) / `no_dash` / `upper` |

---

### 7. `convert_timestamp`

**Desc:** Unix 时间戳与可读时间字符串互转。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `value` | string | yes | — | Unix timestamp (integer) or datetime string (RFC3339 / common formats) |
| `direction` | enum | yes | — | `unix_to_datetime` / `datetime_to_unix` |
| `timezone` | string | no | `local` | IANA tz name, e.g. `Asia/Shanghai` |

Returns multi-timezone comparison (local, UTC, and requested tz) when converting unix→datetime.

---

### 8. `regex_test`

**Desc:** 正则表达式测试（Go RE2 语法）。  
**Permission:** PermPublic

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `pattern` | string | yes | RE2 regex pattern |
| `text` | string | yes | Text to test against |

**Returns:** match result (true/false), all matched substrings, named/indexed capture groups.

---

### 9. `number_base_convert`

**Desc:** 整数进制转换（二 / 八 / 十 / 十六进制）。  
**Permission:** PermPublic

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `value` | string | yes | Number string in source base |
| `from` | enum | yes | `2` / `8` / `10` / `16` |
| `to` | enum | yes | `2` / `8` / `10` / `16` |

Returns result with conventional prefix (`0b`, `0o`, `0x`).

---

### 10. `convert_units`

**Desc:** 物理量单位换算，涵盖七个类别。  
**Permission:** PermPublic

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `value` | number | yes | Numeric value to convert |
| `from_unit` | string | yes | Source unit identifier (see below) |
| `to_unit` | string | yes | Target unit identifier |

**Supported units by category:**

| Category | Units |
|----------|-------|
| length | `mm`, `cm`, `m`, `km`, `inch`, `ft`, `yard`, `mile` |
| weight | `mg`, `g`, `kg`, `ton`, `lb`, `oz` |
| temperature | `C`, `F`, `K` |
| area | `mm2`, `cm2`, `m2`, `km2`, `acre`, `hectare` |
| volume | `ml`, `l`, `gallon`, `fl_oz`, `cup` |
| speed | `m_s`, `km_h`, `mph`, `knot` |
| data | `bit`, `byte`, `KB`, `MB`, `GB`, `TB` |

---

### 11. `get_exchange_rate`

**Desc:** 查询实时外汇汇率并换算金额。  
**Permission:** PermProtected  
**API:** `https://open.er-api.com/v6/latest/{base}` (free, no key required)

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `base` | string | yes | — | Base currency code, e.g. `USD`, `CNY` |
| `targets` | []string | no | major currencies | Target currency codes; empty → USD/EUR/CNY/JPY/GBP/HKD/KRW |
| `amount` | float | no | 1 | Amount of base currency to convert |

**Returns:** rate table + converted amounts for each target.

---

### 12. `convert_color`

**Desc:** 色值格式互转（HEX / RGB / HSL）。  
**Permission:** PermPublic

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `input` | string | yes | — | `#RRGGBB`, `#RGB`, `rgb(r,g,b)`, `hsl(h,s%,l%)` |
| `output` | enum | no | `all` | `hex` / `rgb` / `hsl` / `all` |

---

## Registration

In `registry.go`, append to `All()`:

```go
&FormatJSONTool{},
&JSONToStructTool{},
&YAMLJSONConvertTool{},
&EncodeDecodeTool{},
&HashTextTool{},
&GenerateUUIDTool{},
&ConvertTimestampTool{},
&RegexTestTool{},
&NumberBaseConvertTool{},
&ConvertUnitsTool{},
&GetExchangeRateTool{},
&ConvertColorTool{},
```

`AllPermissionDeclarations()` picks these up automatically via `All()`, so no separate declaration block is needed.

---

## Clipboard Workflow

No per-tool clipboard integration is required. The ReAct agent already has access to `read_clipboard`. When the user says "把剪切板里的 JSON 转成 Go struct", the agent:

1. Calls `read_clipboard` → gets JSON string
2. Calls `json_to_struct` with `language: "go"` and the clipboard content

This is handled transparently by the agent's reasoning loop.

---

## Out of Scope

- JWT decode (requires separate library, lower priority)
- Diff/patch tool (requires line-diff library)
- Windows/Linux variants (project is macOS-only)
