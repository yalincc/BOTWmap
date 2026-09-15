"""Auto-locate the player's coordinate address in Ryujinx guest RAM (BOTW).

Same strategy as the TOTK version (ZeldaTOTKmap/live/locate.py), adapted:

  1. ANCHOR - read the newest BOTW save (`slot_XX/game_data.sav`) for the last
     known player position.  The save stores PLAYER_POSITION (hash 0xA40BA103)
     as a vector3f in three 8-byte chunks:
         [id][f0] [id][f1] [id][f2]  ->  f0 at +4, f1 at +12, f2 at +20
     with (f0, f1, f2) = (X east, Y altitude, Z south+).  The in-memory Actor
     position uses the same engine convention, so the anchor refs are the save
     triple verbatim (no elevation bias, no axis negation - to be re-confirmed
     by calibration).
  2. RELOCATE - scan guest RAM for float32 triples inside a window around that
     anchor and pick the value with the most copies, then let the runtime
     liveness watcher confirm which group actually moves.

BOTW world bounds used for sanity checks: X/Z in (-7000, 7000), alt in
(-600, 5000).

usage:
    python locate.py <pid> [--window N] [--json] [--out FILE]
"""
import ctypes
import json
import os
import struct
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from ctypes import wintypes

import numpy as np

np.seterr(all="ignore")     # NaN/inf in guest RAM is normal; silence the noise

SAVE_ROOT = os.path.expandvars(r"%APPDATA%\Ryujinx\bis\user\save")
PLAYER_POSITION = 0xA40BA103
MIRROR_STEP = 0x800000000   # Ryujinx's second view of guest RAM, 32 GB higher
CHUNK = 64 << 20            # 64 MB per read
THREADS = 16
HIT_CAP = 3_000_000         # safety valve: stop collecting if window too wide
MIN_MB = 512.0              # only scan RW regions at least this large

PROCESS_QUERY_INFORMATION = 0x0400
PROCESS_VM_READ = 0x0010
MEM_COMMIT = 0x1000
MEM_MAPPED = 0x40000


class MEMORY_BASIC_INFORMATION(ctypes.Structure):
    _fields_ = [
        ("BaseAddress", ctypes.c_void_p),
        ("AllocationBase", ctypes.c_void_p),
        ("AllocationProtect", wintypes.DWORD),
        ("PartitionId", wintypes.WORD),
        ("RegionSize", ctypes.c_size_t),
        ("State", wintypes.DWORD),
        ("Protect", wintypes.DWORD),
        ("Type", wintypes.DWORD),
    ]


k32 = ctypes.windll.kernel32
k32.OpenProcess.restype = wintypes.HANDLE
k32.VirtualQueryEx.argtypes = [wintypes.HANDLE, ctypes.c_void_p,
                               ctypes.POINTER(MEMORY_BASIC_INFORMATION),
                               ctypes.c_size_t]
k32.VirtualQueryEx.restype = ctypes.c_size_t


def open_process(pid):
    h = k32.OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, False, pid)
    if not h:
        raise RuntimeError("OpenProcess failed (err=%d)" % ctypes.get_last_error())
    return h


def regions(h, min_mb=MIN_MB):
    """Every committed RW MEM_MAPPED region big enough to be guest RAM.

    BOTW on Ryujinx maps its DRAM as one huge RW block, but a second 3 GB
    block may coexist (measured 2026-09-15), so unlike the TOTK version we
    scan ALL large RW blocks and drop the 32 GB-higher mirror.
    """
    mbi = MEMORY_BASIC_INFORMATION()
    addr, raw, out = 0, [], []
    LIMIT = 0x7FFFFFFFFFFF
    while addr < LIMIT:
        n = k32.VirtualQueryEx(h, ctypes.c_void_p(addr), ctypes.byref(mbi),
                               ctypes.sizeof(mbi))
        if n == 0:
            break
        base = mbi.BaseAddress or 0
        size = mbi.RegionSize
        if (mbi.State == MEM_COMMIT and mbi.Type == MEM_MAPPED
                and (mbi.Protect & 0xFF) in (0x02, 0x04)
                and size >= min_mb * 1048576.0):
            raw.append((base, size))
        addr = base + size
    base_set = {b for b, _s in raw}
    for b, s in raw:
        if (b + MIRROR_STEP) not in base_set:
            out.append((b, s))
    return out


def sane(gx, gz, alt):
    return (-7000.0 < gx < 7000.0 and -7000.0 < gz < 7000.0
            and -600.0 < alt < 5000.0) and (abs(gx) > 1.0 or abs(gz) > 1.0)


def read_save_triples():
    """[(path, playtime, (X, alt, Z))] from every game_data.sav.

    All six slots are rewritten together (one mtime), so recency is judged by
    playtime: the highest-playtime slot is the most advanced save point and the
    one the player is closest to right now.  Sorted by playtime, newest first.
    """
    PLAYTIME_HASH = 0x73C29681
    cands = []
    for dirpath, _dn, fns in os.walk(SAVE_ROOT):
        for fn in fns:
            if fn == "game_data.sav":
                p = os.path.join(dirpath, fn)
                try:
                    cands.append((os.path.getmtime(p), p))
                except OSError:
                    pass
    cands.sort(reverse=True)
    out, seen = [], []
    for _mt, p in cands:
        try:
            with open(p, "rb") as f:
                data = f.read()
        except OSError:
            continue
        i = data.find(struct.pack("<I", PLAYER_POSITION))
        if i < 0 or i + 24 > len(data):
            continue
        f0 = struct.unpack_from("<f", data, i + 4)[0]
        f1 = struct.unpack_from("<f", data, i + 12)[0]
        f2 = struct.unpack_from("<f", data, i + 20)[0]
        if not sane(f0, f2, f1):
            continue
        key = (round(f0, 1), round(f2, 1), round(f1, 1))
        if key in seen:                 # slots share mtime; dedupe by position
            continue
        seen.append(key)
        j = data.find(struct.pack("<I", PLAYTIME_HASH))
        pt = struct.unpack_from("<I", data, j + 4)[0] if j >= 0 else 0
        out.append((p, pt, (f0, f1, f2)))
    out.sort(key=lambda t: -t[1])
    return out


def pick_primary(anchors):
    """Most advanced save point = highest playtime slot (single anchor).

    With one anchor the scan window is tight (the player is near their last
    save), which keeps the scan fast; six spread-out anchors would widen the
    union window to nearly the whole map and the per-hit distance loop would
    crawl.  The motion-scan fallback covers the "player walked far away" case.
    """
    if not anchors:
        return None
    return anchors[0]


def anchor_refs(anchors, limit=8, merge=4.0):
    """Distinct memory-space refs (X, alt, Z), merging nearby slots."""
    out = []
    for _p, hud in anchors:
        if all((abs(hud[0] - o[0]) > merge or abs(hud[1] - o[1]) > merge
                or abs(hud[2] - o[2]) > merge) for o in out):
            out.append(hud)
        if len(out) >= limit:
            break
    return out


def scan_region(h, base, size, refs, window, hits):
    """Collect float32 triples within `window` of ANY anchor ref (X, alt, Z)."""
    np.seterr(all="ignore")
    if not isinstance(refs, list):
        refs = [refs]
    gx0 = min(r[0] for r in refs) - window
    gx1 = max(r[0] for r in refs) + window
    gh0 = min(r[1] for r in refs) - window
    gh1 = max(r[1] for r in refs) + window
    gz0 = min(r[2] for r in refs) - window
    gz1 = max(r[2] for r in refs) + window
    off = 0
    while off < size:
        if len(hits) > HIT_CAP:
            return
        n = min(CHUNK, size - off)
        buf = ctypes.create_string_buffer(n)
        got = ctypes.c_size_t()
        ok = k32.ReadProcessMemory(h, ctypes.c_void_p(base + off), buf, n,
                                   ctypes.byref(got))
        if ok and got.value >= 12:
            raw = buf.raw[:got.value]
            a = np.frombuffer(raw[:len(raw) // 4 * 4], dtype="<f4")
            if len(a) >= 3:
                ax, ah, az = a[:-2], a[1:-1], a[2:]
                idx = np.nonzero((ax > gx0) & (ax < gx1))[0]
                if idx.size:
                    keep = (ah[idx] > gh0) & (ah[idx] < gh1)
                    idx = idx[keep]
                if idx.size:
                    keep = (az[idx] > gz0) & (az[idx] < gz1)
                    idx = idx[keep]
                if idx.size:
                    axs, ahs, azs = ax[idx], ah[idx], az[idx]
                    for j in range(idx.size):
                        x, y, z = float(axs[j]), float(ahs[j]), float(azs[j])
                        ri, rd = 0, 1e18
                        for kk, r in enumerate(refs):
                            d = ((x - r[0]) ** 2 + (y - r[1]) ** 2
                                 + (z - r[2]) ** 2) ** 0.5
                            if d < rd:
                                ri, rd = kk, d
                        if rd <= window:
                            hits.append((base + off + int(idx[j]) * 4,
                                         x, y, z, ri, rd))
        off += n


def rot_ok(h, addr):
    """Orthonormal 3x3 float matrix within 64 bytes after addr (ActorBase)."""
    buf = ctypes.create_string_buffer(128)
    got = ctypes.c_size_t()
    if not k32.ReadProcessMemory(h, ctypes.c_void_p(addr), buf, 128,
                                 ctypes.byref(got)) or got.value < 64:
        return False
    a = np.frombuffer(buf.raw[:got.value // 4 * 4], dtype="<f4").astype(np.float64)
    for st in range(4, 17):
        if st + 9 > len(a):
            break
        m = a[st:st + 9].reshape(3, 3)
        cols = m.T
        norms = np.linalg.norm(cols, axis=0)
        if not np.all(np.abs(norms - 1.0) < 0.05):
            continue
        dots = [abs(float(np.dot(cols[i], cols[j])))
                for i in range(3) for j in range(i + 1, 3)]
        if max(dots) < 0.08:
            return True
    return False


def say(log, text):
    log.append(text)
    print(text, flush=True)


def locate(pid, window=60.0, log=None):
    log = log if log is not None else []
    t0 = time.time()
    anchors = read_save_triples()
    if not anchors:
        say(log, "no usable save anchor found")
        return None, log
    # One anchor: the most advanced save point.  Tight window -> fast scan.
    _p, pt, primary = anchors[0]
    refs = [primary]
    say(log, "save anchor: pt=%d  mem=(%.1f, %.1f, %.1f)" % ((pt,) + tuple(primary)))
    if len(anchors) > 1:
        say(log, "  (%d more save slots exist; not used - they are old "
                 "positions and only widen the scan)" % (len(anchors) - 1))

    h = open_process(pid)
    regs = regions(h)
    say(log, "RW regions>=%.0fMB: %d  (%.1f GB)"
        % (MIN_MB, len(regs), sum(s for _b, s in regs) / 2**30))
    for b, s in regs:
        say(log, "  region 0x%012X  %8.0f MB" % (b, s / 1048576.0))

    best = None
    shortlist = []
    for attempt, win in enumerate((max(window, 120.0), 400.0), 1):
        hits = []
        with ThreadPoolExecutor(max_workers=THREADS) as ex:
            futs = [ex.submit(scan_region, h, b, s, refs, win, hits) for b, s in regs]
            for f in futs:
                f.result()
        say(log, "pass %d (window +/-%.0f): %d triple hits in %.1fs"
            % (attempt, win, len(hits), time.time() - t0))
        if not hits:
            continue
        groups = {}
        for addr, x, y, z, ri, rd in hits:
            key = (round(x, 1), round(y, 1), round(z, 1))
            g = groups.setdefault(key, {"addrs": [], "ri": ri, "dist": rd})
            g["addrs"].append(addr)
            if rd < g["dist"]:
                g["dist"], g["ri"] = rd, ri
        ranked = sorted(groups.items(), key=lambda kv: -len(kv[1]["addrs"]))
        say(log, "  top groups (copies, struct-hit, dist to its own slot anchor):")
        scored = []
        for key, g in ranked[:30]:
            st = sum(1 for a in g["addrs"][:64] if rot_ok(h, a))
            scored.append({"key": key, "addrs": g["addrs"], "struct": st,
                           "dist": g["dist"], "ri": g["ri"],
                           "copies": len(g["addrs"])})
            if len(scored) <= 12:
                say(log, "    x%-5d struct %2d  d=%6.1f  slot#%d  mem=(%.1f, %.1f, %.1f)"
                    % (len(g["addrs"]), st, g["dist"], g["ri"],
                       key[0], key[1], key[2]))
        scored.sort(key=lambda s: (-(s["struct"] > 0), -s["struct"],
                                   s["dist"], -s["copies"]))
        best = scored[0]
        shortlist = [{"hud": [s["key"][0], s["key"][2], s["key"][1]],
                      "copies": s["copies"], "struct": s["struct"],
                      "dist": s["dist"], "slot": s["ri"],
                      "addrs": s["addrs"][:24]} for s in scored]
        if best["struct"] > 0:
            break
    if best is None:
        say(log, "FAILED: no confident candidate")
        return None, log
    key, addr = best["key"], best["addrs"][0]
    say(log, "candidate 0x%012X  copies=%d struct=%d  mem=(%.1f, %.1f, %.1f)  [pending liveness]"
        % (addr, best["copies"], best["struct"], key[0], key[1], key[2]))
    say(log, "shortlist: %d groups, %d addresses"
        % (len(shortlist), sum(len(g["addrs"]) for g in shortlist)))
    say(log, "total %.1fs" % (time.time() - t0))
    return {"addr": addr, "copies": best["copies"], "struct": best["struct"],
            "hud": (key[0], key[2], key[1]),
            "shortlist": shortlist, "log": log}, log


def main():
    pid = int(sys.argv[1])
    window = 60.0
    out = None
    logfile = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                           "_research", "locate_log.txt")
    as_json = "--json" in sys.argv
    for i, a in enumerate(sys.argv):
        if a == "--window":
            window = float(sys.argv[i + 1])
        if a == "--out":
            out = sys.argv[i + 1]
        if a == "--logfile":
            logfile = sys.argv[i + 1]
    try:
        res, log = locate(pid, window)
    except Exception:
        import traceback
        tb = "EXCEPTION\n" + traceback.format_exc()
        print(tb, flush=True)
        if logfile:
            with open(logfile, "w", encoding="utf-8") as f:
                f.write(tb)
        sys.exit(3)
    if logfile:
        with open(logfile, "w", encoding="utf-8") as f:
            f.write("\n".join(log))
    if res is None:
        sys.exit(2)
    if out:
        with open(out, "w", encoding="utf-8") as f:
            json.dump(res, f)
    if as_json:
        print(json.dumps({"addr": "0x%012X" % res["addr"], "copies": res["copies"],
                          "struct": res["struct"], "hud": res["hud"]}))


if __name__ == "__main__":
    main()
