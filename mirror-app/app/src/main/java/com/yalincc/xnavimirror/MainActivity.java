package com.yalincc.xnavimirror;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.os.Bundle;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.WindowManager;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;

/**
 * xnavi-mirror：手机镜像壳 APP。
 *
 * 唯一职责：全屏 WebView 打开 xnavi 局域网镜像页 + 系统级屏幕常亮。
 * 背景：浏览器规定 Wake Lock 仅 https 可用，http 局域网链接在浏览器里必然自动熄屏；
 * 本 APP 用 FLAG_KEEP_SCREEN_ON（安卓系统级 flag，不依赖 https）解决。
 *
 * 交互（极简）：粘贴电脑 xnavi 显示的镜像链接（或只输 IP:端口）→ 打开。
 * 打开前先探测 http://host:port/pos：
 *   - ok:true      → xnavi 在线已锁定 → 直接进入
 *   - ok:false     → xnavi 在线但未锁定 → 照常进入（页面自己会显示「定位中」）
 *   - 连接失败      → 不进入，提示检查 xnavi / WiFi / 地址
 * 链接记住，下次启动自动探测直达。返回键：网页后退 → 首页 → 双击退出。
 */
public class MainActivity extends Activity {

    private static final String PREFS = "xnavi_mirror";
    private static final String KEY_URL = "last_url";
    /** 输入内容若不带镜像路径，自动补默认入口 */
    private static final String DEFAULT_PATH = "/botw/?follow=1&game=botw";

    private WebView web;
    private LinearLayout home;
    private EditText ipInput;
    private TextView status;
    private Button openBtn;
    private long lastBackTs = 0;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        // 核心功能：系统级屏幕常亮（前台期间不熄屏，不依赖 https）
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        String last = getSharedPreferences(PREFS, MODE_PRIVATE).getString(KEY_URL, null);
        showHome(last);
        if (last != null && !last.isEmpty()) {
            probeAndOpen(last);   // 记住过链接：自动探测直达，失败回落首页
        }
    }

    /* ---------- 首页（纯代码布局，无 XML） ---------- */

    private void showHome(String preset) {
        hideSystemUi();
        int pad = dp(24);
        home = new LinearLayout(this);
        home.setOrientation(LinearLayout.VERTICAL);
        home.setGravity(Gravity.CENTER);
        home.setBackgroundColor(Color.parseColor("#121212"));
        home.setPadding(pad, pad, pad, pad);

        TextView title = new TextView(this);
        title.setText("Xnavi Mirror");
        title.setTextColor(Color.WHITE);
        title.setTextSize(26);
        title.setGravity(Gravity.CENTER);
        home.addView(title);

        TextView hint = new TextView(this);
        hint.setText("把电脑 xnavi 显示的镜像链接粘贴到这里\n（或只输 电脑IP:8766）");
        hint.setTextColor(Color.parseColor("#9E9E9E"));
        hint.setTextSize(14);
        hint.setGravity(Gravity.CENTER);
        hint.setPadding(0, dp(10), 0, dp(16));
        home.addView(hint);

        ipInput = new EditText(this);
        ipInput.setHint("http://192.168.x.x:8766/botw/...");
        ipInput.setTextColor(Color.WHITE);
        ipInput.setHintTextColor(Color.parseColor("#757575"));
        ipInput.setSingleLine(true);
        home.addView(ipInput);
        if (preset != null && !preset.isEmpty()) ipInput.setText(preset);

        openBtn = new Button(this);
        openBtn.setText("打开");
        openBtn.setOnClickListener(v -> {
            String s = ipInput.getText().toString().trim();
            if (s.isEmpty()) {
                Toast.makeText(this, "请先粘贴链接或输入 IP:端口", Toast.LENGTH_SHORT).show();
                return;
            }
            probeAndOpen(normalize(s));
        });
        home.addView(openBtn);

        status = new TextView(this);
        status.setText("");
        status.setTextColor(Color.parseColor("#FFB74D"));
        status.setTextSize(13);
        status.setGravity(Gravity.CENTER);
        status.setPadding(0, dp(16), 0, 0);
        home.addView(status);

        setContentView(home);
    }

    /**
     * 规整输入为镜像 URL：
     *   - 完整 URL（带 http(s):// 和路径）→ 原样使用
     *   - 纯 IP(:端口) 或无路径 → 补 http:// 前缀 + 默认镜像路径
     */
    static String normalize(String raw) {
        String s = raw.trim();
        if (s.startsWith("http://") || s.startsWith("https://")) {
            String path = s.substring(s.indexOf("://") + 3);
            if (path.indexOf('/') < 0) s += DEFAULT_PATH;
            return s;
        }
        int slash = s.indexOf('/');
        if (slash < 0) s += DEFAULT_PATH;
        return "http://" + s;
    }

    /** 取 scheme://host:port（探测 /pos 用） */
    static String originOf(String url) {
        int i = url.indexOf('/', url.indexOf("://") + 3);
        return i < 0 ? url : url.substring(0, i);
    }

    /* ---------- 打开前探测：xnavi 是否在线 ---------- */

    private void probeAndOpen(String url) {
        openBtn.setEnabled(false);
        status.setText("检测 xnavi…");
        final String origin = originOf(url);
        new Thread(() -> {
            // "ok" = 在线已锁定；"notok" = 在线未锁定；null = 连不上
            String r = null;
            try {
                HttpURLConnection c = (HttpURLConnection) new URL(origin + "/pos").openConnection();
                c.setConnectTimeout(2500);
                c.setReadTimeout(2500);
                c.setRequestProperty("Connection", "close");
                if (c.getResponseCode() == 200) {
                    InputStream in = c.getInputStream();
                    ByteArrayOutputStream bo = new ByteArrayOutputStream();
                    byte[] buf = new byte[4096];
                    int n;
                    while ((n = in.read(buf)) > 0) bo.write(buf, 0, n);
                    in.close();
                    r = bo.toString("UTF-8").contains("\"ok\":true") ? "ok" : "notok";
                }
                c.disconnect();
            } catch (Exception e) { /* 连不上 → r = null */ }
            final String result = r;
            runOnUiThread(() -> {
                openBtn.setEnabled(true);
                if ("ok".equals(result)) {
                    status.setText("✅ xnavi 在线");
                    openMirror(url);
                } else if ("notok".equals(result)) {
                    status.setText("⚠ xnavi 在线，但尚未锁定定位——进入后请先移动角色");
                    openMirror(url);
                } else {
                    status.setText("✗ 连不上。请确认：电脑 xnavi 已启动 / 手机与电脑同一 WiFi / 地址正确");
                }
            });
        }).start();
    }

    /* ---------- WebView 镜像页 ---------- */

    @SuppressLint("SetJavaScriptEnabled")
    private void openMirror(String url) {
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString(KEY_URL, url).apply();
        hideSystemUi();

        if (web == null) {
            web = new WebView(this);
            web.getSettings().setJavaScriptEnabled(true);
            web.getSettings().setDomStorageEnabled(true);   // live.js 的跟随开关/完成度存 localStorage
            web.setWebViewClient(new WebViewClient() {
                @Override
                public void onReceivedError(WebView view, WebResourceRequest req, WebResourceError err) {
                    if (req.isForMainFrame()) {
                        Toast.makeText(MainActivity.this,
                                "连接中断，请确认电脑上 xnavi 仍在运行", Toast.LENGTH_LONG).show();
                    }
                }
            });
        }
        setContentView(web);
        web.loadUrl(url);
        Toast.makeText(this, "屏幕将保持常亮；返回键可退出", Toast.LENGTH_SHORT).show();
    }

    private void hideSystemUi() {
        View d = getWindow().getDecorView();
        d.setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON); // 双保险
    }

    /* ---------- 返回键：网页后退 → 首页 ---------- */

    @Override
    public boolean onKeyDown(int keyCode, KeyEvent event) {
        if (keyCode == KeyEvent.KEYCODE_BACK) {
            if (web != null && web.canGoBack()) {
                web.goBack();
                return true;
            }
            if (web != null) {
                web.loadUrl("about:blank");
                web = null;
                String last = getSharedPreferences(PREFS, MODE_PRIVATE).getString(KEY_URL, "");
                showHome(last);
                return true;
            }
            long now = System.currentTimeMillis();
            if (now - lastBackTs < 2000) {
                finish();
            } else {
                lastBackTs = now;
                Toast.makeText(this, "再按一次退出", Toast.LENGTH_SHORT).show();
            }
            return true;
        }
        return super.onKeyDown(keyCode, event);
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
