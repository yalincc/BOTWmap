"""One-command launcher: find Ryujinx -> auto-locate the player address -> serve
the BOTW live map + floating overlay.

Port of ZeldaTOTKmap/live/start.py to Breath of the Wild.  Differences:

  * save anchor = game_data.sav PLAYER_POSITION (vector3f flag, no bias):
        mem = (X east, Y altitude, Z south+)  ==  save format
        map px = (2*X + 12000, 2*Z + 10000)   (the BOTWmap coordinate system)
  * BOTW has no sky/underground layers, so `layer` is always 0.
  * default port 8766 (TOTK uses 8765).

Locating strategy, in order:

  1. SAVE ANCHOR (default, no user action)
     Read the most-played BOTW save for the last known player position, scan
     guest RAM for that triple, keep the shortlist, and let the liveness
     watcher lock onto the copy group that actually moves.

  2. MOTION SCAN (fallback when the save is too stale to be useful)
     Collect structurally valid position fields (float triple followed by an
     orthonormal 3x3 rotation), count down, let the player walk, then keep the
     candidates whose value changed.

Usage:
    python start.py [project_root] [port] [--no-save] [--no-open]
Press Ctrl+C to stop.
"""
import ctypes
import json
import os
import struct
import subprocess
import sys
import threading
import time
import webbrowser
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from ctypes import wintypes
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
from numpy.lib.stride_tricks import as_strided

import locate
from locate import MEMORY_BASIC_INFORMATION      # share one ctypes type
try:
    import build_progress      # collection progress from the save file
except Exception as _e:
    build_progress = None      # progress_* missing -> the watcher just idles

ARGS = [a for a in sys.argv[1:] if not a.startswith("--")]
FLAGS = {a for a in sys.argv[1:] if a.startswith("--")}
ROOT = os.path.abspath(ARGS[0] if ARGS else r"E:\WorkSpace\BOTWmap")
PORT = int(ARGS[1]) if len(ARGS) > 1 else 8766
USE_SAVE = "--no-save" not in FLAGS
AUTO_OPEN = "--no-open" not in FLAGS
PROCESS_NAME = "Ryujinx"

CHUNK = 32 * 1024 * 1024
THREADS = 8
BATCH = 2_000_000
ROT_TOL = 0.2
COUNTDOWN = 5

# BOTW: memory/save triple = (X east, Y altitude, Z south+); map px = game*2+off
# (verified 2026-09-15: the save position appears verbatim in guest RAM).
ELEV_BIAS = 0.0

PROCESS_QUERY_INFORMATION = 0x0400
PROCESS_VM_READ = 0x0010
TH32CS_SNAPPROCESS = 0x2
MEM_COMMIT = 0x1000
MEM_MAPPED = 0x40000
PAGE_GUARD = 0x100
PAGE_NOACCESS = 0x01

k32 = ctypes.windll.kernel32
k32.OpenProcess.restype = wintypes.HANDLE

HANDLE = None
PID = None
LOCK = {"addr": None, "verified": False, "copies": 0, "source": "-"}
STATE = {"ok": False, "gx": 0.0, "gy": 0.0, "gz": 0.0,
         "mx": 0.0, "my": 0.0, "layer": 0, "age": 0.0,
         "verified": False, "copies": 0, "source": "-"}

# --- remembered address set -------------------------------------------------
# The coordinate slot is DETERMINISTIC across a game restart (verified for TOTK;
# BOTW is expected to behave the same).  We persist offsets, not addresses.
KNOWN_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                          "known_addrs.json")
DEFAULT_OFFSETS = []
LEGACY_BLOCK = 0

GUEST_BASE = 0                     # host address of the guest DRAM block


def load_offsets():
    """Offsets of the coordinate slot inside guest DRAM (newest first)."""
    try:
        with open(KNOWN_FILE, encoding="utf-8") as f:
            data = json.load(f)
    except Exception:
        data = {}
    out = []

    def add(v):
        try:
            o = int(v, 16) if isinstance(v, str) else int(v)
        except Exception:
            return
        if 0 < o < 0x100000000 and o not in out:
            out.append(o)

    for v in data.get("offsets") or []:
        add(v)
    try:
        blk = data.get("block") or LEGACY_BLOCK
        blk = int(blk, 16) if isinstance(blk, str) else int(blk)
    except Exception:
        blk = LEGACY_BLOCK
    for v in data.get("addrs") or []:
        try:
            a = int(v, 16) if isinstance(v, str) else int(v)
        except Exception:
            continue
        if a > blk:
            add(a - blk)
    for o in DEFAULT_OFFSETS:
        add(o)
    return out


def load_known():
    """Absolute addresses for the current block base (used when saving)."""
    return [GUEST_BASE + o for o in load_offsets()] if GUEST_BASE else []


def save_known(addrs):
    """Persist addresses as offsets relative to the current guest block."""
    global GUEST_BASE
    if not GUEST_BASE:
        GUEST_BASE = guest_ram()[0]
    offs = []
    for a in addrs:
        try:
            o = a - GUEST_BASE if a > GUEST_BASE else a
        except Exception:
            continue
        if 0 < o < 0x100000000 and o not in offs:
            offs.append(o)
    for o in load_offsets():
        if o not in offs:
            offs.append(o)
    try:
        with open(KNOWN_FILE, "w", encoding="utf-8") as f:
            json.dump({"block": "0x%012X" % GUEST_BASE,
                       "offsets": ["0x%08X" % o for o in offs],
                       "updated": time.strftime("%Y-%m-%d %H:%M:%S")}, f, indent=2)
        print("  [known] saved %d offset(s) to %s (block 0x%012X)"
              % (len(offs), os.path.basename(KNOWN_FILE), GUEST_BASE), flush=True)
    except Exception as e:
        print("  [known] save failed: %s" % e, flush=True)


def decode_pos(d):
    """12 bytes of guest memory -> (gx, gz, alt) or None if not player-like.

    BOTW memory triple = (X east, Y altitude, Z south+); map px = (2X+12000,
    2Z+10000).  gx = v0, gz = v2, alt = v1.
    """
    if not d or len(d) != 12:
        return None
    v0, v1, v2 = struct.unpack("<3f", d)
    if v0 == 0.0 and v1 == 0.0 and v2 == 0.0:
        return None     # slot not initialised yet (title / loading screen)
    gx, alt, gz = v0, v1, v2
    if not (-7000.0 < gx < 7000.0 and -7000.0 < gz < 7000.0
            and -600.0 < alt < 5000.0):
        return None
    return (gx, gz, alt)


def try_known():
    """Fast path: re-base the remembered offsets and read them."""
    return try_offsets()


class PROCESSENTRY32(ctypes.Structure):
    _fields_ = [("dwSize", wintypes.DWORD), ("cntUsage", wintypes.DWORD),
                ("th32ProcessID", wintypes.DWORD),
                ("th32DefaultHeapID", ctypes.POINTER(ctypes.c_ulong)),
                ("th32ModuleID", wintypes.DWORD), ("cntThreads", wintypes.DWORD),
                ("th32ParentProcessID", wintypes.DWORD),
                ("pcPriClassBase", ctypes.c_long), ("dwFlags", wintypes.DWORD),
                ("szExeFile", ctypes.c_char * 260)]


def find_pid(name):
    snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
    if snap == -1:
        return None
    e = PROCESSENTRY32()
    e.dwSize = ctypes.sizeof(PROCESSENTRY32)
    ok = k32.Process32First(snap, ctypes.byref(e))
    found = None
    while ok:
        if e.szExeFile.decode("ascii", "replace").lower().startswith(name.lower()):
            found = e.th32ProcessID
            break
        ok = k32.Process32Next(snap, ctypes.byref(e))
    k32.CloseHandle(snap)
    return found


def read_mem(addr, size):
    buf = ctypes.create_string_buffer(size)
    got = ctypes.c_size_t()
    if not k32.ReadProcessMemory(HANDLE, ctypes.c_void_p(addr), buf, size,
                                 ctypes.byref(got)):
        return None
    return buf.raw[:got.value]


def guest_ram(min_mb=1024.0):
    """Host address + size of the largest guest DRAM block."""
    best = (0, 0)
    mbi = MEMORY_BASIC_INFORMATION()
    addr = 0
    while addr < 0x7FFFFFFFFFFF:
        n = k32.VirtualQueryEx(HANDLE, ctypes.c_void_p(addr), ctypes.byref(mbi),
                               ctypes.sizeof(mbi))
        if n == 0:
            break
        base, size = mbi.BaseAddress or 0, mbi.RegionSize
        if (mbi.State == MEM_COMMIT and mbi.Type == MEM_MAPPED
                and (mbi.Protect & 0xFF) in (0x04, 0x02)
                and not (mbi.Protect & PAGE_GUARD)
                and size > best[1]):
            best = (base, size)
        nxt = base + size
        addr = nxt if nxt > addr else addr + 0x1000
    return best if best[1] >= min_mb * 1048576.0 else (0, 0)


def try_offsets(verbose=True):
    """Re-base the remembered offsets onto the current process and read them."""
    global GUEST_BASE
    if not reopen_process():
        if verbose:
            print("  [known] Ryujinx not running", flush=True)
        return None
    base, size = guest_ram()
    if not base:
        if verbose:
            print("  [known] guest DRAM block not ready yet (%.0f MB largest)"
                  % (size / 1048576.0), flush=True)
        return None
    GUEST_BASE = base
    offs = load_offsets()
    vals = []
    for o in offs:
        v = decode_pos(read_mem(base + o, 12))
        if v is not None:
            vals.append((base + o, o, v))
    if not vals:
        if verbose:
            print("  [known] none of the %d remembered offsets is usable "
                  "(block 0x%012X)" % (len(offs), base), flush=True)
        return None
    best, bestn = None, -1
    for a, _o, v in vals:
        n = sum(1 for _b, _p, w in vals
                if abs(v[0] - w[0]) < 2 and abs(v[1] - w[1]) < 2
                and abs(v[2] - w[2]) < 2)
        if n > bestn:
            best, bestn = (a, v), n
    if verbose:
        print("  [known] block 0x%012X (%.0f MB) | %d/%d offsets readable, "
              "best 0x%X agreed by %d copy(ies)  hud=(%.1f, %.1f, %.1f)"
              % (base, size / 1048576.0, len(vals), len(offs), best[0],
                 bestn - 1, best[1][0], best[1][1], best[1][2]), flush=True)
    if bestn < 2 and len(offs) > 1:
        if verbose:
            print("  [known] no agreement between copies - unreliable",
                  flush=True)
        return None
    return best


def scan_chunk(job):
    """Structural scan: float triple + orthonormal 3x3 rotation after it."""
    base, size = job
    data = read_mem(base, size + 64)
    if not data or len(data) < 256:
        return []
    n = len(data) // 4
    a = np.frombuffer(data[:n * 4], dtype=np.float32)
    cols = as_strided(a, shape=(len(a) - 2, 3), strides=(4, 4))
    N = len(a) - 12
    out = []
    old = np.seterr(all="ignore")
    for s in range(0, N, BATCH):
        e = min(s + BATCH, N)
        c0 = cols[s:e]
        r0, r1, r2 = cols[s + 3:e + 3], cols[s + 6:e + 6], cols[s + 9:e + 9]
        orth = ((np.abs((r0 * r0).sum(1) - 1.0) < ROT_TOL)
                & (np.abs((r1 * r1).sum(1) - 1.0) < ROT_TOL)
                & (np.abs((r2 * r2).sum(1) - 1.0) < ROT_TOL)
                & (np.abs((r0 * r1).sum(1)) < ROT_TOL)
                & (np.abs((r0 * r2).sum(1)) < ROT_TOL)
                & (np.abs((r1 * r2).sum(1)) < ROT_TOL))
        x, y, z = c0[:, 0], c0[:, 1], c0[:, 2]
        coord = ((np.abs(x) > 5.0) & (np.abs(x) < 7000.0)
                 & (y > -600.0) & (y < 5000.0)
                 & (np.abs(z) > 5.0) & (np.abs(z) < 7000.0))
        for i in np.nonzero(orth & coord)[0]:
            j = s + int(i)
            if j * 4 + 48 > size:
                continue
            out.append((base + j * 4, float(x[i]), float(y[i]), float(z[i])))
    np.seterr(**old)
    return out


def beep(times=3):
    try:
        import winsound
        for _ in range(times):
            winsound.Beep(880, 160)
            time.sleep(0.12)
    except Exception:
        pass


def locate_by_motion():
    """Fallback: structural candidates + walk-a-few-steps differencing."""
    ram_base, ram_size = guest_ram()
    print("  guest RAM: 0x%X  %.0f MB" % (ram_base, ram_size / 1048576.0))
    if not ram_size:
        return None
    # scan all large RW regions, not just the largest block
    regs = locate.regions(HANDLE)
    t0 = time.time()
    jobs = []
    for b, s in regs:
        off = 0
        while off < s:
            n = min(CHUNK, s - off)
            jobs.append((b + off, n))
            off += n
    hits = []
    with ThreadPoolExecutor(max_workers=THREADS) as ex:
        for r in ex.map(scan_chunk, jobs):
            hits.extend(r)
    baseline = {addr: (x, y, z) for addr, x, y, z in hits}
    addrs = list(baseline.keys())
    print("  scan: %d structural candidates in %.0fs"
          % (len(addrs), time.time() - t0))
    if not addrs:
        return None

    print("")
    print("  >>> WALK A FEW STEPS IN THE GAME NOW <<<")
    print("      (do NOT open the in-game map - it pauses the world)")
    beep()
    for i in range(COUNTDOWN, 0, -1):
        print("      sampling in %d ..." % i, flush=True)
        time.sleep(1.0)

    changed = []
    for a in addrs:
        d = read_mem(a, 12)
        if not d or len(d) != 12:
            continue
        x, y, z = struct.unpack("<3f", d)
        ox, oy, oz = baseline[a]
        if abs(x - ox) < 0.5 and abs(y - oy) < 0.5 and abs(z - oz) < 0.5:
            continue
        if not (abs(x) < 7000 and abs(z) < 7000 and -600 < y < 5000
                and (abs(x) > 5 or abs(z) > 5)):
            continue
        changed.append((a, x, y, z))
    print("  %d of %d addresses moved" % (len(changed), len(addrs)))
    if not changed:
        print("  nothing moved. Re-run and walk during the countdown.")
        return None

    cnt, sample = Counter(), {}
    for a, x, y, z in changed:
        k = (round(x, 1), round(y, 1), round(z, 1))
        cnt[k] += 1
        sample.setdefault(k, a)
    print("  top moved-value clusters:")
    for k, c in cnt.most_common(8):
        print("     %5d copies   mem=(%9.1f, %9.1f, %9.1f)"
              % (c, k[0], k[1], k[2]))
    k, c = cnt.most_common(1)[0]
    print("  player locked: copies=%d  mem=(%.1f, %.1f, %.1f)"
          % (c, k[0], k[1], k[2]))
    print("  address = 0x%X" % sample[k])
    return sample[k]


def poll(addr=None):
    """Mirror the locked address into STATE at 10 Hz."""
    while True:
        a = LOCK["addr"]
        d = read_mem(a, 12) if a else None
        v = decode_pos(d) if d else None
        if v:
            gx, gz, alt = v
            STATE.update(ok=True, gx=gx, gy=gz, gz=alt,
                         mx=2.0 * gx + 12000.0, my=2.0 * gz + 10000.0,
                         layer=0, age=time.time(),
                         verified=LOCK["verified"], copies=LOCK["copies"],
                         source=LOCK["source"])
        else:
            STATE["ok"] = False
            STATE["source"] = "locating..." if not a else "address lost"
        time.sleep(0.1)


def watch_shortlist(shortlist):
    """Lock onto the candidate group that actually ticks."""
    base = {}
    for g in shortlist:
        for a in g["addrs"]:
            d = read_mem(a, 12)
            if d and len(d) == 12:
                base[a] = struct.unpack("<3f", d)
    moved = [0] * len(shortlist)
    confirmed = -1
    while True:
        time.sleep(0.6)
        for gi, g in enumerate(shortlist):
            for a in g["addrs"]:
                d = read_mem(a, 12)
                if not d or len(d) != 12:
                    continue
                v = struct.unpack("<3f", d)
                b = base.get(a)
                base[a] = v
                if b is None:
                    continue
                if (abs(v[0] - b[0]) > 0.5 or abs(v[1] - b[1]) > 0.5
                        or abs(v[2] - b[2]) > 0.5):
                    moved[gi] += 1
        top = max(range(len(moved)), key=lambda i: moved[i])
        if moved[top] >= 3:
            if top != confirmed:
                confirmed = top
                a = shortlist[top]["addrs"][0]
                LOCK.update(addr=a, verified=True,
                            copies=shortlist[top]["copies"], source="scan")
                save_known([a] + load_known())
                print("  [watch] locked onto group %d: copies=%d struct=%d mem=%s"
                      % (top, shortlist[top]["copies"], shortlist[top]["struct"],
                         [round(x, 1) for x in shortlist[top]["hud"]]), flush=True)


def reopen_process():
    """Re-attach after the emulator itself was restarted."""
    global HANDLE, PID
    pid = find_pid(PROCESS_NAME)
    if not pid:
        return False
    if pid == PID and HANDLE:
        return True
    h = k32.OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, False, pid)
    if not h:
        return False
    HANDLE, PID = h, pid
    print("  [proc] re-attached to %s pid=%d" % (PROCESS_NAME, pid), flush=True)
    return True


def verify_known(addr, peers):
    """Background validation of a remembered address."""
    prev = decode_pos(read_mem(addr, 12))
    bad = 0
    while True:
        time.sleep(0.3)
        if LOCK["addr"] != addr:
            return                      # something else took over
        cur = decode_pos(read_mem(addr, 12))
        if cur is None:
            bad += 1
            if bad == 2:
                reopen_process()        # did Ryujinx itself restart?
            if bad >= 4:
                hit = try_offsets(verbose=False)
                if hit:
                    a, v = hit
                    print("  [verify] re-based offset 0x%08X onto new block "
                          "0x%012X -> 0x%X  mem=(%.1f, %.1f, %.1f)"
                          % ((a - GUEST_BASE, GUEST_BASE, a) + v), flush=True)
                    LOCK.update(addr=a, verified=False, copies=0, source="known")
                    prev, bad = v, 0
                    continue
            if bad >= 10:
                print("  [verify] address 0x%X went bad - re-locating" % addr,
                      flush=True)
                LOCK["addr"] = None
                LOCK["verified"] = False
                relocalize("remembered address became unreadable")
                return
            continue
        bad = 0
        if prev is not None and (abs(cur[0] - prev[0]) > 0.2
                                 or abs(cur[1] - prev[1]) > 0.2
                                 or abs(cur[2] - prev[2]) > 0.2):
            if not LOCK["verified"]:
                print("  [verify] address 0x%X is live (moved) -> verified"
                      % addr, flush=True)
                LOCK["verified"] = True
        prev = cur


def relocalize(reason):
    """Plan B: save-anchor scan + shortlist watcher, then remember the result."""
    if not reopen_process() or PID is None:
        print("  [relocate] %s -> Ryujinx not running" % reason, flush=True)
        return False
    print("  [relocate] %s -> running save-anchor scan" % reason, flush=True)
    try:
        res, log = locate.locate(PID)
    except Exception:
        import traceback
        print(traceback.format_exc(), flush=True)
        return False
    for line in log:
        print("    " + line, flush=True)
    if not res:
        print("  [relocate] scan found nothing", flush=True)
        return False
    addr = res["addr"]
    LOCK.update(addr=addr, verified=False, copies=res.get("copies", 0), source="scan")
    sl = res.get("shortlist", [])
    if sl:
        threading.Thread(target=watch_shortlist, args=(sl,), daemon=True).start()
    else:
        LOCK["verified"] = True
    save_known([addr] + load_known())
    print("  [relocate] candidate 0x%X mem=(%.1f, %.1f, %.1f) - being watched"
          % ((addr,) + tuple(res["hud"])), flush=True)
    return True


LAST_SCAN = [0.0]


def watchdog():
    """Keep a working lock without any user action."""
    while True:
        time.sleep(3)
        if LOCK["addr"] and STATE["ok"]:
            continue
        hit = try_offsets(verbose=False)
        if hit:
            a, v = hit
            first = LOCK["addr"] is None
            LOCK.update(addr=a, verified=False, copies=0, source="known")
            print("  [watchdog] recovered 0x%X (offset 0x%08X, block 0x%012X) "
                  "mem=(%.1f, %.1f, %.1f)"
                  % (a, a - GUEST_BASE, GUEST_BASE, v[0], v[1], v[2]), flush=True)
            if first:
                threading.Thread(target=verify_known, args=(a, []), daemon=True,
                                 name="verify").start()
            continue
        if time.time() - LAST_SCAN[0] > 180:
            LAST_SCAN[0] = time.time()
            relocalize("watchdog: no remembered offset works")


PROG_MTIME = [0.0]
# Navigation target picked on the web map: {"name", "x", "y", "type"} or None.
TARGET = [None]

# ---- map config: the WEB page is the only place settings live; the overlay
# ---- just reads this.  Only the names of enabled categories are stored.
CONFIG_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                           os.pardir, "data", "map_config.json")
DEFAULT_CONFIG = {
    "cats": ["神庙", "希卡塔", "克洛格果实"],
    "doneFilter": "all",        # all | done | todo
    "showAreas": True,
}
CONFIG = [None]
OVERLAY_SEEN = [0.0]            # last time the overlay polled us


def load_config():
    try:
        with open(CONFIG_FILE, encoding="utf-8") as f:
            cfg = json.load(f)
        merged = dict(DEFAULT_CONFIG)
        merged.update(cfg)
        return merged
    except Exception:
        return dict(DEFAULT_CONFIG)


def save_config(cfg):
    try:
        d = os.path.dirname(CONFIG_FILE)
        if not os.path.isdir(d):
            os.makedirs(d)
        with open(CONFIG_FILE, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=1)
    except Exception as e:
        print("  [config] save failed: %r" % (e,), flush=True)


def overlay_running(timeout=6.0):
    return (time.time() - OVERLAY_SEEN[0]) < timeout


def start_overlay():
    """Launch overlay.py detached, in the user's session."""
    script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "overlay.py")
    exe = os.path.join(os.path.dirname(sys.executable), "pythonw.exe")
    if not os.path.exists(exe):
        exe = sys.executable
    DETACHED_PROCESS = 0x00000008
    CREATE_NEW_PROCESS_GROUP = 0x00000200
    p = subprocess.Popen(
        [exe, script, ROOT, str(PORT)],
        creationflags=DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP,
        close_fds=True, cwd=os.path.dirname(script))
    print("  [overlay] started pid=%d via %s" % (p.pid, os.path.basename(exe)),
          flush=True)
    return p.pid


def newest_save_mtime():
    """Latest mtime over every game_data.sav."""
    if build_progress is None:
        return 0.0
    try:
        files = build_progress.prog.find_saves()
    except Exception:
        return 0.0
    m = 0.0
    for p in files:
        try:
            m = max(m, os.path.getmtime(p))
        except OSError:
            pass
    return m


def progress_watcher():
    """Rebuild data/progress_points.json whenever the save file changes."""
    while True:
        time.sleep(20)
        if build_progress is None:
            return
        try:
            mt = newest_save_mtime()
            if not mt:
                continue
            if PROG_MTIME[0] == 0.0:
                PROG_MTIME[0] = mt            # first sighting: only record it
                continue
            if mt != PROG_MTIME[0]:
                print("  [progress] save changed -> rebuilding", flush=True)
                build_progress.main()
                PROG_MTIME[0] = mt
        except Exception as e:
            print("  [progress] watcher error: %r" % (e,), flush=True)


class Handler(SimpleHTTPRequestHandler):
    def _json(self, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/rescan":
            threading.Thread(target=relocalize,
                             args=("/rescan requested",), daemon=True).start()
            self._json({"started": True})
            return
        if path == "/pos":
            if "who=overlay" in self.path:
                OVERLAY_SEEN[0] = time.time()      # overlay heartbeat
            if CONFIG[0] is None:
                CONFIG[0] = load_config()
            payload = dict(STATE)
            payload["target"] = TARGET[0]
            payload["config"] = CONFIG[0]          # overlay reads settings here
            self._json(payload)
            return
        if path == "/target":
            self._json(TARGET[0] or {"ok": False})
            return
        if path == "/config":
            if CONFIG[0] is None:
                CONFIG[0] = load_config()
            self._json(CONFIG[0])
            return
        if path == "/overlay/status":
            self._json({"running": overlay_running()})
            return
        super().do_GET()

    def do_POST(self):
        path = self.path.split("?")[0]
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n).decode("utf-8") if n else "{}"
        try:
            obj = json.loads(raw)
        except Exception:
            obj = {}

        if path == "/target":
            if obj.get("clear"):
                TARGET[0] = None
            else:
                TARGET[0] = {
                    "name": obj.get("name", ""),
                    "x": float(obj.get("x", 0)),
                    "y": float(obj.get("y", 0)),
                    "type": obj.get("type", ""),
                    "ts": time.time(),
                }
            print("  [target] %s" % (TARGET[0] or "cleared"), flush=True)
            self._json({"ok": True, "target": TARGET[0]})
            return

        if path == "/config":
            cur = CONFIG[0] or load_config()
            for k in ("cats", "doneFilter", "showAreas"):
                if k in obj:
                    cur[k] = obj[k]
            CONFIG[0] = cur
            save_config(cur)
            print("  [config] cats=%d doneFilter=%s showAreas=%s"
                  % (len(cur.get("cats") or []), cur.get("doneFilter"),
                     cur.get("showAreas")), flush=True)
            self._json({"ok": True, "config": cur})
            return

        if path == "/overlay/start":
            if overlay_running():
                self._json({"ok": True, "already": True})
                return
            try:
                pid = start_overlay()
                self._json({"ok": True, "pid": pid})
            except Exception as e:
                self._json({"ok": False, "error": str(e)})
            return

        self._json({"ok": False, "error": "unknown endpoint"})

    def do_OPTIONS(self):
        self.send_response(200)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def log_message(self, *a):
        pass


def main():
    global HANDLE
    print("=" * 62)
    print(" Zelda BOTW live map - auto locating player position")
    print("=" * 62)
    global PID
    pid = find_pid(PROCESS_NAME)
    if pid:
        PID = pid
        HANDLE = k32.OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ,
                                 False, pid)
        if not HANDLE:
            print("OpenProcess failed (err=%d). Try running as administrator."
                  % ctypes.get_last_error())
            return
        print("  pid = %d" % pid)
    else:
        print("  Ryujinx not running yet - starting anyway, the watchdog "
              "attaches as soon as it appears.")

    addr = None
    shortlist = []

    print("  stage 0: remembered offsets (no scan needed) ...")
    hit = try_known()
    if hit:
        a, v = hit
        LOCK.update(addr=a, verified=False, copies=0, source="known")
        print("  address 0x%X -> mem=(%.1f, %.1f, %.1f)   [fast path, no scan]"
              % (a, v[0], v[1], v[2]))
        print("  it is marked verified as soon as the value moves.")
    elif reopen_process():          # Ryujinx is actually up, so go hunting
        print("  stage 1: save anchor + anchored scan ...")
        if not (USE_SAVE and relocalize("remembered addresses unusable")):
            print("  stage 2: motion scan (walk a few steps when prompted)")
            a2 = locate_by_motion()
            if a2:
                LOCK.update(addr=a2, verified=True, copies=0, source="motion")
                save_known([a2] + load_known())
            else:
                print("  no luck yet - serving anyway, the watchdog keeps trying.")
    else:
        # The map page and the collection progress do not need the emulator at
        # all, so never refuse to start just because the game is not running.
        print("  Ryujinx is not running yet - serving anyway.")
        print("  Map + collection progress work now; the live dot starts as")
        print("  soon as the emulator appears (watchdog, no restart needed).")

    threading.Thread(target=poll, daemon=True).start()
    threading.Thread(target=watchdog, daemon=True).start()
    threading.Thread(target=progress_watcher, daemon=True).start()
    if LOCK["addr"]:
        threading.Thread(target=verify_known, args=(LOCK["addr"], []),
                         daemon=True, name="verify").start()
    time.sleep(0.5)
    print("")
    print("  serving ->", json.dumps(STATE))
    url = "http://127.0.0.1:%d/app/index.html" % PORT
    print("  live map -> %s" % url)
    print("  (Ctrl+C to stop)")
    if AUTO_OPEN:
        try:
            webbrowser.open(url)
        except Exception:
            pass

    os.chdir(ROOT)
    ThreadingHTTPServer(("127.0.0.1", PORT), partial(Handler)).serve_forever()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nstopped.")
