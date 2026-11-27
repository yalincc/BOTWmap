package com.yalincc.xnavimirror;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.SharedPreferences;
import android.content.pm.ActivityInfo;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
import android.os.Bundle;
import android.text.SpannableString;
import android.text.Spanned;
import android.text.style.ForegroundColorSpan;
import android.text.style.RelativeSizeSpan;
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
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.NetworkInterface;
import java.net.Socket;
import java.net.URL;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Enumeration;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * xnavi-mirror：手机镜像壳 APP（极简零第三方依赖）。
 *
 * 能力：
 *   1. 全屏 WebView 打开 xnavi 局域网镜像页 + 系统级屏幕常亮（可开关）
 *   2. 连接历史记录（最近 5 条：点击直连，长按删除）
 *   3. 一键清缓存（仅清 WebView 文件缓存，保留 localStorage 进度与设置）
 *   4. 局域网一键扫描（并发探测同网段 8766~8768 的 xnavi 服务）
 *   5. 全屏页按返回键呼出轻量菜单（强制刷新 / 横竖屏锁定 / 常亮开关 / 换 IP / 退出）
 */
public class MainActivity extends Activity {

    private static final String PREFS = "xnavi_mirror";
    private static final String KEY_URL = "last_url";
    private static final String KEY_HISTORY = "url_history";
    private static final String KEY_KEEP_ON = "keep_screen_on";
    private static final String KEY_ORIENT = "orient_mode"; // 0=自动 1=横屏 2=竖屏
    private static final int MAX_HISTORY = 5;
    private static final String DEFAULT_PATH = "/botw/?follow=1&game=botw";

    private WebView web;
    private String currentUrl = "";
    private EditText ipInput;
    private TextView status;
    private Button openBtn;
    private Button scanBtn;
    private LinearLayout historyBox;
    private long lastBackTs = 0;
    private boolean keepScreenOn = true;
    private int orientMode = 0;
    private final AtomicBoolean scanning = new AtomicBoolean(false);

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        SharedPreferences sp = getSharedPreferences(PREFS, MODE_PRIVATE);
        keepScreenOn = sp.getBoolean(KEY_KEEP_ON, true);
        orientMode = sp.getInt(KEY_ORIENT, 0);
        applyScreenFlags();
        applyOrientation();

        String last = sp.getString(KEY_URL, null);
        showHome(last);
        if (last != null && !last.isEmpty()) {
            probeAndOpen(last);
        }
    }

    private void applyScreenFlags() {
        if (keepScreenOn) {
            getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        } else {
            getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        }
    }

    private void applyOrientation() {
        if (orientMode == 1) {
            setRequestedOrientation(ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE);
        } else if (orientMode == 2) {
            setRequestedOrientation(ActivityInfo.SCREEN_ORIENTATION_PORTRAIT);
        } else {
            setRequestedOrientation(ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED);
        }
    }

    /* ---------- 历史记录存取 ---------- */

    private List<String> loadHistory() {
        String raw = getSharedPreferences(PREFS, MODE_PRIVATE).getString(KEY_HISTORY, "");
        List<String> out = new ArrayList<>();
        if (raw == null || raw.isEmpty()) return out;
        for (String line : raw.split("\n")) {
            String s = line.trim();
            if (!s.isEmpty() && !out.contains(s)) out.add(s);
        }
        return out;
    }

    private void saveHistory(List<String> list) {
        StringBuilder sb = new StringBuilder();
        int n = Math.min(list.size(), MAX_HISTORY);
        for (int i = 0; i < n; i++) {
            if (i > 0) sb.append('\n');
            sb.append(list.get(i));
        }
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString(KEY_HISTORY, sb.toString()).apply();
    }

    private void recordHistory(String url) {
        List<String> list = loadHistory();
        list.remove(url);
        list.add(0, url);
        if (list.size() > MAX_HISTORY) list = new ArrayList<>(list.subList(0, MAX_HISTORY));
        saveHistory(list);
    }

    /* ---------- 首页（纯代码布局，极简暗色主题） ---------- */

    private GradientDrawable roundBg(String fillHex, String strokeHex, int radiusDp) {
        GradientDrawable gd = new GradientDrawable();
        gd.setColor(Color.parseColor(fillHex));
        gd.setCornerRadius(dp(radiusDp));
        if (strokeHex != null) gd.setStroke(dp(1), Color.parseColor(strokeHex));
        return gd;
    }

    private void showHome(String preset) {
        hideSystemUi();
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(Color.parseColor("#0E1410"));

        int pad = dp(22);
        LinearLayout home = new LinearLayout(this);
        home.setOrientation(LinearLayout.VERTICAL);
        home.setGravity(Gravity.CENTER_HORIZONTAL);
        home.setPadding(pad, dp(40), pad, dp(28));

        TextView title = new TextView(this);
        SpannableString titleText = new SpannableString("Xnavi Mirror v0.2");
        int verStart = "Xnavi Mirror ".length();
        titleText.setSpan(new RelativeSizeSpan(0.52f), verStart, titleText.length(), Spanned.SPAN_EXCLUSIVE_EXCLUSIVE);
        titleText.setSpan(new ForegroundColorSpan(Color.parseColor("#A38745")), verStart, titleText.length(), Spanned.SPAN_EXCLUSIVE_EXCLUSIVE);
        title.setText(titleText);
        title.setTextColor(Color.parseColor("#F2C94C"));
        title.setTextSize(26);
        title.setGravity(Gravity.CENTER);
        home.addView(title);

        TextView hint = new TextView(this);
        hint.setText("粘贴电脑镜像链接 / 输入 IP:8766 / 或直接扫描局域网");
        hint.setTextColor(Color.parseColor("#93A68F"));
        hint.setTextSize(13);
        hint.setGravity(Gravity.CENTER);
        hint.setPadding(0, dp(8), 0, dp(18));
        home.addView(hint);

        ipInput = new EditText(this);
        ipInput.setHint("http://192.168.x.x:8766/botw/...");
        ipInput.setTextColor(Color.parseColor("#E8F0E4"));
        ipInput.setHintTextColor(Color.parseColor("#60715D"));
        ipInput.setSingleLine(true);
        ipInput.setTextSize(14);
        ipInput.setPadding(dp(12), dp(10), dp(12), dp(10));
        ipInput.setBackground(roundBg("#151F17", "#2A3A2C", 8));
        LinearLayout.LayoutParams ipLp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        ipLp.bottomMargin = dp(12);
        home.addView(ipInput, ipLp);
        if (preset != null && !preset.isEmpty()) ipInput.setText(preset);

        // 主按钮：打开
        openBtn = new Button(this);
        openBtn.setText("打开导航地图");
        openBtn.setTextColor(Color.parseColor("#F2C94C"));
        openBtn.setBackground(roundBg("#2E2712", "#A38745", 8));
        openBtn.setOnClickListener(v -> {
            String s = ipInput.getText().toString().trim();
            if (s.isEmpty()) {
                Toast.makeText(this, "请先输入 IP:端口 或点击扫局域网", Toast.LENGTH_SHORT).show();
                return;
            }
            probeAndOpen(normalize(s));
        });
        LinearLayout.LayoutParams btnLp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, dp(44));
        btnLp.bottomMargin = dp(10);
        home.addView(openBtn, btnLp);

        // 工具行：扫局域网 + 清缓存
        LinearLayout tools = new LinearLayout(this);
        tools.setOrientation(LinearLayout.HORIZONTAL);
        LinearLayout.LayoutParams toolsLp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        toolsLp.bottomMargin = dp(14);

        scanBtn = new Button(this);
        scanBtn.setText("扫局域网");
        scanBtn.setTextColor(Color.parseColor("#E8F0E4"));
        scanBtn.setTextSize(13);
        scanBtn.setBackground(roundBg("#162118", "#2A3A2C", 8));
        scanBtn.setOnClickListener(v -> startLanScan());
        LinearLayout.LayoutParams halfLeft = new LinearLayout.LayoutParams(0, dp(40), 1f);
        halfLeft.rightMargin = dp(6);
        tools.addView(scanBtn, halfLeft);

        Button clearBtn = new Button(this);
        clearBtn.setText("清缓存");
        clearBtn.setTextColor(Color.parseColor("#E8F0E4"));
        clearBtn.setTextSize(13);
        clearBtn.setBackground(roundBg("#162118", "#2A3A2C", 8));
        clearBtn.setOnClickListener(v -> clearWebCache(true));
        LinearLayout.LayoutParams halfRight = new LinearLayout.LayoutParams(0, dp(40), 1f);
        halfRight.leftMargin = dp(6);
        tools.addView(clearBtn, halfRight);
        home.addView(tools, toolsLp);

        status = new TextView(this);
        status.setText("");
        status.setTextColor(Color.parseColor("#FFB74D"));
        status.setTextSize(13);
        status.setGravity(Gravity.CENTER);
        status.setPadding(0, dp(2), 0, dp(14));
        home.addView(status);

        // 历史记录区域
        historyBox = new LinearLayout(this);
        historyBox.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams histLp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        home.addView(historyBox, histLp);
        renderHistoryList();

        scroll.addView(home);
        setContentView(scroll);
    }

    private void renderHistoryList() {
        if (historyBox == null) return;
        historyBox.removeAllViews();
        List<String> list = loadHistory();
        if (list.isEmpty()) return;

        TextView label = new TextView(this);
        label.setText("最近连接（点击直连，长按删除）");
        label.setTextColor(Color.parseColor("#93A68F"));
        label.setTextSize(12);
        label.setPadding(dp(2), dp(6), 0, dp(6));
        historyBox.addView(label);

        for (String url : list) {
            TextView row = new TextView(this);
            row.setText(url);
            row.setTextColor(Color.parseColor("#D2DED0"));
            row.setTextSize(13);
            row.setSingleLine(true);
            row.setPadding(dp(12), dp(10), dp(12), dp(10));
            row.setBackground(roundBg("#121A14", "#243226", 7));
            row.setOnClickListener(v -> {
                ipInput.setText(url);
                probeAndOpen(url);
            });
            row.setOnLongClickListener(v -> {
                List<String> cur = loadHistory();
                cur.remove(url);
                saveHistory(cur);
                renderHistoryList();
                Toast.makeText(this, "已删除历史记录", Toast.LENGTH_SHORT).show();
                return true;
            });
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                    LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
            lp.bottomMargin = dp(6);
            historyBox.addView(row, lp);
        }
    }

    private void clearWebCache(boolean showToast) {
        if (web != null) {
            web.clearCache(true);
        } else {
            WebView tmp = new WebView(this);
            tmp.clearCache(true);
            tmp.destroy();
        }
        if (showToast) {
            Toast.makeText(this, "已清除网页缓存（保留设置与进度）", Toast.LENGTH_SHORT).show();
        }
    }

    /* ---------- 局域网一键扫描 ---------- */

    private boolean isIgnoredInterface(String name) {
        if (name == null) return true;
        String n = name.toLowerCase();
        return n.startsWith("tun") || n.startsWith("tap") || n.startsWith("wg")
                || n.startsWith("ppp") || n.startsWith("rmnet") || n.startsWith("ccmni")
                || n.startsWith("p2p") || n.startsWith("dummy") || n.startsWith("v4-")
                || n.startsWith("lo");
    }

    private String getLocalSubnetPrefix() {
        String bestPrefix = null;
        int bestScore = -1;
        try {
            Enumeration<NetworkInterface> nis = NetworkInterface.getNetworkInterfaces();
            while (nis != null && nis.hasMoreElements()) {
                NetworkInterface ni = nis.nextElement();
                if (!ni.isUp() || ni.isLoopback()) continue;
                String name = ni.getName();
                if (isIgnoredInterface(name)) continue;
                String lower = name.toLowerCase();
                int ifaceScore = 0;
                if (lower.startsWith("wlan") || lower.startsWith("ap")) ifaceScore = 100;
                else if (lower.startsWith("eth") || lower.startsWith("en")) ifaceScore = 80;
                else ifaceScore = 20;

                Enumeration<InetAddress> addrs = ni.getInetAddresses();
                while (addrs.hasMoreElements()) {
                    InetAddress a = addrs.nextElement();
                    if (a instanceof Inet4Address && a.isSiteLocalAddress()) {
                        String ip = a.getHostAddress();
                        if (ip == null) continue;
                        int idx = ip.lastIndexOf('.');
                        if (idx <= 0) continue;
                        int ipScore = ip.startsWith("192.168.") ? 50 : (ip.startsWith("172.") ? 20 : 5);
                        int score = ifaceScore + ipScore;
                        if (score > bestScore) {
                            bestScore = score;
                            bestPrefix = ip.substring(0, idx + 1);
                        }
                    }
                }
            }
        } catch (Exception ignored) {}
        return bestPrefix;
    }

    private void startLanScan() {
        if (!scanning.compareAndSet(false, true)) return;
        String prefix = getLocalSubnetPrefix();
        if (prefix == null) {
            scanning.set(false);
            status.setText("未检测到局域网 Wi-Fi IP，请确认手机已连接 Wi-Fi");
            return;
        }
        scanBtn.setEnabled(false);
        status.setText("正在扫描 " + prefix + "x 局域网中的 xnavi…");

        new Thread(() -> {
            List<String> found = Collections.synchronizedList(new ArrayList<>());
            int[] ports = new int[]{8766, 8767, 8768};
            ExecutorService pool = Executors.newFixedThreadPool(36);
            for (int host = 1; host <= 254; host++) {
                final String ip = prefix + host;
                pool.execute(() -> {
                    for (int port : ports) {
                        if (!tcpOpen(ip, port, 180)) continue;
                        if (probePosBody("http://" + ip + ":" + port) != null) {
                            found.add("http://" + ip + ":" + port + DEFAULT_PATH);
                            return;
                        }
                    }
                });
            }
            pool.shutdown();
            try { pool.awaitTermination(8, TimeUnit.SECONDS); } catch (InterruptedException ignored) {}
            scanning.set(false);

            runOnUiThread(() -> {
                if (scanBtn != null) scanBtn.setEnabled(true);
                if (found.isEmpty()) {
                    status.setText("未扫描到 xnavi，请确认电脑已启动且在同一 Wi-Fi");
                } else if (found.size() == 1) {
                    String hit = found.get(0);
                    ipInput.setText(hit);
                    status.setText("已找到：" + hit);
                    probeAndOpen(hit);
                } else {
                    status.setText("找到 " + found.size() + " 个导航服务，请选择：");
                    String[] items = found.toArray(new String[0]);
                    new AlertDialog.Builder(MainActivity.this)
                            .setTitle("选择局域网 xnavi")
                            .setItems(items, (d, which) -> {
                                ipInput.setText(items[which]);
                                probeAndOpen(items[which]);
                            })
                            .show();
                }
            });
        }).start();
    }

    private boolean tcpOpen(String ip, int port, int timeoutMs) {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress(ip, port), timeoutMs);
            return true;
        } catch (Exception e) {
            return false;
        }
    }

    private String probePosBody(String origin) {
        HttpURLConnection c = null;
        try {
            c = (HttpURLConnection) new URL(origin + "/pos").openConnection();
            c.setConnectTimeout(1800);
            c.setReadTimeout(1800);
            c.setRequestProperty("Connection", "close");
            if (c.getResponseCode() == 200) {
                InputStream in = c.getInputStream();
                ByteArrayOutputStream bo = new ByteArrayOutputStream();
                byte[] buf = new byte[4096];
                int n;
                while ((n = in.read(buf)) > 0) bo.write(buf, 0, n);
                in.close();
                String body = bo.toString("UTF-8");
                if (body.contains("\"xnavi\"") || body.contains("\"verified\"")) return body;
            }
        } catch (Exception ignored) {
        } finally {
            if (c != null) c.disconnect();
        }
        return null;
    }

    /* ---------- URL 规整 ---------- */

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

    static String originOf(String url) {
        int i = url.indexOf('/', url.indexOf("://") + 3);
        return i < 0 ? url : url.substring(0, i);
    }

    /* ---------- 打开前探测：xnavi 是否在线 ---------- */

    private void probeAndOpen(String url) {
        if (openBtn != null) openBtn.setEnabled(false);
        if (status != null) status.setText("检测 xnavi…");
        final String origin = originOf(url);
        new Thread(() -> {
            String body = probePosBody(origin);
            final String result = (body == null) ? null : (body.contains("\"ok\":true") ? "ok" : "notok");
            runOnUiThread(() -> {
                if (openBtn != null) openBtn.setEnabled(true);
                if ("ok".equals(result)) {
                    status.setText("✅ xnavi 在线");
                    openMirror(url);
                } else if ("notok".equals(result)) {
                    status.setText("⚠ xnavi 在线，等待游戏锁定定位");
                    openMirror(url);
                } else {
                    status.setText("✗ 连不上，请确认电脑 xnavi 已启动且同一 Wi-Fi");
                }
            });
        }).start();
    }

    /* ---------- WebView 镜像页 ---------- */

    @SuppressLint("SetJavaScriptEnabled")
    private void openMirror(String url) {
        currentUrl = url;
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString(KEY_URL, url).apply();
        recordHistory(url);
        hideSystemUi();

        if (web == null) {
            web = new WebView(this);
            web.getSettings().setJavaScriptEnabled(true);
            web.getSettings().setDomStorageEnabled(true);
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
        Toast.makeText(this, "按返回键可呼出快捷菜单", Toast.LENGTH_SHORT).show();
    }

    private void showWebMenu() {
        String orientLabel = orientMode == 1 ? "横屏锁定" : (orientMode == 2 ? "竖屏锁定" : "自动旋转");
        String[] items = new String[]{
                "强制刷新（清缓存重载）",
                "屏幕方向：" + orientLabel + "（点击切换）",
                "屏幕常亮：" + (keepScreenOn ? "已开启" : "已关闭") + "（点击切换）",
                "切换连接（返回首页）",
                "退出应用"
        };
        new AlertDialog.Builder(this)
                .setTitle("Xnavi 快捷菜单")
                .setItems(items, (d, which) -> {
                    if (which == 0 && web != null) {
                        web.clearCache(true);
                        web.reload();
                        Toast.makeText(this, "已清缓存并刷新", Toast.LENGTH_SHORT).show();
                    } else if (which == 1) {
                        orientMode = (orientMode + 1) % 3;
                        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putInt(KEY_ORIENT, orientMode).apply();
                        applyOrientation();
                        String next = orientMode == 1 ? "横屏锁定" : (orientMode == 2 ? "竖屏锁定" : "自动旋转");
                        Toast.makeText(this, "屏幕方向：" + next, Toast.LENGTH_SHORT).show();
                    } else if (which == 2) {
                        keepScreenOn = !keepScreenOn;
                        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putBoolean(KEY_KEEP_ON, keepScreenOn).apply();
                        applyScreenFlags();
                        Toast.makeText(this, "屏幕常亮：" + (keepScreenOn ? "开" : "关"), Toast.LENGTH_SHORT).show();
                    } else if (which == 3) {
                        if (web != null) {
                            web.loadUrl("about:blank");
                            web.destroy();
                            web = null;
                        }
                        showHome(currentUrl);
                    } else if (which == 4) {
                        finish();
                    }
                    hideSystemUi();
                })
                .setOnCancelListener(d -> hideSystemUi())
                .show();
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
        applyScreenFlags();
    }

    /* ---------- 返回键：网页后退 → 快捷菜单；首页双击退出 ---------- */

    @Override
    public boolean onKeyDown(int keyCode, KeyEvent event) {
        if (keyCode == KeyEvent.KEYCODE_BACK) {
            if (web != null && web.canGoBack()) {
                web.goBack();
                return true;
            }
            if (web != null) {
                showWebMenu();
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
