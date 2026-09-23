# live-float — 小窗口导航（独立模块）

> 独立于 BotwNavi 与地图 app 的置顶小窗导航。**失败即下架**：不启动 / 删除本目录即完全回退，原项目零改动。

## 依赖（只读，反向零依赖）

| 依赖 | 对象 | 说明 |
|---|---|---|
| 数据 API | `BotwNavi.exe`（live-go/，端口 8766） | /pos /target /config /progress；**启动时自动探测并拉起**（方案 A，`--no-open`） |
| 静态资源 | `app/`（EdgeOne 部署目录）**或线上 botw.yalin.site** | 瓦片 + data.js 标记；**资源基址自适应**：本地 `/app/` 存在走本地（离线/开发），无本地资源自动切 `https://botw.yalin.site/`（发布场景只带 exe 也能用）；`FLOATNAVI_APP_DIR` 环境变量可覆盖地图目录 |

## 使用

```
启动 FloatNavi.exe（唯一入口；自动拉起 BotwNavi，无需手动开两个）
```

## 功能（M5 已验收）

- 地图渲染：瓦片层级随缩放自适应（`z=round(7+log2(SC))`，SC 0.0175~1.0，localStorage 记忆）
- 图标标记：加载 app/js/data.js 全量标记，**图层跟随网页端 `/config` 的 cats**；克洛格/回忆等密集类按缩放分级显示（seed≥0.18、memory≥0.08）
- 滚轮缩放（以玩家为中心）、点图标设目标（经 /target 同源代理）、右键菜单（缩放重置/透明度/图层状态/退出）
- 透明度：页面级 CSS opacity（30~100%），/win/alpha 端点或滑块，localStorage 记忆
- **冻结自检（M5）**：坐标 60s 未更新 → 底部黄条弱提示；180s → 自动触发 `/win/rescan`（限频 5min）；仍无效（300s+）→ 红条提示"请在游戏内保存游戏，再点此重定位"（点击即重定位）
- **数据异常自愈（M5-B）**：单次坐标跳变 >500m（BotwNavi 锁到错误槽的特征）→ 判定异常，**保持原位置不漂移** + 状态栏红字"数据异常 Nm" + 自动 rescan；连续 3 次异常 → **自动重启 BotwNavi**（`/win/restart-navi`，限频 30s）
- **传送不误判（M5-B+）**：单次大跳变（传送/快速旅行）→ **接受新位置**（不提示不重启）；仅"**持续乱跳**"（错误槽特征，连续 3 次 >500m）才自愈重启——区分正常传送与错误槽
- **自愈重启清记忆**：`/win/restart-navi` 重启 BotwNavi 前删除 `live-go/known_addrs.json`（BotwNavi 会把错误槽也记住，重启走"记忆快速路径"直接锁错——清掉后强制全新扫描，`source=scan`）
- **导航线**：目标绿线（虚线）+ 绿点；**无方向箭头**（视野外方向箭头已移除，方向由绿线本身表达）
- 置顶小窗：不抢焦点（WS_EX_NOACTIVATE）、可拖动、位置记忆 data/overlay_pos.json

## 故障恢复（坐标冻结，方案 A 手册 + 方案 B 自动）

**现象**：浮窗金色点不随角色移动（坐标长期不变），但游戏在跑。

**原因**：BotwNavi 长时间运行后内存扫描锁定槽可能失效（返回旧坐标快照）；且扫描以**最近存档位置 ±400m** 为窗口，玩家跑远后重新扫描也找不到活槽。

**处理**（已四档自动 + 手动兜底）：
1. **自动（方案 B）**：60s 黄条提示 → 180s 自动 `/win/rescan` → 300s+ 红条"请游戏内保存再重定位"；**数据异常（持续 >500m 乱跳）→ 保持原位不漂 + 自动重启 BotwNavi（清记忆强制扫描）**
2. **传送识别**：单次大跳变视为正常传送直接接受，不触发任何自愈
3. **手动（方案 A）**：游戏内打开菜单**保存游戏**（刷新存档锚点）→ 点浮窗提示条重定位；或重启 BotwNavi.exe
4. 说明：均不修改 live-go/，全部为本模块能力；BotwNavi 重启后目标(target)会清空属预期

## 模块化铁律

1. 依赖单向：本模块 → BotwNavi / app；二者不感知本模块存在
2. 不修改 live-go/ 与 app/ 的任何文件
3. 页面独立在本目录 web/，配置存 localStorage `botwmap.float.*`

## 已踩坑位（重要，勿重复）

1. **WS_EX_LAYERED 与 WebView2 不兼容**：窗口加 Layered + SetLayeredWindowAttributes 后 WebView2 内容不再合入屏幕（PrintWindow 仍能渲染，误导排查）。透明度一律走页面 CSS opacity，不碰 Win32 Layered。
2. **WS_EX_NOACTIVATE 窗口不合成 `click` 事件**：WebView2 从不激活 → click 永不触发。交互用底层 `pointerdown`（mousedown 亦可）。
3. **WebView2 COM 跨线程调用崩溃**：Eval 必须在创建 WebView2 的线程（UI 线程）。HTTP handler 里直接 Eval 会崩进程。方案：UI 线程执行队列（chan）+ **消息循环用 PeekMessage 轮询**——GetMessageW 在无消息时阻塞会饿死队列，必须 PeekMessage + Sleep(10ms)。
4. **跨端口 CORS**：页面 8777 → BotwNavi 8766，JSON POST 触发 preflight 被拦。GET（/pos 轮询）为简单请求不受影响；POST 走 FloatNavi 同源代理（/target）。
5. **滚动/点击注入坐标**：PrintWindow 位图坐标即窗口客户区坐标，勿再加 canvas 偏移；真实屏幕分辨率（本机 3440×1440）与自动化截图（1920×1080）不一致，验证以 BitBlt/PrintWindow 为准。

开发计划见根目录 `开发计划-小窗口导航v2.md`。
