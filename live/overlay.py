"""Transparent always-on-top mini-map overlay for the BOTW live tracker.

Port of ZeldaTOTKmap/live/overlay.py to Breath of the Wild.  Differences:

  * BOTW tiles are WebP at `app/assets/tiles/{z}_{tx}_{ty}.webp`, native up to
    z=7 (zoom7 is 1:1 with the map's 24000x20000 px).  The map's logical px sit
    inside the tile grid with an offset: tpx = px + (4384, 6384).
  * The map coordinate system is (pxX east, pxY south), so north points up on
    the screen via -Y.  Bearings are clockwise from north.
  * Categories come from data.js with Chinese names (神庙 / 希卡塔 /
    克洛格果实 ...).  1 map px = 0.5 game units = 0.5 m.

A 320px borderless window that sits on top of Ryujinx and shows the map around
the player.  It is a pure client of start.py's HTTP service -- run
`python live/start.py` first, or just double-click live/start-all.bat.

Interaction:
    left-drag   move the window (position is remembered)
    wheel       zoom in / out (native tiles go down to z=7, upscale beyond)
    right-click menu: opacity, zoom, always-on-top, reset, quit
    Esc         quit

The window is created with WS_EX_NOACTIVATE so clicking it never steals focus
from the game.

usage:
    python overlay.py [project_root] [port]
"""
import ctypes
import json
import math
import os
import sys
import threading
import time
import tkinter as tk
from collections import OrderedDict
from ctypes import wintypes
import urllib.request
import webbrowser
from urllib.request import ProxyHandler, build_opener

from PIL import Image, ImageDraw, ImageTk

ARGS = [a for a in sys.argv[1:] if not a.startswith("--")]
ROOT = os.path.abspath(ARGS[0] if ARGS else r"E:\WorkSpace\BOTWmap")
PORT = int(ARGS[1]) if len(ARGS) > 1 else 8766
POS_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                        "overlay_pos.json")
# Launched via pythonw there is no console, so anything that goes wrong must
# land in a file or it disappears silently.
LOG_FILE = os.path.join(ROOT, ".workbuddy", "overlay.log")
PERF_FILE = os.path.join(ROOT, ".workbuddy", "overlay_perf.log")

TILES = os.path.join(ROOT, "app", "assets", "tiles")
MAP = 320                       # map viewport, pixels
BAR = 22                        # draggable title bar
INFO = 64                       # strip: coords / nearby / target / progress
TILE = 256
MAX_NATIVE = 7                  # local tiles stop at z=7
ZOOM_MIN, ZOOM_MAX = 4, 9       # ...but we keep going by upscaling those tiles
# map px -> zoom7 tile-grid px offset (from app/js/map.js: tpx = px + offset)
TILE_OX, TILE_OY = 4384, 6384
METRES_PER_UNIT = 0.5           # 1 map px = 0.5 game units = 0.5 m
TILE_CACHE_MAX = 600
# compass rose, clockwise from north (map pxX grows east, pxY grows south)
ARROWS = ("↑", "↗", "→", "↘", "↓", "↙", "←", "↖")

# The overlay is a PURE DISPLAY: every setting lives on the web page and is
# polled from /pos -> config.  Categories are matched by their Chinese name
# (the `cn` field in app/js/data.js).
DONE_CATS = {"神庙", "希卡塔", "克洛格果实"}   # have a save-file flag
DEF_CATS = ["神庙", "希卡塔", "克洛格果实"]    # if /config is empty
CAT_COLOUR = {                                   # per-category dot colour
    "神庙": (255, 183, 3), "希卡塔": (255, 209, 102),
    "克洛格果实": (126, 217, 87),
    "宝箱": (240, 196, 60), "守护者": (231, 76, 60),
    "石头巨人": (168, 128, 96), "独眼巨人": (190, 105, 120),
    "莱尼尔.人马": (200, 120, 220), "莫尔德拉吉克": (230, 170, 90),
    "回忆影片": (140, 170, 255), "神兽": (90, 200, 255),
    "主线任务": (255, 120, 120), "神庙挑战": (255, 160, 90),
    "支线任务": (255, 220, 120), "驿站": (120, 200, 160),
    "村庄": (255, 200, 140), "古代研究所": (170, 210, 240),
    "女神像": (200, 200, 255), "大精灵泉": (255, 180, 230),
}
DONE_COLOUR = (58, 70, 83, 190)          # already collected -> dim grey
SHOW_ITEMS = ("nav", "near", "progress", "coords", "zoom")
SAMPLE_MS = 200                 # repaint cadence when something moved
IDLE_MS = 1000                  # repaint cadence while standing still
SLOW_FRAME = 0.25               # seconds; anything slower gets logged


def log(msg, path=None):
    try:
        p = path or LOG_FILE
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "a", encoding="utf-8") as f:
            f.write("[%s] %s\n" % (time.strftime("%m-%d %H:%M:%S"), msg))
    except Exception:
        pass


u32 = ctypes.windll.user32
u32.GetWindowRect.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.RECT)]
u32.GetWindowLongW.restype = ctypes.c_long
u32.SetWindowLongW.argtypes = [wintypes.HWND, ctypes.c_int, ctypes.c_long]
u32.SetWindowLongW.restype = ctypes.c_long
GWL_EXSTYLE = -20
WS_EX_NOACTIVATE = 0x08000000
WS_EX_TOOLWINDOW = 0x00000080


def load_markers():
    """Every map pin with its category: {cat: [(x, y, name)]}.

    Category = the `cn` field in data.js.  The overlay no longer decides what
    to show -- it filters by whatever the web page configured.
    """
    path = os.path.join(ROOT, "app", "js", "data.js")
    try:
        with open(path, encoding="utf-8") as f:
            src = f.read()
        import re
        m = re.search(r"BOTW_DATA = (\{.*\})", src, re.S)
        d = json.loads(m.group(1))
        raw = d["markers"]
    except Exception as e:
        log("markers load failed: %r" % (e,))
        return {}
    by_cat = {}
    cat_cn = {c.get("key"): (c.get("cn") or c.get("en") or c.get("key"))
              for c in d.get("categories", [])}
    for mk in raw:
        try:
            key = mk.get("cat") or "(未分类)"
            cat = cat_cn.get(key, key)
            by_cat.setdefault(cat, []).append(
                (float(mk["px"][0]), float(mk["px"][1]),
                 mk.get("name") or mk.get("en") or ""))
        except (KeyError, TypeError, ValueError):
            pass
    return by_cat


def load_progress():
    """[(x, y, kind, done, name)] from build_progress.py."""
    path = os.path.join(ROOT, "data", "progress_points.json")
    try:
        with open(path, encoding="utf-8") as f:
            obj = json.load(f)
    except Exception as e:
        log("progress load failed: %r" % (e,))
        return [], None
    out = []
    for p in obj.get("points", []):
        out.append((float(p["x"]), float(p["y"]),
                    p.get("t", "shrine"), bool(p.get("done")),
                    p.get("cn") or p.get("en") or ""))
    return out, obj.get("counts")


class Overlay:
    def __init__(self):
        self.markers = load_markers()
        self.progress, self.prog_counts = load_progress()
        self.target = None          # (name, metres, arrow, x, y)
        self.target_manual = False
        self.progress_mtime = 0.0
        self._last_tname = None
        self._last_pick = None
        # settings mirrored from the web page (see /pos -> config)
        self.cats_on = set(DEF_CATS)
        self.done_filter = "all"    # all | done | todo
        self.show = {"nav": True}   # info-strip lines; web decides
        self.done_set = set()
        self._cfg_seen = False
        self.rebuild_done_set()
        self.cache = OrderedDict()
        self.zoom = 6
        self.alpha = 0.88
        self.topmost = True
        self.trail = []
        self.fail = 0
        self.photo = None
        self.last_key = None
        self.last_draw = 0.0
        self._move_job = None
        self._pending_xy = None
        self._msg = ""

        # latest sample, written by the poller thread
        self.sample = None
        self.sample_lock = threading.Lock()

        self.root = tk.Tk()
        self.root.title("BOTW mini-map")
        self.root.overrideredirect(True)
        self.root.attributes("-topmost", self.topmost)
        self.root.attributes("-alpha", self.alpha)
        self.root.configure(bg="#0e141c")

        w, h = MAP, BAR + MAP + INFO
        x, y = self.default_pos(w, h)
        self.root.geometry("%dx%d+%d+%d" % (w, h, x, y))

        self.bar = tk.Frame(self.root, bg="#182430", height=BAR, cursor="fleur")
        self.bar.pack(fill="x")
        self.bar.pack_propagate(False)
        self.title = tk.Label(self.bar, text="BOTW", bg="#182430",
                              fg="#8fc7e8", font=("Microsoft YaHei UI", 9, "bold"))
        self.title.pack(side="left", padx=8)
        self.weblnk = tk.Label(self.bar, text="网页 ↗", bg="#182430", fg="#5fa8d3",
                               font=("Microsoft YaHei UI", 9), cursor="hand2")
        self.weblnk.pack(side="right", padx=(0, 6))
        self.weblnk.bind("<Button-1>", lambda e: self.open_web())
        self.weblnk.bind("<Enter>", lambda e: self.weblnk.config(fg="#9fd3f0"))
        self.weblnk.bind("<Leave>", lambda e: self.weblnk.config(fg="#5fa8d3"))
        self.close = tk.Label(self.bar, text="×", bg="#182430", fg="#c05555",
                              font=("Microsoft YaHei UI", 11, "bold"),
                              cursor="hand2")
        self.close.pack(side="right", padx=8)
        self.close.bind("<Button-1>", lambda e: self.quit())

        self.canvas = tk.Canvas(self.root, width=MAP, height=MAP,
                                bg="#0e141c", highlightthickness=0)
        self.canvas.pack()

        self.info = tk.Label(self.root, bg="#182430", fg="#cfe6f5",
                             font=("Microsoft YaHei UI", 8), justify="left",
                             anchor="nw", padx=8, pady=2)
        self.info.pack(fill="both", expand=True)

        for wdg in (self.bar, self.title, self.canvas):
            wdg.bind("<Button-1>", self.drag_start)
            wdg.bind("<B1-Motion>", self.drag_move)
        self.root.bind("<MouseWheel>", self.on_wheel)
        self.root.bind("<Escape>", lambda e: self.quit())
        self.canvas.bind("<Button-3>", self.menu)
        self.bar.bind("<Button-3>", self.menu)

        self.root.update_idletasks()
        self.make_noactivate()
        threading.Thread(target=self.poller, daemon=True).start()
        self.tick()

    # -- window plumbing ---------------------------------------------------
    def default_pos(self, w, h):
        """Left of the Ryujinx window, vertically centred; else screen-left."""
        saved = None
        try:
            with open(POS_FILE, encoding="utf-8") as f:
                saved = json.load(f)
        except Exception:
            pass
        if saved and isinstance(saved.get("x"), int):
            return saved["x"], saved["y"]
        rx, ry, rw, rh = self.ryujinx_rect()
        if rw:
            return max(8, rx - w - 24), ry + max(0, (rh - h) // 2)
        return 60, 200

    def ryujinx_rect(self):
        """Geometry of the Ryujinx main window, or (0,0,0,0)."""
        best = (0, 0, 0, 0)

        @ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
        def cb(hwnd, _l):
            nonlocal best
            if not u32.IsWindowVisible(hwnd):
                return True
            ln = u32.GetWindowTextLengthW(hwnd)
            if ln <= 0:
                return True
            buf = ctypes.create_unicode_buffer(ln + 2)
            u32.GetWindowTextW(hwnd, buf, ln + 2)
            if "Ryujinx" not in buf.value:
                return True
            r = wintypes.RECT()
            u32.GetWindowRect(hwnd, ctypes.byref(r))
            w, h = r.right - r.left, r.bottom - r.top
            if w > 200 and h > 200 and w * h > best[2] * best[3]:
                best = (r.left, r.top, w, h)
            return True

        u32.EnumWindows(cb, 0)
        return best

    def make_noactivate(self):
        """Stop the overlay from stealing focus from the game."""
        try:
            hwnd = u32.GetParent(self.root.winfo_id()) or self.root.winfo_id()
            ex = u32.GetWindowLongW(hwnd, GWL_EXSTYLE)
            u32.SetWindowLongW(hwnd, GWL_EXSTYLE,
                               ex | WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW)
        except Exception as e:
            log("noactivate failed: %r" % (e,))

    def drag_start(self, e):
        self._dx, self._dy = e.x, e.y

    def drag_move(self, e):
        """Coalesce motion events: at most ~60 moves/s reach the window manager."""
        self._pending_xy = (e.x_root - self._dx, e.y_root - self._dy)
        if self._move_job is None:
            self._move_job = self.root.after(16, self._apply_move)

    def _apply_move(self):
        self._move_job = None
        if self._pending_xy:
            self.root.geometry("+%d+%d" % self._pending_xy)
            self._pending_xy = None

    def on_wheel(self, e):
        step = 1 if e.delta > 0 else -1
        self.set_zoom(self.zoom + step)

    def menu(self, e):
        m = tk.Menu(self.root, tearoff=0)
        op = tk.Menu(m, tearoff=0)
        for a in (1.0, 0.88, 0.7, 0.5):
            op.add_command(label="%d%%" % (a * 100),
                           command=lambda v=a: self.set_alpha(v))
        m.add_cascade(label="不透明度", menu=op)
        z = tk.Menu(m, tearoff=0)
        for zz in range(ZOOM_MIN, ZOOM_MAX + 1):
            z.add_command(label="z=%d" % zz,
                          command=lambda v=zz: self.set_zoom(v))
        m.add_cascade(label="缩放", menu=z)
        self.top_var = tk.BooleanVar(value=self.topmost)
        m.add_checkbutton(label="总在最前", variable=self.top_var,
                          command=self.toggle_top)
        m.add_separator()
        m.add_command(label="打开网页地图", command=self.open_web)
        m.add_command(label="清除导航目标", command=self.clear_target)
        m.add_separator()
        m.add_command(label="重置位置", command=self.reset_pos)
        m.add_separator()
        m.add_command(label="退出", command=self.quit)
        m.tk_popup(e.x_root, e.y_root)

    # -- helpers ----------------------------------------------------------
    def open_web(self):
        """Open the full-screen map page in the default browser."""
        url = "http://127.0.0.1:%d/app/index.html" % PORT
        try:
            webbrowser.open(url)
            log("opened %s" % url)
        except Exception as e:
            log("open browser failed: %r" % (e,))

    def clear_target(self):
        try:
            opener = build_opener(ProxyHandler({}))
            req = urllib.request.Request(
                "http://127.0.0.1:%d/target" % PORT,
                data=b'{"clear": true}', method="POST",
                headers={"Content-Type": "application/json"})
            opener.open(req, timeout=2).close()
        except Exception:
            pass
        self.target = None
        self.target_manual = False
        self.last_key = None

    def set_zoom(self, z):
        z = max(ZOOM_MIN, min(ZOOM_MAX, z))
        if z != self.zoom:
            self.zoom = z
            self.last_key = None        # force a redraw

    def set_alpha(self, a):
        self.alpha = a
        self.root.attributes("-alpha", a)

    def toggle_top(self):
        self.topmost = bool(self.top_var.get())
        self.root.attributes("-topmost", self.topmost)

    def reset_pos(self):
        self.root.geometry("+%d+%d" % self.default_pos(MAP, BAR + MAP + INFO))

    def save_pos(self):
        try:
            with open(POS_FILE, "w", encoding="utf-8") as f:
                json.dump({"x": self.root.winfo_x(), "y": self.root.winfo_y()}, f)
        except Exception:
            pass

    def quit(self):
        self.save_pos()
        self.root.destroy()

    # -- data (background thread: the UI must never block on HTTP) ---------
    def poller(self):
        url = "http://127.0.0.1:%d/pos?who=overlay" % PORT
        # empty ProxyHandler: never route a localhost call through a system
        # proxy (this machine has both HTTP_PROXY and http_proxy set)
        opener = build_opener(ProxyHandler({}))
        while True:
            t0 = time.time()
            data = None
            try:
                with opener.open(url, timeout=3.0) as r:
                    data = json.loads(r.read().decode("utf-8"))
            except Exception:
                data = None
            dt = time.time() - t0
            if dt > 1.0:
                log("slow /pos: %.2fs" % dt, PERF_FILE)
            with self.sample_lock:
                self.sample = data
            time.sleep(0.2)

    def take_sample(self):
        with self.sample_lock:
            return self.sample

    # -- progress hot-reload ------------------------------------------------
    def maybe_reload_progress(self):
        """Pick up data/progress_points.json when the service rewrites it."""
        path = os.path.join(ROOT, "data", "progress_points.json")
        try:
            mt = os.path.getmtime(path)
        except OSError:
            return
        if mt == self.progress_mtime:
            return
        self.progress, self.prog_counts = load_progress()
        self.progress_mtime = mt
        self.rebuild_done_set()
        self.last_key = None                # force a repaint
        log("progress reloaded: %d points" % len(self.progress))

    def rebuild_done_set(self):
        """Rounded coords of every completed point, for O(1) done lookups."""
        self.done_set = set()
        for px, py, _k, dn, _nm in self.progress:
            if dn:
                self.done_set.add((int(round(px)), int(round(py))))

    def is_done(self, x, y, tol=3):
        """Pins sit ~1-3 map px from the object they describe -> probe a box."""
        cx, cy = int(round(x)), int(round(y))
        for dx in range(-tol, tol + 1):
            for dy in range(-tol, tol + 1):
                if (cx + dx, cy + dy) in self.done_set:
                    return True
        return False

    def apply_config(self, cfg):
        """Mirror the web page's settings; the overlay owns no settings itself."""
        if not isinstance(cfg, dict):
            return
        cats = cfg.get("cats")
        if isinstance(cats, list):
            new = set(cats)
            if new != self.cats_on:
                self.cats_on = new
                self.last_key = None
                log("config cats -> %d: %s" % (len(new), "、".join(sorted(new))[:120]))
        elif cats is None and self._cfg_seen is False:
            log("config has no cats field (got keys: %s)"
                % (",".join(sorted(cfg.keys())) or "none"))
        self._cfg_seen = True
        df = cfg.get("doneFilter")
        if df in ("all", "done", "todo") and df != self.done_filter:
            self.done_filter = df
            self.last_key = None
        show = cfg.get("show")
        if isinstance(show, dict) and show != self.show:
            self.show = show
            self.last_key = None

    def done_near(self, x, y, tol=0.75):
        """Is the point at (x, y) already collected? (pin coords are ~1-3 px off)"""
        for px, py, _kind, done, _nm in self.progress:
            if (px - x) ** 2 + (py - y) ** 2 <= tol * tol:
                return done
        return False

    # -- tiles -------------------------------------------------------------
    def tile(self, z, col, row):
        if col < 0 or row < 0:
            return None
        key = (z, col, row)
        hit = self.cache.get(key, "MISS")
        if hit != "MISS":
            self.cache.move_to_end(key)
            return hit
        img = None
        p = os.path.join(TILES, "%d_%d_%d.webp" % (z, col, row))
        if os.path.exists(p):
            try:
                img = Image.open(p).convert("RGBA")
            except Exception as e:
                log("bad tile %s: %r" % (p, e))
                img = None
        self.cache[key] = img
        if len(self.cache) > TILE_CACHE_MAX:
            self.cache.popitem(last=False)
        return img

    # -- rendering ---------------------------------------------------------
    def render(self, mx, my):
        """Draw the viewport centred on (mx, my).

        BOTW tiles go natively to z=7; beyond that the base map is assembled at
        z=7 and upscaled by 2^(zoom-7).  Map px -> zoom7 tile grid px uses the
        offset (TILE_OX, TILE_OY); each tile at native zoom n spans
        256 * 2^(7-n) map px.
        """
        t0 = time.time()
        native = min(self.zoom, MAX_NATIVE)
        over = 1 << (self.zoom - native)        # 1, 2 or 4
        zn = 1.0 / (1 << (7 - native))          # native px per map px (<=1)
        zs = float(1 << (self.zoom - 7)) if self.zoom >= 7 \
            else 1.0 / (1 << (7 - self.zoom))   # window px per map px
        sub = MAP // over                       # base-map canvas, native px
        tspan = 256 * (1 << (7 - native))       # map px per tile at native zoom

        def to_px(x, y):
            """map px (x east, y south) -> window px."""
            return ((x - mx) * zs + MAP / 2.0, (y - my) * zs + MAP / 2.0)

        view = Image.new("RGBA", (sub, sub), (14, 20, 28, 255))
        # tile grid coords covering the viewport
        c0 = int(((mx - sub / 2.0 / zn) + TILE_OX) // tspan)
        c1 = int(((mx + sub / 2.0 / zn) + TILE_OX) // tspan)
        r0 = int(((my - sub / 2.0 / zn) + TILE_OY) // tspan)
        r1 = int(((my + sub / 2.0 / zn) + TILE_OY) // tspan)
        for col in range(c0, c1 + 1):
            for row in range(r0, r1 + 1):
                t = self.tile(native, col, row)
                tl_x = col * tspan - TILE_OX        # map px of tile left edge
                tl_y = row * tspan - TILE_OY
                ox = int(round((tl_x - mx) * zn + sub / 2.0))
                oy = int(round((tl_y - my) * zn + sub / 2.0))
                if t is not None:
                    view.paste(t, (ox, oy), t)
                else:
                    view.paste((10, 14, 20, 255),
                               (ox, oy, ox + int(tspan * zn), oy + int(tspan * zn)))
        if over > 1:
            view = view.resize((MAP, MAP), Image.LANCZOS)

        d = ImageDraw.Draw(view)
        # every pin of an enabled category; done-filter comes from the web page
        for cat, pins in self.markers.items():
            if cat not in self.cats_on:
                continue
            for x, y, _n in pins:
                sx, sy = to_px(x, y)
                if not (-8 <= sx <= MAP + 8 and -8 <= sy <= MAP + 8):
                    continue
                done = self.is_done(x, y) if cat in DONE_CATS else False
                if self.done_filter == "done" and not done:
                    continue
                if self.done_filter == "todo" and done:
                    continue
                if done:
                    d.ellipse([sx - 2, sy - 2, sx + 2, sy + 2], fill=DONE_COLOUR,
                              outline=(90, 107, 122, 200))
                    continue
                r, g, b = CAT_COLOUR.get(cat, (56, 168, 207))
                if cat == "希卡塔":
                    d.ellipse([sx - 4, sy - 4, sx + 4, sy + 4], fill=(r, g, b, 255),
                              outline=(181, 143, 0, 255), width=2)
                else:
                    rad = 3.5 if cat == "神庙" else 2.8
                    d.ellipse([sx - rad, sy - rad, sx + rad, sy + rad],
                              fill=(r, g, b, 235),
                              outline=(max(0, r - 74), max(0, g - 57),
                                       max(0, b - 3), 255))
        pts = [to_px(x, y) for x, y in self.trail]
        if len(pts) > 1:
            d.line(pts, fill=(231, 76, 60, 150), width=2)
        c = MAP / 2.0
        if self.target:                       # guide line to the next target
            _nm, _metres, _arrow, tx, ty = self.target
            sx, sy = to_px(tx, ty)
            d.line([(c, c), (sx, sy)], fill=(255, 183, 3, 190), width=2)
            d.ellipse([sx - 6, sy - 6, sx + 6, sy + 6],
                      outline=(255, 183, 3, 255), width=2)
        d.ellipse([c - 6, c - 6, c + 6, c + 6], fill=(231, 76, 60, 255),
                  outline=(255, 255, 255, 240), width=2)
        d.rectangle([0, 0, MAP - 1, MAP - 1], outline=(90, 110, 130, 200))
        dt = time.time() - t0
        if dt > SLOW_FRAME:
            log("slow render: %.3fs  z=%d cache=%d"
                % (dt, self.zoom, len(self.cache)), PERF_FILE)
        return view

    def nearest(self, mx, my, n=2):
        best = []
        for cat, pins in self.markers.items():
            if cat not in self.cats_on:
                continue
            for x, y, name in pins:
                dx, dy = x - mx, y - my
                best.append((dx * dx + dy * dy, name))
        best.sort(key=lambda t: t[0])
        return [(nm, (dd ** 0.5) * METRES_PER_UNIT) for dd, nm in best[:n]]

    def metres(self, mx, my, x, y):
        return ((x - mx) ** 2 + (y - my) ** 2) ** 0.5 * METRES_PER_UNIT

    def bearing(self, mx, my, x, y):
        """Map pxX grows east, pxY grows south -> clockwise from north."""
        dE, dN = x - mx, -(y - my)
        ang = math.atan2(dE, dN)
        return ARROWS[int(round(ang / (math.pi / 4.0))) % 8]

    def nearest_target(self, mx, my):
        """Closest not-yet-done pin among the categories the web page enabled."""
        best = None
        for cat, pins in self.markers.items():
            if cat not in self.cats_on:
                continue
            for x, y, name in pins:
                if cat in DONE_CATS and self.is_done(x, y):
                    continue                    # already collected -> skip
                d = (x - mx) ** 2 + (y - my) ** 2
                if best is None or d < best[0]:
                    best = (d, x, y, name, cat)
        if best is None:
            return None
        d, x, y, name, cat = best
        key = (name, cat)
        if key != self._last_pick:
            self._last_pick = key
            log("pick %s [%s] %.0fm  (cats_on=%d: %s)"
                % (name, cat, (d ** 0.5) * METRES_PER_UNIT, len(self.cats_on),
                   "、".join(sorted(self.cats_on))[:80]))
        return (name, (d ** 0.5) * METRES_PER_UNIT,
                self.bearing(mx, my, x, y), x, y)

    # -- main loop ---------------------------------------------------------
    def tick(self):
        """Repaint only when the picture actually changes; never block."""
        try:
            self._tick()
        except Exception as e:
            import traceback
            log("tick error: %r\n%s" % (e, traceback.format_exc()))
        interval = SAMPLE_MS if self._msg else IDLE_MS
        self.root.after(interval, self.tick)

    def _tick(self):
        t0 = time.time()
        self.maybe_reload_progress()
        p = self.take_sample()
        if p:
            self.apply_config(p.get("config"))     # settings come from the web
        if not p or not p.get("ok"):
            self.fail += 1
            if self.fail == 1:
                self._msg = "offline"
                self.canvas.delete("all")
                self.canvas.create_text(
                    MAP // 2, MAP // 2, fill="#8fa8bd",
                    font=("Microsoft YaHei UI", 9), justify="center",
                    text="等待 start.py 服务…\n\n先运行：\npython live/start.py")
                self.info.config(text="未连接 http://127.0.0.1:%d/pos" % PORT)
            return
        self.fail = 0
        self._msg = ""

        mx, my = float(p["mx"]), float(p["my"])

        key = (round(mx, 2), round(my, 2), self.zoom)
        moving = self.last_key is not None and key != self.last_key
        # standing still -> repaint rarely, but still refresh the status line
        if key == self.last_key and time.time() - self.last_draw < IDLE_MS / 1000.0:
            return
        if moving:
            self.trail.append((mx, my))
            if len(self.trail) > 400:
                self.trail.pop(0)

        # navigation target: whatever was picked on the web map wins, otherwise
        # fall back to the closest unfinished point of the enabled categories
        self.target_manual = False
        tgt = p.get("target")
        if isinstance(tgt, dict) and "x" in tgt:
            try:
                tx, ty = float(tgt["x"]), float(tgt["y"])
                if self.done_near(tx, ty):
                    # already collected -> drop it, fall back to auto target
                    self.clear_target()
                    self.target = self.nearest_target(mx, my)
                else:
                    self.target = (str(tgt.get("name") or ""),
                                   self.metres(mx, my, tx, ty),
                                   self.bearing(mx, my, tx, ty), tx, ty)
                    self.target_manual = True
            except (TypeError, ValueError):
                self.target = None
        else:
            self.target = self.nearest_target(mx, my)

        self.target = self.target or None
        tname = self.target[0] if self.target else ""
        if tname != self._last_tname:          # only when it actually changes
            self._last_tname = tname
            log("target -> %s" % (tname or "(none)"))
        img = self.render(mx, my)
        self.photo = ImageTk.PhotoImage(img)
        self.canvas.delete("all")
        self.canvas.create_image(0, 0, anchor="nw", image=self.photo)

        show = self.show or {}
        lines = []
        if show.get("coords"):
            lines.append("X %7.1f  Z %7.1f  H %6.1f"
                         % (float(p["gx"]), float(p["gy"]), float(p["gz"])))
        if show.get("near"):
            lines.append("  ".join("%s %.0fm" % (nm[:9], d)
                                   for nm, d in self.nearest(mx, my)))
        if show.get("nav"):
            if self.target:
                tname, tdist, tarrow, _x, _y = self.target
                lines.append("→ %s  %.0fm %s" % (tname[:14], tdist, tarrow))
            else:
                lines.append("→ 已开启类别中没有未完成目标")
        if show.get("progress"):
            pc = self.prog_counts or {}
            sh = pc.get("shrine", [0, 0])
            tw = pc.get("tower", [0, 0])
            ko = pc.get("korok", [0, 0])
            lines.append("神庙 %d/%d  塔 %d/%d  克洛格 %d/%d"
                         % (sh[0], sh[1], tw[0], tw[1], ko[0], ko[1]))
        if show.get("zoom"):
            lines.append("z=%d" % self.zoom)
        self.info.config(text="\n".join(lines))
        self.title.config(text="BOTW")
        self.last_key = key
        self.last_draw = time.time()
        dt = time.time() - t0
        if dt > SLOW_FRAME:
            log("slow tick: %.3fs" % dt, PERF_FILE)


def main():
    log("overlay start: root=%s port=%d" % (ROOT, PORT))
    print("overlay: project=%s  port=%d" % (ROOT, PORT))
    print("  left-drag move · wheel zoom · right-click menu · Esc quit")
    try:
        Overlay().root.mainloop()
    except tk.TclError as e:
        log("TclError: %r" % (e,))
        print("cannot open a window here: %s" % e)
        return 1
    except Exception as e:
        import traceback
        log("fatal: %r\n%s" % (e, traceback.format_exc()))
        raise
    log("overlay closed")
    print("overlay closed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
