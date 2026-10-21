# enavi-gui · Eden 实时定位（测试界面）

《塞尔达传说：旷野之息》**Eden 模拟器**只读实时定位器的桌面界面 —— 给
[`enavi`](../enavi/) 定位核心套一层可观测的壳，方便实机反复测试。

网页地图：<https://botw.yalin.site>

---

## 它解决什么问题

enavi 原本是「双击 `M1.bat` → 盯黑窗口」的形态。排查「锁没锁对」「为什么掉线」
只能靠翻日志文本。enavi-gui 把定位过程**摊开成可读的状态**：

- 当前锁的是**哪个地址**（hex）、扫了几次、缓存写了几次；
- `verified` 是真玩家还是还没走动确认；
- **神庙挂起**状态一眼可见（进庙时红点钉门口，网页端不再掉线）；
- 实时日志按级别着色，失败/重扫/锁定分色。

---

## 双入口独立

```
┌──────────────┐   启停子进程      ┌───────────────┐
│ enavi-gui    │ ────────────────► │ enavi.exe     │
│ (Wails GUI)  │ ◄──────────────── │ (定位核心)    │
└──────────────┘  读 status.json   └───────────────┘
                   读 enavi.log            │
                   写 rescan.req           ▼ HTTP :8766 /pos
                                       https://botw.yalin.site
```

- **GUI ↔ 核心只走文件**（`status.json` / `enavi.log` / `rescan.req`），
  **不依赖 8766**。端口被别的程序占了 → 网页端不可用，GUI 照常能定位、能看日志、能重扫。
- **核心 ↔ 网页** 仍是 8766 HTTP `/pos`（协议 v1，`app/js/live.js` 零改动）。

---

## 使用

1. 启动 Eden，进《旷野之息》，读到存档，人物能走动；
2. 双击 `enavi-gui.exe`（需与 `enavi.exe` **同目录**）；
3. 点 **▶ 开始定位**；
4. 状态条走到「已锁定」即定位成功，红点会出现在网页地图上。

> Eden 的 BOTW 存档目录**留空即可自动探测**。探测不到时点「设置」手动指定
> （点「浏览」选到 `…\user\nand\user\save` 这一层即可）。

### 界面说明

```
┌──────────────────────────────────────────────────────┐
│ ● Enavi · Eden 定位伴侣 v0.1.0      ●定位中 ⚙ 🌐 ℹ   │
├──────────────┬────────┬──────────────────────────────┤
│ 运行环境      │ ▶开始  │ 定位状态  [未验证]            │
│ Eden 存档目录 │  定位  │ X/alt/Z · 地图像素 · 地址     │
│ PID · Web可用 │        │ 扫描/写入 · 锚点 · 神庙挂起  │
├──────────────┴────────┴──────────────────────────────┤
│  ● 就绪 — ● 扫描中 — ● 验证移动 — ● 已锁定           │
├──────────────────────────────────────────────────────┤
│ 实时日志              清屏 导出诊断包 打开目录 重新扫描│
│ [11:21:58] [attach] pid=6800 G:\YUZU\eden\eden.exe  │
│ [11:22:04] [locate] 锁定失败：User Heap 为空…         │
└──────────────────────────────────────────────────────┘
```

- **开始/停止**：启停定位核心（停止会释放 8766 端口）；
- **重新扫描**：手动重扫（写 `rescan.req`，核心下一拍消费）；
- **导出诊断包**：打包 `status.json` + `enavi.log` + `known_player.json` + 系统信息，
  出问题时把这个 zip 发回来即可；
- **打开目录**：打开工作目录（所有日志/缓存都在里面）。

---

## 构建

```powershell
$env:Path = 'D:\tools\go\bin;D:\tools\go\gopath\bin;' + $env:Path
Set-Location E:\WorkSpace\BOTWmap\enavi-gui
& wails build -skipbindings     # 前端已构建过 dist，-skipbindings 更快
```

前端单独构建（在 `frontend/` 下）：

```powershell
cd E:\WorkSpace\BOTWmap\enavi-gui\frontend
npm install
npm run build          # → frontend/dist（wails 会嵌进 exe）
```

> `frontend/wailsjs/` 是 Wails 生成的绑定，**不入库**（每次 `wails build` 重新生成）。
> 改了 Go 后端的方法签名后，用 `wails generate module` 重新生成绑定。

核心 `enavi.exe` 单独构建（放在 GUI 同目录）：

```powershell
Set-Location E:\WorkSpace\BOTWmap\enavi
& D:\tools\go\bin\go.exe build -trimpath -ldflags "-s -w" -o enavi.exe .\cmd\enavi
```

---

## 目录

| 路径 | 作用 |
|---|---|
| `main.go` | Wails 入口（窗口标题/尺寸、embed 前端） |
| `app.go` | Wails 后端：启停核心、读状态/日志、重新扫描、诊断包 |
| `frontend/src/App.vue` | 主界面 |
| `frontend/src/bridge.js` | Wails API/事件适配层 + 浏览器预览 mock |
| `frontend/dist/` | 前端构建产物（embed 进 exe，不入库） |

---

## 数据与隐私

- 全部在本机：只读 Eden 进程内存、只读本地存档、只监听 `127.0.0.1:8766`。
- **只申请 `PROCESS_QUERY_INFORMATION | PROCESS_VM_READ`**，不写内存、不注入、不改存档。
- 不联网上传任何数据。「打开地图」只是用浏览器打开公开网页。

## 相关文档

- 定位原理与状态机规范：[`docs/spec/ENAVI-HANDBOOK.md`](../docs/spec/ENAVI-HANDBOOK.md)
- 定位规则与阈值总表：[`docs/spec/LOCATE-RULES.md`](../docs/spec/LOCATE-RULES.md)
- 核心说明：[`enavi/README.md`](../enavi/README.md)
- 本轮方案：[`PLAN-ENAVI-GUI.md`](../PLAN-ENAVI-GUI.md)
