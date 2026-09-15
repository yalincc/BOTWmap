# -*- coding: utf-8 -*-
"""Build data/save_flags.json: { flag_hash: [t, marker_id] } for the web save parser.

Reads data/progress_points.json (produced by build_progress.py, which now also
carries memories + beasts) and matches each point to its data.js marker by the
same grid rule the front-end uses (128px cell, 3x3 neighbourhood, 8px radius,
same type). Points are written with the marker px itself, so the match is exact.

The lookup table is save-independent -> safe to ship on the static site.
"""
import io
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
DATA_JS = os.path.join(ROOT, "app", "js", "data.js")
PROG_JSON = os.path.join(ROOT, "data", "progress_points.json")
OUT = os.path.join(ROOT, "data", "save_flags.json")

POINT_TYPES = {  # point t -> marker cat
    "shrine": "shrine", "tower": "tower", "korok": "seed",
    "memory": "memory", "beast": "beast",
}


def load_pins():
    src = io.open(DATA_JS, encoding="utf-8").read()
    m = re.search(r"BOTW_DATA = (\{.*\})", src, re.S)
    return json.loads(m.group(1))["markers"]


def main():
    with io.open(PROG_JSON, encoding="utf-8") as f:
        prog = json.load(f)
    markers = load_pins()
    by_type = {}
    for mk in markers:
        by_type.setdefault(mk.get("cat"), []).append(mk)

    CELL = 128.0
    RADIUS = 8.0
    out = {}
    miss = []
    for p in prog.get("points", []):
        t = p.get("t")
        cat = POINT_TYPES.get(t)
        if cat is None:
            continue
        # 神庙用完成判定哈希（Clear_DungeonNNN），其余类型 point hash 即完成哈希
        key_hash = p.get("done_hash") if p.get("t") == "shrine" else p.get("hash")
        if not key_hash:
            miss.append((p.get("t"), p.get("hash"), p.get("en"), 0))
            continue
        hits = [mk for mk in by_type.get(cat, [])
                if abs(mk["px"][0] - p["x"]) <= RADIUS and abs(mk["px"][1] - p["y"]) <= RADIUS]
        if len(hits) != 1:
            miss.append((p.get("t"), p.get("hash"), p.get("en"), len(hits)))
            continue
        out[str(key_hash)] = [t, hits[0]["id"]]

    with io.open(OUT, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

    print("wrote %s (%d flags)" % (OUT, len(out)))
    if miss:
        print("  unmatched:", miss[:10])
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
