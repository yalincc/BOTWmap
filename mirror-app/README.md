# xnavi-mirror — 手机镜像壳 APP

一个 ~18KB 的极简安卓 APP（零第三方依赖）：**粘贴链接打开 xnavi 局域网镜像页 + 屏幕常亮 + 打开前探测 xnavi 是否在线**。

## 为什么需要它

手机浏览器访问 http 局域网链接时，浏览器禁用 Screen Wake Lock（仅 https 可用），
页面必然自动熄屏。本 APP 用安卓系统级 `FLAG_KEEP_SCREEN_ON` 保持常亮——
**不需要 https、不需要装证书**。WebView 加载 http 明文（仅局域网）已显式放行。

## 使用

1. 电脑启动 xnavi（开始定位），复制它显示的镜像链接（`http://电脑IP:8766/botw/...`）发到手机（微信文件传输助手/QQ 等）；
2. 手机安装 `xnavi-mirror.apk`，粘贴链接（或只输 `电脑IP:8766`）→ 点「打开」；
3. APP 先探测 xnavi 是否在线：
   - 在线已锁定 → 直接进入，屏幕保持常亮；
   - 在线未锁定 → 照常进入（页面会显示「定位中」，动一下角色即可）；
   - 连不上 → 不进入，提示三查（xnavi 启动 / 同一 WiFi / 地址正确）；
4. 链接被记住，下次启动自动探测直达（连不上回落首页并预填，不会卡死）。
5. 返回键：网页后退；退无可退再按一次回首页；首页再按一次退出。

## 构建

环境（本机已就绪）：JDK 17（`JAVA_HOME`）+ Android SDK（`D:/dev/AndroidSdk`）+ Gradle 9.3.1（`D:/dev/.gradle` 缓存）。

```bash
pwsh -NoProfile -Command '$env:JAVA_HOME="D:/dev/jdk17/jdk-17.0.20.1+1"; & "D:\dev\.gradle\wrapper\dists\gradle-9.3.1-all\5wooodlkfwsm42ictrailuf0e\gradle-9.3.1\bin\gradle.bat" -p "E:\WorkSpace\BOTWmap\mirror-app" assembleDebug'
# 产物：app/build/outputs/apk/debug/app-debug.apk
```

- 依赖走腾讯/阿里镜像（`settings.gradle` 已配）；AGP 8.13.0 本机已缓存。
- release 签名：正式发布前生成 keystore（`app/build.gradle` release 块预留 minify）。

## 技术

- 纯 Java 单 Activity（无 androidx 直接依赖）+ zxing-android-embedded 扫码 + 系统 WebView
- minSdk 24（Android 7）/ target & compile 35
- `usesCleartextTraffic="true"`：仅用于局域网 http，不做公网用途
- `domStorageEnabled` 开启（镜像页 localStorage 存跟随开关/完成度标记）

## 边界（诚实声明）

- **iOS 无对应 APP**：App Store 拒纯 WebView 壳、TestFlight 90 天折腾。iOS 用户走
  「设置 → 显示与亮度 → 自动锁定 → 永不」，或装 `certs/rootCA.pem` 走 https + Wake Lock。
- 息屏（主动按电源键）后 WebView JS 仍会被系统冻结——「亮屏使用中不熄屏」已解决；
  「息屏后台持续更新」需前台服务，留 v1.1（见 `docs/backlog/PLAN-MIRROR-APP.md`）。
