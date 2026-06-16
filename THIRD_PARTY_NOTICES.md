# Third-Party Notices

PrismCast 在运行时或构建时使用了以下第三方软件。除另有说明外，PrismCast 本体代码以 [Apache-2.0](LICENSE) 发布；下列组件保留其各自许可证。

## 运行时依赖（Go）

| 组件 | 许可证 | 说明 |
|------|--------|------|
| [Wails v2](https://github.com/wailsapp/wails) | MIT | 桌面 GUI 框架（WebView2） |
| [gorilla/mux](https://github.com/gorilla/mux) | BSD-3-Clause | HTTP 路由 |
| [fyne.io/systray](https://github.com/fyne-io/systray) | BSD-3-Clause / Apache-2.0 | 系统托盘 |
| [google/uuid](https://github.com/google/uuid) | BSD-3-Clause | UUID 生成 |

完整依赖树见 `prismcast/go.sum`。

## 前端构建依赖（npm，编译进发布包）

| 组件 | 许可证 | 说明 |
|------|--------|------|
| [Vue 3](https://github.com/vuejs/core) | MIT | UI 框架 |
| [Vue Router](https://github.com/vuejs/router) | MIT | 前端路由 |
| [Vite](https://github.com/vitejs/vite) | MIT | 构建工具（仅开发/构建阶段） |

## 随应用分发的第三方文件

### hls.js

- **文件**：`prismcast/internal/dlna/hls.min.js`
- **项目**：https://github.com/video-dev/hls.js
- **许可证**：Apache License 2.0
- **用途**：无 MPV 时，浏览器 HLS 播放回退
- **许可证全文**：见 `prismcast/third_party/hls.js/LICENSE`

## 外部可选组件（不随 PrismCast 源码/安装包捆绑）

| 组件 | 说明 |
|------|------|
| **mpv** | 推荐播放器；用户自行安装，遵循 mpv 自身许可证 |
| **Microsoft Edge WebView2 Runtime** | Wails 在 Windows 上依赖；由 Microsoft 提供 |
| **系统默认播放器 / 图片查看器** | Windows 关联程序，非 PrismCast 分发 |

## 协议与商标

- **UPnP / DLNA**：PrismCast 实现 UPnP AV MediaRenderer 相关能力，与 DLNA 指南兼容；**并非** DLNA 官方认证产品。
- **DLNA** 为 Digital Living Network Alliance 商标；本项目未获该商标授权，亦不代表官方认证。

## 图标与品牌

- 应用图标与 UI Logo（`PrismCast_LOGO.png`、`frontend/src/assets/logo.png` 等）为 PrismCast 项目自有资产，随 Apache-2.0 一并发布。
