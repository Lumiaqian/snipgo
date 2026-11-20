# 更新记录

## 0.1.3

### 核心亮点
- 全面重构「识图提取」窗口与 Liquid Glass 液态玻璃控件：去除原生玻璃叠加双层描边与硬分割线，升级 macOS 26 风格透亮分段滑块，浅/深模式视觉更统一轻盈
- 识图表格模式（Markdown / 表格 TSV）默认以可视化交互表格呈现，支持点击任意单元格单独复制，并可一键切换等宽「源码」编辑
- 修复 Apple Silicon 下误装 Rosetta x86_64 版本导致 macOS Vision OCR 报错（`CRImageReaderError error 9`）的问题，新增 Rosetta 2 架构提示、独立文本/二维码识别通道与 CPU 自动回退
- 优化 Release 安装包命名，清晰区分 `AppleSilicon-arm64`、`Intel-amd64` 与 `Universal` 版本

## 0.1.2

### 核心亮点
- 迁移至 MyGo 官方 `mygo.Updater` 签名更新内核：稳定通道直接读取 GitHub Releases 静态清单 `update-darwin-*.json`，彻底避免 GitHub REST API 403 限流
- 全量更新归档（`.tar.gz`）与 `bsdiff` 增量补丁（`.delta`）启用 Ed25519 非对称数字签名校验，同时保留原生 Liquid Glass 更新窗口

## 0.1.1

### 核心亮点
- 全新 macOS 26 Tahoe Liquid Glass 液态玻璃浮窗、弹簧过渡动效、主题强调色与三档玻璃浓度调节
- 全套悬浮面板支持原生左上角红灯关闭（`⌘W` / `Esc`）与按键录制式全局快捷键设置
- 新增 10 套高颜值「套模板工作台」、离线 AI 主体抠图（`K`）、二维表格 OCR 与局部放大镜（`0`）

## 0.1.0

- 首个公开版本：0ms 透明框选、10 种矢量标注、离线 OCR 识图、滚动长截图与悬浮贴图全家桶
