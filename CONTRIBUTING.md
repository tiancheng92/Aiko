# 贡献指南

感谢你对 Aiko 桌面宠物项目的关注！我们欢迎所有形式的贡献，包括代码、文档、测试、建议和反馈。

## 🤝 参与方式

### 1. 报告问题
如果你发现了 Bug 或有功能建议：
- 查看 [Issues](https://github.com/tiancheng92/Aiko/issues) 确认问题未被重复报告
- 创建新 Issue，使用合适的标签（bug/enhancement/question）
- 详细描述问题和复现步骤

### 2. 提交代码
欢迎提交 Pull Request！请遵循以下流程：

1. **Fork 项目** 到你的 GitHub 账号
2. **创建功能分支** `git checkout -b feature/your-feature-name`
3. **提交更改** `git commit -m 'feat: add your feature'`
4. **推送分支** `git push origin feature/your-feature-name`
5. **创建 Pull Request**

## 📋 开发规范

### 代码风格

**Go 后端**
- 遵循 `gofmt` 和 `golint` 标准
- 所有导出函数必须有文档注释：`// FunctionName ...`
- 错误处理使用 `fmt.Errorf("context: %w", err)` 包装
- 单元测试覆盖核心功能

**Vue 前端**
- 使用 `<script setup>` 语法
- 组件名采用 PascalCase
- 使用 yarn 作为包管理器
- ESLint 和 Prettier 保持代码一致性

### 提交消息规范

使用 [Conventional Commits](https://conventionalcommits.org/) 格式：

```
type(scope): description

[optional body]

[optional footer]
```

**类型 (type)**:
- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式调整
- `refactor`: 代码重构
- `test`: 测试相关
- `chore`: 构建/工具链更新

**示例**:
```
feat(chat): add voice input support
fix(ui): resolve bubble position calculation
docs: update installation guide
```

## 🛠️ 开发环境

### 前置条件
- Go 1.22+
- Node.js 16+ + Yarn
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- macOS 10.15+ (当前版本限制)

### 本地开发
```bash
# 1. 克隆你 fork 的仓库
git clone https://github.com/tiancheng92/Aiko.git
cd Aiko

# 2. 安装依赖
go mod download
cd frontend && yarn install

# 3. 启动开发模式
wails dev
```

### 测试
```bash
# 运行 Go 测试
go test ./...

# 前端类型检查
cd frontend && yarn type-check

# 构建验证
wails build
```

## 🎯 贡献领域

我们特别欢迎以下方面的贡献：

### 高优先级
- 🖥️ **Windows 支持** - 点击穿透和窗口管理的 Win32 API 实现
- 🐧 **Linux 支持** - X11/Wayland 桌面环境适配
- 🎙️ **语音功能** - STT/TTS 集成和语音交互
- 🌍 **国际化** - 多语言界面支持

### 欢迎贡献
- 📚 **文档改进** - 使用指南、API 文档、教程
- 🐛 **Bug 修复** - 问题诊断和修复
- ⚡ **性能优化** - 内存占用、启动速度、响应性能
- 🎨 **UI/UX 增强** - 界面优化、交互改进
- 🔧 **新工具** - 内置工具扩展和 MCP 工具开发
- 🎭 **Live2D 模型** - 新角色模型和动画

### 技术专长领域
- **桌面应用开发** (Wails, Electron, Tauri)
- **AI 集成** (LLM, RAG, Agent 框架)
- **前端开发** (Vue 3, CSS 动画, WebGL)
- **系统编程** (CGO, 平台 API, 音频处理)

## 📝 Pull Request 要求

### 提交前检查
- [ ] 代码遵循项目规范
- [ ] 添加必要的测试
- [ ] 更新相关文档
- [ ] 通过所有现有测试
- [ ] 没有引入安全漏洞

### PR 描述模板
```markdown
## 变更类型
- [ ] Bug 修复
- [ ] 新功能
- [ ] 文档更新
- [ ] 性能优化
- [ ] 其他

## 变更说明
简要描述你的修改内容和动机

## 测试
描述如何测试你的变更

## 截图/演示
如果涉及 UI 变更，请提供截图或 GIF

## 检查清单
- [ ] 代码已自测
- [ ] 添加了测试用例
- [ ] 更新了文档
- [ ] 通过了 CI 检查
```

## 🏆 贡献者认可

- 所有贡献者将在项目 README 中列出
- 重大贡献者将获得项目维护者权限
- 优秀贡献将在 Release Notes 中特别感谢

## 🤔 寻求帮助

如果你在贡献过程中遇到问题：

1. 查看 [Documentation](https://github.com/tiancheng92/Aiko/wiki)
2. 搜索现有 [Issues](https://github.com/tiancheng92/Aiko/issues)
3. 创建 Discussion 或 Issue 寻求帮助
4. 联系维护者：xutiancheng92@gmail.com

## 📄 许可协议

通过贡献代码，你同意你的贡献将在 [MIT License](LICENSE) 下进行许可。

---

再次感谢你的贡献！每一个 PR、每一个 Issue、每一个建议都让 Aiko 变得更好。🎉