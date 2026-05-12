# 向量模型独立接入配置

**日期：** 2026-05-12  
**状态：** 已批准

## 背景

当前 `ModelProfile` 只有一对 `BaseURL` / `APIKey`，chat model 和 embedder 共用同一套接入参数。部分用户的向量模型（如 OpenAI text-embedding）和对话模型（如本地 Ollama / DeepSeek）来自不同服务商，需要独立配置。

## 目标

- 允许向量模型走独立的 Base URL 和 API Key
- 默认行为不变（继承 chat 模型配置）
- 取消继承且 Base URL 为空时，向量功能自动禁用（不报错）

## 方案

**方案 A（采用）**：在 `ModelProfile` 和 `Config` 上新增 embedding 专属字段，`ApplyProfile` 时根据 inherit 开关决定填哪组值，`NewEmbedder` 只读 `Config` 中的 embedding 专属字段。

## 数据层

### ModelProfile（`internal/config/profile.go`）

新增字段：

```go
EmbeddingInherit bool   `json:"embedding_inherit"` // true = 继承 chat 模型配置（默认）
EmbeddingBaseURL string `json:"embedding_base_url"`
EmbeddingAPIKey  string `json:"embedding_api_key"`
```

DB 表 `model_profiles` 新增三列（idempotent patch）：

```sql
ALTER TABLE model_profiles ADD COLUMN embedding_inherit  INTEGER NOT NULL DEFAULT 1
ALTER TABLE model_profiles ADD COLUMN embedding_base_url TEXT    NOT NULL DEFAULT ''
ALTER TABLE model_profiles ADD COLUMN embedding_api_key  TEXT    NOT NULL DEFAULT ''
```

List / Get / Save SQL 同步更新以包含这三列。

### Config（`internal/config/config.go`）

新增字段：

```go
EmbeddingBaseURL string
EmbeddingAPIKey  string
```

### ApplyProfile 逻辑

```
if p.EmbeddingInherit {
    Config.EmbeddingBaseURL = p.BaseURL
    Config.EmbeddingAPIKey  = p.APIKey
} else {
    Config.EmbeddingBaseURL = p.EmbeddingBaseURL
    Config.EmbeddingAPIKey  = p.EmbeddingAPIKey
}
```

### VectorEnabled()

判断顺序：

1. `EmbeddingModel == ""` → false
2. `EmbeddingBaseURL == ""` → false（inherit=false 且未填时留空）
3. 否则 → true

## LLM 层（`internal/llm/client.go`）

`NewEmbedder` 改为使用 `cfg.EmbeddingBaseURL` / `cfg.EmbeddingAPIKey`，不再直接引用 `cfg.LLMBaseURL` / `cfg.LLMAPIKey`。

其余函数（`NewChatModel`、`NewSummarizer`）无需改动。

## 前端（`frontend/src/components/SettingsWindow.vue`）

### profileForm 默认值增加

```js
embedding_inherit: true,
embedding_base_url: '',
embedding_api_key: ''
```

### editProfile

```js
profileForm.value = {
  ...p,
  embedding_inherit: p.embedding_inherit ?? true,
  tts_backend: p.tts_backend || ''
}
```

### 表单 UI

在"向量模型（Embedding）"模型选择下方插入：

1. **继承开关**（始终可见）
   ```html
   <label class="checkbox-label">
     <input type="checkbox" v-model="profileForm.embedding_inherit" />
     向量模型继承 Chat 模型配置（使用相同的 Base URL 和 API Key）
   </label>
   ```

2. **独立配置区**（`v-if="!profileForm.embedding_inherit"`）
   - Base URL 输入，placeholder `http://localhost:11434/v1`
   - API Key 密码输入，placeholder `（可选）`

### 保存校验

`!embedding_inherit && !embedding_base_url.trim()` 时**不报错**，向量功能将在运行时因 `VectorEnabled()` 返回 false 而自动禁用。

## 不受影响的部分

- `NewChatModel` / `NewSummarizer` — 继续使用 `cfg.LLMBaseURL` / `cfg.LLMAPIKey`
- Agent 初始化逻辑 — 无需改动
- Profile 卡片列表副标题 — 不展示 embedding 地址
