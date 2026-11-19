# SnipGo

面向 macOS 的轻量截图与悬浮贴图工具，基于 [MyGo](https://mygo.egoist.dev/docs) 构建。

以 Snipaste 的截图、标注、贴图工作流为核心，参考 Shottr 的轻量体验。菜单栏常驻，默认本地处理，无需主窗口。

## 运行

```sh
go tool mygo dev      # 开发构建，带热重载
go tool mygo vet .    # go vet + 原生 UI 生命周期检查
go test ./...         # 单元测试
SNIPGO_CAPTURE=1 go test -run TestGrabRealDisplays ./internal/capture/  # 真机截屏（需已授予屏幕录制权限）
go tool mygo build    # 生成 .app / .dmg
```

需要 macOS 14+（ScreenCaptureKit `SCScreenshotManager`）。首次使用需在系统设置中授予屏幕录制权限，授权后重开应用。

## 自动打包与发版（GitHub Actions）

仓库内置两条自动化流水线：

- **日常构建与校验（`.github/workflows/ci.yml`）**：每次推送 `main`、发起 PR 或手动触发 `workflow_dispatch` 时，自动执行全套单测、`@2x` 渲染回归、`mygo vet` 与跨平台静态检查，并打包 `arm64` / `amd64` / `universal` 三套 `.dmg` 与 `.app.zip` 上传至 Actions Artifacts。
- **标签自动发版（`.github/workflows/release.yml`）**：推送 `v*` 标签（或在 Actions 页面手动填入版本号触发）即可自动构建并发布 GitHub Release（附带 `SHA256SUMS.txt`）：
  ```sh
  git tag v0.1.0
  git push origin v0.1.0
  ```
- **可选 Developer ID 签名与 Apple 公证**：未配置证书机密时默认使用 ad-hoc 签名打包；若在仓库 Secrets 中配置 `MACOS_CERTIFICATE`（Base64 `.p12`）、`MACOS_CERTIFICATE_PASSWORD`、`MACOS_SIGNING_IDENTITY`（以及公证可选的 `APPLE_ID`、`APPLE_TEAM_ID`、`APPLE_APP_PASSWORD`），流水线会自动导入临时钥匙串完成正式签名与公证。

## 当前状态

已实现对齐 **Snipaste + Shottr + CleanShot** 核心体验的全功能截图与贴图闭环：

- **实时透明框选（`Cmd+Shift+2`，默认）**：按下快捷键 **0ms 直接弹出透明选区蒙层**，选区内透出原生真实桌面；移动鼠标自动命中 Z 序最上层可见窗口，**单击**锁定窗口，**拖拽**自由框选；支持实时按 **`C`** 复制 `#RRGGBB`（`Shift+C` 复制 `R, G, B`）。
- **冻结屏幕截图（`Ctrl+Cmd+2`）& 延时倒计时定格（`Ctrl+Shift+Cmd+2`）**：定格全屏画面再框选（支持 3s / 5s / 10s 倒计时从容展开右键菜单与悬停浮层），内置 `11×11` 像素放大镜与 **`R` 键十字射线像素测量尺**（自动吸附 UI 边缘测量宽高间距，单击复制 `W × H px`）。
- **截图内矢量标注（`1`~`9`）& 点选二次编辑 & 一键敏感脱敏（`X`）**：
  - 支持**矩形框（`1`）**、**真椭圆框（`2`）**、**流线型实心箭头（`3`）**、**均匀半透明荧光笔（`4`）**、**高对比度递增序号 `①②③`（`5`）**、**像素马赛克（`6`）**、**原生字体文字批注（`7`）**、**聚光灯高亮暗角（`8`）**、**四边插值无痕擦除（`9`）**。
  - **一键敏感信息自动脱敏（`X`）**：基于离线 `Vision` 文本框定位自动识别 `sk-...` / `ghp_...` / `AKIA...` 密钥、手机号、邮箱、身份证与内网 IP，原地生成可撤销的无痕擦除框。
  - **图元点选二次编辑**：单击已有标注即可平移、拖拽首尾控制点 `P0`/`P1`、实时换色改线宽、按 `Delete`/`⌫` 删除，支持多步快照 `Cmd+Z` / `Shift+Cmd+Z` 撤销重做。
- **滚动长截图（`L`）**：穿透绿色采集框自由滚动网页/代码/聊天记录，自动剥离顶部/底部固定栏与右侧滚动条，按高方差纹理带自动对齐与 4px 接缝羽化拼接，右侧实时显示长图预览。
- **离线极速 OCR 识字 & 二维码秒解（`O` / 全局 `Alt+Cmd+O`）**：调用 macOS 原生 `Vision.framework` 离线识别中英混排文字与二维码（小选区自动留白放大增强），弹出可编辑悬浮识图面板并支持一键复制或打开链接。
- **纯净单窗抠图（`W`）& 一键渐变美化卡片（`B`）**：锁定窗口后开启「单窗」通过 `SCContentFilter(desktopIndependentWindow)` 剔除壁纸与遮挡窗口；开启「美化」自动合成圆角 + 居中柔和投影 + 4 套渐变背景。
- **贴图进阶全家桶**：
  - **万物皆可贴（`Alt+Cmd+2`）**：剪贴板图片、本地图片、代码/文本片段（悬浮便签卡片）、`#RRGGBB` / `rgb()` 色号（设计师色卡）一键变贴图。
  - **原地二次矢量标注（`E`）& 原生 PNG 拖出**：悬浮贴图可随时进入非破坏性矢量标注模式继续增删修改；按住贴图直接拖出原生 `.png` 文件到微信、Slack、GitHub 或 Finder。
  - **鼠标点击穿透（`P`）、迷你胶囊折叠（`M`）& 关闭历史恢复（`Shift+Alt+Cmd+2`）**：支持半透明置顶逐像素比对前端稿，按 `Ctrl+Alt+Cmd+2` 一键恢复所有穿透贴图。
- **最近截图历史抽屉（`Shift+Cmd+H`）& 偏好设置（`Cmd+,`）**：本地环形缓存最近 20 张截图（双列网格预览、直接拖出 PNG、重复制/贴图/识图/删除），支持配置 **`Cmd+S` 免弹窗一键快存**、保存目录与默认倒计时秒数。

| 包 | 职责 |
|---|---|
| `main.go` / `overlay.go` / `pin.go` / `scroll_ui.go` / `history.go` / `settings.go` / `ocr.go` | 菜单栏、全局快捷键、选区蒙层、滚动长图采集窗、悬浮贴图（含原地重编辑与文件拖出）、识图面板、历史抽屉与偏好设置 |
| `internal/session` | 截图状态机：Idle → Capturing → Selecting → Adjusting，窗口命中、图元点选二次编辑、单窗/美化/OCR/标尺/历史钩子编排 |
| `internal/annotate` | 9 种矢量/像素标注图元（含聚光灯与无痕擦除）、命中选中与控制点编辑、快照撤销/重做栈与 Retina 抗锯齿栅格化 |
| `internal/scrollcap` | 滚动长截图拼接引擎：固定头尾栏与右侧滚动条剥离、底部高方差纹理行匹配与接缝羽化融合 |
| `internal/vision` | macOS `Vision.framework` 离线中英混排 OCR、文本框归一化坐标回映射、敏感信息自动检测与 QR 二维码识别 |
| `internal/store` | 本地最近截图环形持久化仓库（`index.json` + PNG 自动淘汰与快速索引） |
| `internal/config` | 用户偏好设置持久化（免弹窗快存、保存时自动复制、快存目录、延时截图秒数等） |
| `internal/beautify` | 圆角居中柔和阴影渐变背景卡片合成、剪贴板文本/代码便签与 `#HEX` 色卡渲染 |
| `internal/measure` | 四向颜色差分边缘吸附像素测量尺 |
| `internal/textrender` | macOS CoreGraphics + `NSFont` 离屏中英/等宽字体高分屏渲染 |
| `internal/capture` | ScreenCaptureKit 屏幕/独立单窗采集与 CoreGraphics Z 序遮挡感知窗口探测 |
| `internal/crop` | DIP 选区 → 像素裁剪与尺寸预算，跨屏按最大缩放最近邻拼接 |

## 路线图

每个里程碑以“可在真机验收”为完成标准，前一个未验收不开始下一个。

### M0 闭环加固（进行中）

目标：现有闭环在真实使用环境里可靠，后续功能不在不稳的地基上叠加。

- [x] 选区蒙层层级：提升至 `kCGStatusWindowLevel + 2`（27），盖住实时菜单栏（24）、状态栏（25）、Dock（20）与已有悬浮贴图（3），同时保留中文输入法候选窗（101）正常浮于蒙层之上。
- [x] 截图排除旧蒙层：按本进程 PID 与选区窗口标题 `excludingWindows:` 过滤未关完的旧蒙层，保留贴图入镜；贴图是否入镜做成后续设置项。
- [ ] 场景矩阵真机手测：单屏 / 双屏不同缩放 / 外接屏在主屏左侧或上方（负坐标）/ 全屏应用与其他 Space / 权限被撤销 / 连按快捷键 / 截图中途退出。
- [ ] 建立 `go test` + `mygo vet` 的提交前检查。

验收：上述矩阵逐项手测通过，结果记录在本节。

### M1 选区体验

目标：从“拖完即出图”变成“选好再决定做什么”，这是标注、保存等后续功能的入口。

- Session 增加 `Adjusting` 阶段：松手后保留选区，出现工具栏（复制 / 保存 / 贴图 / 取消），Enter 默认复制，双击贴图。
- 选区可移动、8 向手柄缩放、方向键 1px 微调（Shift 10px）。
- 单击选中鼠标下的窗口：用 `SCShareableContent` 的窗口 frame 做命中，悬停时高亮。
- 放大镜 + 选区尺寸（DIP 与像素）显示。

验收：多屏下调整后的选区与输出像素一致（扩充 `crop` 测试），窗口识别在常见应用上命中正确。

### M2 标注

目标：在 Adjusting 阶段内完成常用标注，输出结果含标注。

- 新增 `internal/annotate`：标注是矢量操作列表（矩形、椭圆、箭头、画笔、文字、马赛克/模糊、序号），支持撤销/重做；导出时按目标缩放栅格化到 NRGBA。
- UI 只负责编辑和预览，栅格化逻辑放在 Go 包内可单测。
- 颜色、线宽记住上次选择。

验收：标注在 1x / 2x / 跨屏缩放下导出位置和粗细一致（像素级测试）。

### M3 贴图增强

- 滚轮缩放、`Cmd+±` 缩放，透明度调节，鼠标穿透（带全局快捷键恢复）。
- 右键菜单：复制、保存、透明度、穿透、关闭、关闭全部。
- 双击贴图再次进入标注；贴图保持原始像素，缩放时不重复失真。
- “剪贴板内容直接贴图”快捷键。

### M4 保存、历史与设置

- 保存为 PNG 到默认目录，文件名模板；可选同时复制。
- `internal/store`：本地截图历史（数量/容量上限），可恢复上次关闭的贴图。
- 设置窗口：自定义快捷键、保存目录、开机启动、截图是否包含贴图。

### M5 分发

- 签名与公证，屏幕录制权限的首次引导完善，`mygo keygen` 配置自动更新。

### 之后再评估

OCR（Vision）、取色与测量、滚动截图。不做：录屏、云空间、团队协作。

## 技术约束

- MyGo 的 `CapturePage()` 只捕获自身窗口，桌面采集必须走 ScreenCaptureKit。
- 坐标统一为 DIP、主屏左上角为原点、y 向下；像素换算只经过 `crop.DIPToPixel`。
- ScreenCaptureKit 调用必须固定 OS 线程（自动释放池按线程生效）；回调超时后由后台 goroutine 收尾释放。
- 默认框选使用透明蒙层并在确认时通过 `SCStreamConfiguration.sourceRect` 只采选区像素；冻结模式只读定格像素，不二次截屏。
