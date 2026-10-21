# navicemu v1.3.2 版本报告（2026-10-21）

## 1. 版本概述

navicemu 自 2026-10-07 并入 xnavi 后停止常规维护（v1.3.1 收尾）。本版为**应急复用**：
有用户反馈 xnavi 无法定位，老大决定发老版 navicemu 给该用户应急使用，并把 navicemu
缺的手机镜像导航能力一次补齐（对齐 xnavi v3.1.0 的手机镜像三件套）。
**结论：已发布**（`RELEASE/navicemu-v1.3.2-BOTWmap-package.zip`）。定位逻辑零改动。

## 2. 发布内容

- 新增：根路径直达镜像（手机只输 `IP:8766` → 302 跳 `/botw/?follow=1&game=botw`）
- 新增：HTTPS 双协议（certs 自动识别，手机防息屏；可选功能，缺证书自动降级 http）
- 新增：缺图内置引导页（`mirror_fallback.html` 编译进 exe，无 app/ 时替代白屏）
- 变更：exe 同目录 `app/` 优先挂载 + 惰性解析（后补地图刷新即生效）
- 加固：resolveBotwFS 跳过空路径候选（空路径经 `filepath.Abs` 变 cwd，会误挂当前目录）
- 发布包：主程序 + start.bat + 离线地图 app/（V1.7.1，11184 文件）+ certs/ 脚本与说明（不含私钥）
- 轻量单独版：`RELEASE/navicemu-v1.3.2/`（exe + certs + start.bat + README，zip 约 3MB，
  不含离线地图 app/；无 app/ 时自动返回内置引导页，需离线地图请用 BOTWmap-package 版）
- 存档读取加固（10-21 晚）：`--cemu-save-dir=<存档根>` 手动指定唯一根最优先（隔离自动探测、无档即提示）；
  自动扫描收敛为 `%APPDATA%\Cemu` + 运行中 Cemu.exe 所在目录（兼容 mlc01/usr 两种布局），便携版免配自动命中；进度纯 mtime 最新
- 存档可读性（10-22 凌晨）：启动日志升级为 `[progress] save: <完整路径>` + `source: auto/manual(--cemu-save-dir)` + `slot: <槽号>`；
  README 新增"存档（可选：手动指定）"小节，写明参数用法与目录格式

## 3. 技术方案

- 全部改动在 HTTP 服务层（main.go / server.go），定位 trackLoop / progress 零改动。
- 双协议 = xnavi v3.1.0 同款实现：Accept 后预读首字节，`0x16`（TLS ClientHello）→ TLS 通道，
  否则明文通道；连接即关（端口探测）静默丢弃，不刷 TLS 握手错误日志。
- 与 xnavi 的差异：navicemu 无 GUI，改为 **certs/ 自动识别**（xnavi 是 `--tls` 参数 + GUI 勾选）。
- 引导页 `go:embed` 编译进 exe；`/pos.lanUrl` 按证书状态输出 http(s)，网页地图二维码面板直接复用。

## 4. 踩坑记录

| 坑 | 根因 | 修复 | 教训 |
|---|---|---|---|
| overlay 测试变体把路径清空后，地图根被挂成 exe 所在目录 | `filepath.Abs("")` 返回 cwd，`os.Stat(cwd)` 恒成功 | resolveBotwFS 跳过空候选 | 构造"路径不存在"的测试时，空串路径会退化成 cwd，必须显式排除 |
| 本机测试 curl 打不到被测进程 | python 占位 127.0.0.1:8766（精确绑定），navicemu 绑 0.0.0.0:8766（Windows 允许共存），127.0.0.1 优先命中前者 | 用局域网 IP 访问被测端口；测完 taskkill 清理 | Windows 上 python http.server（SO_REUSEADDR）挡不住 Go 的 0.0.0.0 绑定，占位法测端口不可靠 |
| curl 局域网 IP 返回 502/000 | 本机代理拦截 | `curl --noproxy "*"` | 本机有透明代理，测局域网服务必带 --noproxy |
| 改 CHANGELOG 用标题行做锚点，把 V1.7.1 标题吃掉 | Edit 的 old_string 只含标题行、new_string 未回填 | 立即检查补回 | 插入式编辑的锚点要放进 new_string 尾部，别用将被替换的行当锚 |

## 5. 测试与验收

- http：/pos 200（lanUrl 正常）；`/` 302 → 镜像页；`/botw/?follow=1` 200（30KB 地图页）；`/botw/js/live.js` 200。
- https（gen-cert.ps1 生成本机证书，SAN 含 192.168.31.234）：`https://<LAN-IP>:8766/pos` 200 且 lanUrl 输出 https；
  https `/` 302；https `/botw/` 200；同端口 http 照常 200（双协议共存验证）。
- 缺图引导页：overlay 构建清空全部地图路径 → `/` 与 `/botw/` 均返回内置引导页，/pos 照常。
- 未测（留待用户场景）：手机真机 wakeLock 行为、iPhone 证书信任流程（与 xnavi v3.1.0 同一套脚本，已在线上验证过）。

## 6. 遗留与后续

- gen-cert.ps1 依赖 OpenSSL（Git for Windows 自带），终端用户没装时会失败——certs/README.md 已写明并给下载指引；
- navicemu 仍是应急定位方案，用户若能跑通 xnavi 应回归 xnavi（老大口径）；
- `tools/verify.js` 不涉及（无网页代码改动）；

## 7. 回溯

- commits：见 git log（live-cemu/main.go、server.go、mirror_fallback.html、certs/、.gitignore、CHANGELOG.md）
- tag：`navicemu-v1.3.2`
- 发布包：`RELEASE/navicemu-v1.3.2-BOTWmap-package/` + 同名 zip（约 82MB，11190 文件）
- 参照实现：xnavi v3.1.0（CHANGELOG 469 行起）、`RELEASE/xnavi-v3.1.3/certs/`
