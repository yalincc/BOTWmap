# BOTWmap 独立语音导航模块 (`app/voice/`)

本目录是 **BOTWmap（网页地图）** 专属的零耦合、可插拔实时语音导航模块（纯网页端收口，不内置于 `xnavi` 桌面定位端）。

## 目录结构

```text
app/voice/
├── voice-nav.js          ← 独立核心引擎（UMD 纯原生 JS，对外暴露 window.VoiceNav）
├── voice-config.json     ← 音色配置与 32 个标准导航词条定义表
├── zelda/                ← 离线语音包 A：塞尔达公主风（中文 32 个 Opus .webm 切片）
├── yujie/                ← 离线语音包 B：御姐领航员（中文 32 个 Opus .webm 切片）
├── en/                   ← 离线语音包 C：英式女声（英文 32 个 Opus .webm 切片，英文界面自动启用）
└── README.md             ← 本说明文档
```

## 核心特性

- **罗盘 8 方位绝对方向**：严格对齐 BOTWmap 坐标系（`mx` 东正西负、`my` 北负南正，`1px = 0.5m`）。
- **距离阶梯与偏航检测**：起步播报、500 / 300 / 100m 里程碑、到达提醒（克洛格 `seed` 与神庙 `shrine` 专属彩蛋）、传送远离 `+80m` 自动重置里程碑、连续背离 >120° 达 6 秒触发偏航纠偏。
- **三级播报兜底**：本地 `.webm` → 线上 `https://botw.yalin.site/voice/` → 浏览器原生 TTS；另内置 Web Audio 希卡之石雷达感应音（`chime`）。
- **双处 UI 同步**：顶栏快捷静音/开启按钮 `#btnVoice` + 设置弹层 `#settingsBox` 音色/音量/试听/播报频次配置，支持中英双语与 `#stReset` 恢复默认。
