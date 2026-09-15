"""Headless render test for the BOTW overlay port (no Tk window needed)."""
import os, sys, math, time
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import overlay  # noqa

# --- 1. markers / progress load ---
m = overlay.load_markers()
print("markers cats:", {k: len(v) for k, v in m.items()})
prog, counts = overlay.load_progress()
print("progress points:", len(prog), "counts:", counts)
assert len(m["神庙"]) == 137, len(m["神庙"])  # 136 real + 复苏神庙 (story)
assert len(m["希卡塔"]) == 15
assert len(m["克洛格果实"]) == 900

# --- 2. tile formula check: a known tile ---
# player at map px (15634.5, 12028.4) ~ mem (1817.2, 222.7, 1014.2)
# native zoom 7: tile = floor((px + offset)/256)
import glob
mx, my = 15634.5, 12028.4
c0 = int((mx + overlay.TILE_OX) // 256); r0 = int((my + overlay.TILE_OY) // 256)
print("centre tile z7:", c0, r0)
p = os.path.join(overlay.TILES, "7_%d_%d.webp" % (c0, r0))
print("centre tile exists:", os.path.exists(p), p)

# --- 3. offline render at zooms 5..9 ---
from PIL import Image
class Fake:
    zoom = 7
    markers = m
    cats_on = {"神庙", "希卡塔", "克洛格果实"}
    done_filter = "all"
    show = {"nav": True}
    target = None
    trail = []
    cache = None
    def __init__(self):
        self.cache = __import__("collections").OrderedDict()
        self.done_set = set()
        for px, py, _k, dn, _nm in prog:
            if dn:
                self.done_set.add((int(round(px)), int(round(py))))
    def tile(self, z, col, row): return overlay.Overlay.tile(self, z, col, row)
    def is_done(self, x, y): return overlay.Overlay.is_done(self, x, y)

f = Fake()
for z in (5, 6, 7, 8, 9):
    f.zoom = z
    t0 = time.time()
    img = overlay.Overlay.render(f, mx, my)
    dt = time.time() - t0
    # count non-background pixels to make sure tiles actually drew
    px = img.load()
    filled = 0
    for yy in range(0, 320, 8):
        for xx in range(0, 320, 8):
            if px[xx, yy][:3] != (14, 20, 28):
                filled += 1
    print("z=%d render %.3fs cache=%d filled=%d/1600" % (z, dt, len(f.cache), filled))
    img.save(r"E:\WorkSpace\BOTWmap\live\_research\test_render_z%d.png" % z)
print("OK")
