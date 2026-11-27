# xnavi-gui 构建与图标规范（AI & 人工必读）

本目录是 BOTWmap 导航程序（模拟器版 `xnavi` + PC 移植版 `xnavi-pc`）共用的 **Wails GUI 外壳**，通过 Go build tag 区分两个版本：
- 模拟器版（Cemu / Ryujinx / Eden）：`edition_emu.go`（默认 `!pc`）
- PC 移植版（Savage Wind）：`edition_pc.go`（`-tags pc`）

> 图标更换与发布归档完整 SOP 见 `docs/spec/DEV-HANDBOOK.md §7.1`。

---

## 一、构建铁律（禁区清单）

1. **严禁直接用 `go build` 编译 `xnavi.exe`**：直接 `go build` 会跳过 Wails 的 `.syso` 资源生成，导致生成的 `xnavi.exe` **没有程序图标和 Windows manifest**（v3.3.3 踩过坑）。**必须用 `wails.exe build`**。
2. **严禁加 `-clean` 参数**：`wails build -clean` 会清空 `build\bin\` 里的配套文件。
3. **每次构建完必须验图标**：不能靠肉眼或文件大小/时间戳判断，必须运行 `tools\check-exe-icon.py` 看到 **`==> MATCH`** 才算合格。
4. **两版本共用同一输出路径**：产物都在 `build\bin\xnavi.exe`。同时打模拟器版和 PC 版时，**编完一个立刻拷走并验图标，再编另一个**，切勿串包。

---

## 二、标准构建命令（PowerShell 直接执行）

### 1. 构建 GUI 外壳 `xnavi.exe`

**模拟器版（emu，配 `xnavi\` 核心）：**
```powershell
$env:Path = 'D:\tools\go\bin;' + $env:Path
Set-Location E:\WorkSpace\BOTWmap\xnavi-gui
& 'D:\tools\go\gopath\bin\wails.exe' build -skipbindings
python E:\WorkSpace\BOTWmap\tools\check-exe-icon.py E:\WorkSpace\BOTWmap\xnavi-gui\build\bin\xnavi.exe E:\WorkSpace\BOTWmap\xnavi-gui\build\windows\icon.ico
```

**PC 移植版（pc，配 `xnavi-pc\` 核心）：**
```powershell
$env:Path = 'D:\tools\go\bin;' + $env:Path
Set-Location E:\WorkSpace\BOTWmap\xnavi-gui
& 'D:\tools\go\gopath\bin\wails.exe' build -skipbindings -tags pc
python E:\WorkSpace\BOTWmap\tools\check-exe-icon.py E:\WorkSpace\BOTWmap\xnavi-gui\build\bin\xnavi.exe E:\WorkSpace\BOTWmap\xnavi-gui\build\windows\icon.ico
```

> 若修改了 Go ↔ 前端绑定方法（`app.go` 导出签名变化），去掉 `-skipbindings` 跑一次完整 `wails.exe build` 即可。

### 2. 构建配套核心 `xnavi-core.exe`

```powershell
# 模拟器版核心（Cemu / Ryujinx / Eden）
Set-Location E:\WorkSpace\BOTWmap\xnavi
& D:\tools\go\bin\go.exe build -trimpath -ldflags "-s -w" -o xnavi-core.exe .

# PC 移植版核心（Savage Wind）
Set-Location E:\WorkSpace\BOTWmap\xnavi-pc
& D:\tools\go\bin\go.exe build -trimpath -ldflags "-s -w" -o xnavi-core.exe .
```

---

## 三、图标规范

- **唯一生效图标源**：
  - `xnavi-gui\build\windows\icon.ico`（Windows exe 内嵌图标，7 帧多尺寸：`16/24/32/48/64/128/256`）
  - `xnavi-gui\build\appicon.png`（应用图标源）
- **图标校验命令（必跑）**：
  ```powershell
  python E:\WorkSpace\BOTWmap\tools\check-exe-icon.py <目标xnavi.exe> E:\WorkSpace\BOTWmap\xnavi-gui\build\windows\icon.ico
  ```
  - 正常输出：`==> MATCH: exe 内嵌图标 = 指定 icon.ico`
  - 若报错 `KeyError: 0`：说明 exe 完全没有 PE 图标资源，一定是误用了 `go build` 而非 `wails build`！
- **发布包内不放 `.ico` 文件**：图标已内嵌到 `xnavi.exe`，不要把 `icon.ico` 拷进 `RELEASE\` 压缩包。

---

## 四、打包前 4 项必检清单

1. **图标校验**：目标发布目录下的 `xnavi.exe` 跑 `check-exe-icon.py` 输出 `MATCH`。
2. **版本配对**：
   - `xnavi-vX.Y.Z`（模拟器版）= `wails build -skipbindings` 编出的 `xnavi.exe` + `xnavi\xnavi-core.exe`
   - `xnavi-vX.Y.Z-PC`（PC 版）= `wails build -skipbindings -tags pc` 编出的 `xnavi.exe` + `xnavi-pc\xnavi-core.exe`
3. **无运行残留**：打包前确认 `RELEASE\` 目录内不含 `status.json`、`xnavi-gui-core.log`、`xnavi-pc-chain.json` 等运行时生成文件。
4. **先杀进程再覆盖**：若本机正在运行 `xnavi.exe` / `xnavi-core.exe`，先结束进程再替换，避免写入失败。
