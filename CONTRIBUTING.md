# 贡献指南

感谢你对 PrismCast 项目的关注！我们欢迎各种形式的贡献，包括但不限于：

- 提交 Bug 报告
- 提出新功能建议
- 改进文档
- 提交代码修复或新功能
- 分享使用经验

## 如何贡献

### 报告问题

如果你发现了 Bug 或有功能建议，请通过 [GitHub Issues](https://github.com/hezimy/PrismCast/issues) 提交。

提交问题时，请尽可能提供以下信息：

- **问题描述**：清晰描述问题或建议
- **复现步骤**：如果是 Bug，请提供详细的复现步骤
- **环境信息**：
  - Windows 版本
  - PrismCast 版本
  - 是否安装 MPV
- **日志文件**：如有必要，请提供 `%TEMP%\PrismCast\prismcast.log` 中的相关日志

### 提交代码

1. **Fork 仓库**
   ```bash
   git clone https://github.com/hezimy/PrismCast.git
   cd PrismCast
   ```

2. **创建分支**
   ```bash
   git checkout -b feature/your-feature-name
   # 或
   git checkout -b fix/bug-description
   ```

3. **开发环境搭建**
   ```bash
   cd prismcast
   go mod tidy
   cd frontend && npm install && cd ..
   wails dev
   ```

4. **提交更改**
   - 确保代码可以正常编译
   - 遵循现有的代码风格
   - 提交信息使用中文或英文，清晰描述更改内容

5. **推送并创建 Pull Request**
   ```bash
   git push origin feature/your-feature-name
   ```
   然后在 GitHub 上创建 Pull Request。

## 代码规范

### Go 代码

- 使用 `gofmt` 格式化代码
- 遵循 [Effective Go](https://go.dev/doc/effective_go) 指南
- 为新功能添加适当的注释

### 前端代码

- 使用 2 空格缩进
- 遵循 Vue 3 组合式 API 风格
- 组件名使用 PascalCase

## 提交信息规范

提交信息格式：

```
<type>: <subject>

<body>
```

**类型 (type)：**

- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式调整（不影响功能）
- `refactor`: 代码重构
- `perf`: 性能优化
- `test`: 测试相关
- `chore`: 构建过程或辅助工具的变动

**示例：**

```
feat: 添加开机自启功能

- 在设置页面添加自启开关
- 实现 Windows 注册表操作
- 添加相应的配置持久化
```

## 开发注意事项

### 项目结构

```
prismcast/
├── internal/
│   ├── config/     # 配置管理
│   ├── dlna/       # DLNA 服务核心
│   ├── player/     # 播放器控制
│   └── applog/     # 日志系统
└── frontend/       # Vue 3 前端
```

### 测试

- 为关键功能添加单元测试
- 手动测试 DLNA 投屏功能
- 测试不同播放器回退路径

## 许可证

通过提交 Pull Request，你同意你的贡献将在 [Apache License 2.0](LICENSE) 下授权。

## 获取帮助

如有任何问题，可以通过以下方式联系：

- 在 [GitHub Discussions](https://github.com/hezimy/PrismCast/discussions) 发起讨论
- 发送邮件至项目维护者

再次感谢你的贡献！
