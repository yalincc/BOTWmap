"""Merge BOTW save progress + collectible data + map pins -> progress_points.json.

Inputs
    _research/koroks.json       900 koroks (hash, flag, x, y) from lud99
    _research/shrines.json      120 shrines (hash, name, x, y) from lud99
    _research/dlc_shrines.json  16 DLC shrines (hash, x, y) from lud99
    _research/towers.json       15 towers (n, en) from waypoint LocationMarker.csv
    _research/zelda-botw.hashes.csv  flag hash -> name (marcrobledo)
    app/js/data.js              BOTWmap pins (px coords + Chinese names)
    %APPDATA%/Ryujinx/.../game_data.sav  the save to read

Output
    data/progress_points.json   flat list for the front-end / overlay

Coordinate system (verified 2026-09-15 against the user's save):
    lud99 / save position = (X east, Y altitude, Z south+)
    map px = (2 * X + 12000, 2 * Z + 10000)
"""
import io
import json
import os
import re
import sys
import time
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path.insert(0, HERE)

import progress as prog           # noqa: E402  (same folder)

RES = os.path.join(HERE, "_research")
DATA_JS = os.path.join(ROOT, "app", "js", "data.js")
OUT = os.path.join(ROOT, "data", "progress_points.json")


def crc32(s):
    return zlib.crc32(s.encode("utf-8")) & 0xFFFFFFFF


def load_pins():
    src = io.open(DATA_JS, encoding="utf-8").read()
    m = re.search(r"BOTW_DATA = (\{.*\})", src, re.S)
    d = json.loads(m.group(1))
    return d["markers"]


def load_hashnames():
    """hash -> internal flag name (marcrobledo hashes.csv)."""
    m = {}
    with io.open(os.path.join(RES, "zelda-botw.hashes.csv"), encoding="utf-8") as f:
        for line in f:
            parts = line.rstrip("\n").split(";")
            if len(parts) >= 3:
                m[int(parts[0])] = parts[2]
    return m


def px_of(x, z):
    return (2.0 * x + 12000.0, 2.0 * z + 10000.0)


def nearest(pins, mx, my, limit=None):
    best, bd = None, 1e18
    for pi in pins:
        d = ((pi["px"][0] - mx) ** 2 + (pi["px"][1] - my) ** 2) ** 0.5
        if d < bd:
            best, bd = pi, d
    if limit is not None and bd > limit:
        return None, bd
    return best, bd


def match_bijection(src, pins, radius):
    """src: [(key, mx, my)]   pins: [marker dict]  ->  {key: (marker, dist)}

    Greedy nearest pair walking; near-1:1 so this reproduces the optimum.
    """
    cand = []
    for key, mx, my in src:
        for pi in pins:
            d = ((pi["px"][0] - mx) ** 2 + (pi["px"][1] - my) ** 2) ** 0.5
            if d <= radius:
                cand.append((d, key, id(pi), pi))
    cand.sort(key=lambda t: t[0])
    used_key, used_pin, out = set(), set(), {}
    for d, key, pid, pi in cand:
        if key in used_key or pid in used_pin:
            continue
        used_key.add(key)
        used_pin.add(pid)
        out[key] = (pi, d)
    return out


def main():
    path, meta = prog.pick_save()
    if not path:
        print("no save found")
        return 1
    entries, meta = prog.parse_save(path)
    names = load_hashnames()
    markers = load_pins()

    shrine_mk = [x for x in markers if x["cat"] == "shrine"]
    tower_mk = [x for x in markers if x["cat"] == "tower"]
    seed_mk = [x for x in markers if x["cat"] == "seed"]

    shrines = json.load(io.open(os.path.join(RES, "shrines.json"), encoding="utf-8"))
    dlc = json.load(io.open(os.path.join(RES, "dlc_shrines.json"), encoding="utf-8"))
    koroks = json.load(io.open(os.path.join(RES, "koroks.json"), encoding="utf-8"))
    towers = json.load(io.open(os.path.join(RES, "towers.json"), encoding="utf-8"))

    def clear_hash(sh_hash):
        """Clear_DungeonNNN hash for a Location_DungeonNNN hash."""
        nm = names.get(sh_hash)
        m = re.match(r"Location_Dungeon(\d+)", nm or "")
        if not m:
            return None
        return crc32("Clear_Dungeon" + m.group(1))

    points = []

    # ---- shrines: name match first, coordinate bijection as fallback ----
    shrine_objs = ([{"hash": s["hash"], "name": s["name"],
                     "x": s["x"], "y": s["y"], "dlc": False} for s in shrines] +
                   [{"hash": s["hash"], "name": s.get("name", ""),
                     "x": s["x"], "y": s["y"], "dlc": True} for s in dlc])
    by_en = {}
    for mk in shrine_mk:
        en = (mk.get("en") or "").strip()
        if en and en not in by_en:
            by_en[en] = mk
    name_hits = 0
    unmatched_objs = []
    for s in shrine_objs:
        if s["name"] in by_en:
            name_hits += 1
    # coordinate fallback for unnamed / unmatched
    missing = [s for s in shrine_objs if s["name"] not in by_en]
    unused_pins = [mk for mk in shrine_mk
                   if (mk.get("en") or "").strip() not in by_en.values()]
    bi = match_bijection([(s["hash"],) + px_of(s["x"], s["y"]) for s in missing],
                         unused_pins, 60.0)
    n_sh = 0
    for s in shrine_objs:
        mk = by_en.get(s["name"])
        if mk is None:
            hit = bi.get(s["hash"])
            if hit:
                mk = hit[0]
        if mk is None:
            print("  [shrine] unmatched:", s["name"] or s["hash"], file=sys.stderr)
            continue
        ch = clear_hash(s["hash"])
        done = ch is not None and entries.get(ch, 0) != 0
        entered = entries.get(s["hash"], 0) != 0
        points.append({
            "t": "shrine", "hash": s["hash"], "done_hash": ch,
            "en": s["name"], "cn": mk.get("name") or mk.get("en") or "",
            "x": round(mk["px"][0], 3), "y": round(mk["px"][1], 3),
            "done": done, "entered": entered, "dlc": s["dlc"],
        })
        n_sh += 1

    # ---- towers: match by en name ----
    tw_en = {t["en"]: t for t in towers}
    n_tw = 0
    for mk in tower_mk:
        en = (mk.get("en") or "").strip()
        t = tw_en.get(en)
        if t is None:
            print("  [tower] no id for marker:", en, file=sys.stderr)
            continue
        h = crc32("MapTower_%02d" % t["n"])
        points.append({
            "t": "tower", "hash": h,
            "en": en, "cn": mk.get("name") or en,
            "x": round(mk["px"][0], 3), "y": round(mk["px"][1], 3),
            "done": entries.get(h, 0) != 0,
        })
        n_tw += 1

    # ---- koroks: coordinate bijection ----
    used_seeds = set()
    n_ko = 0
    for k in koroks:
        mx, my = px_of(k["x"], k["y"])
        best, bd = None, 1e18
        for pi in seed_mk:
            if id(pi) in used_seeds:
                continue
            d = ((pi["px"][0] - mx) ** 2 + (pi["px"][1] - my) ** 2) ** 0.5
            if d < bd:
                best, bd = pi, d
        if best is None or bd > 80.0:
            print("  [korok] unmatched:", k["hash"], "dist=%.1f" % bd, file=sys.stderr)
            continue
        used_seeds.add(id(best))
        points.append({
            "t": "korok", "hash": k["hash"],
            "en": best.get("id") or k["flag"], "cn": best.get("name") or best.get("id") or "",
            "x": round(best["px"][0], 3), "y": round(best["px"][1], 3),
            "done": entries.get(k["hash"], 0) != 0,
        })
        n_ko += 1

    # ---- memories: 全量 23 个，全自动逐点判定 ----
    # 照片回忆 13 个用「影片已播放」flag IsPlayed_Demo126_0~138_0 逐点判定，替代旧的
    # IsGet_MemoryPhoto_000~012（= 照片入手 flag，英帕给照片即置1，不等于已恢复）。
    # 编号→flag 映射经 zeldamods Demo 表 + 存档三方互证：
    #   其1→126 其3→127 其5→128 其7→129 其8→130 其9→131 其14→132 其11→133
    #   其12→134 其13→135 其15→136 其16→137 其17(最终)→138
    # 英杰(其2/4/6/10)=Clear_Remains*；大师剑(其18)=Get_MasterSword_Finish；EX=BalladOfHero*_Finish
    # 交叉校验：照片已恢复数 应 == 12 - PictureMemory_Spot_Int（还剩几处未造访）
    MEMORY_FLAGS = [
        # (游戏内回忆编号 or None, tier, flag, en 子串匹配[仅 EX])
        (1,  "photo", "IsPlayed_Demo126_0", None),     # 其1 空有形式的仪式
        (3,  "photo", "IsPlayed_Demo127_0", None),     # 其3 决意与苦恼
        (5,  "photo", "IsPlayed_Demo128_0", None),     # 其5 萨尔达的焦躁
        (7,  "photo", "IsPlayed_Demo129_0", None),     # 其7 依盖队之凶刃
        (8,  "photo", "IsPlayed_Demo130_0", None),     # 其8 预兆
        (9,  "photo", "IsPlayed_Demo131_0", None),     # 其9 宁静公主
        (14, "photo", "IsPlayed_Demo132_0", None),     # 其14 前往拉聂尔山
        (11, "photo", "IsPlayed_Demo133_0", None),     # 其11 躲雨
        (12, "photo", "IsPlayed_Demo134_0", None),     # 其12 父与女
        (13, "photo", "IsPlayed_Demo135_0", None),     # 其13 未觉醒的力量
        (15, "photo", "IsPlayed_Demo136_0", None),     # 其15 大灾厄复活
        (16, "photo", "IsPlayed_Demo137_0", None),     # 其16 绝望
        (17, "photo", "IsPlayed_Demo138_0", None),     # 其17 萨尔达觉醒（最终，照片13）
        (2,  "auto",  "Clear_RemainsWind", None),      # 其2 英杰 力巴尔
        (4,  "auto",  "Clear_RemainsFire", None),      # 其4 英杰 达尔克尔
        (6,  "auto",  "Clear_RemainsElectric", None),  # 其6 英杰 乌尔波札
        (10, "auto",  "Clear_RemainsWater", None),     # 其10 英杰 米法
        (18, "auto",  "Get_MasterSword_Finish", None), # 其18 大师之剑
        (None, "dlc", "BalladOfHeroRito_Finish", "Revali"),    # EX 其1 力巴尔之诗
        (None, "dlc", "BalladOfHeroGoron_Finish", "Daruk"),    # EX 其2 达尔克尔之诗
        (None, "dlc", "BalladOfHeroZora_Finish", "Mipha"),     # EX 其3 米法之诗
        (None, "dlc", "BalladOfHeroGerudo_Finish", "Urbosa"),  # EX 其4 乌尔波扎之诗
        (None, "dlc", "BalladOfHeroes_Finish", "Champions"),   # EX 其5 英杰之诗
    ]
    mem_mk = {}
    for x in markers:
        if x["cat"] != "memory":
            continue
        ex = x.get("extra") or {}
        if ex.get("tier") == "dlc":
            mem_mk.setdefault("dlc:" + (x.get("en") or ""), x)
            continue
        m = re.search(r"#(\d+)", x.get("name") or "")
        if m:
            mem_mk.setdefault(int(m.group(1)), x)
    n_mem = 0
    for no, tier, flag, en_sub in MEMORY_FLAGS:
        if en_sub:
            mk = next((v for k, v in mem_mk.items()
                       if isinstance(k, str) and en_sub in k), None)
        else:
            mk = mem_mk.get(no)
        if mk is None:
            print("  [memory] no marker for", no or en_sub, file=sys.stderr)
            continue
        h = crc32(flag)
        points.append({
            "t": "memory", "hash": h, "tier": tier,
            "photoNo": mk.get("extra", {}).get("photoNo"),
            "en": mk.get("en") or "", "cn": mk.get("name") or "",
            "x": round(mk["px"][0], 3), "y": round(mk["px"][1], 3),
            "done": entries.get(h, 0) != 0,
        })
        n_mem += 1

    # ---- beasts: Clear_Remains*（神兽内部名 Remains） ----
    beast_mk = {x.get("en"): x for x in markers if x["cat"] == "beast"}
    beast_flags = [
        ("Divine Beast Vah Ruta", "Clear_RemainsWater"),
        ("Divine Beast Vah Medoh", "Clear_RemainsWind"),
        ("Divine Beast Vah Rudania", "Clear_RemainsFire"),
        ("Divine Beast Vah Naboris", "Clear_RemainsElectric"),
    ]
    n_beast = 0
    for en, fl in beast_flags:
        mk = beast_mk.get(en)
        if mk is None:
            print("  [beast] no marker:", en, file=sys.stderr)
            continue
        h = crc32(fl)
        points.append({
            "t": "beast", "hash": h,
            "en": en, "cn": mk.get("name") or en,
            "x": round(mk["px"][0], 3), "y": round(mk["px"][1], 3),
            "done": entries.get(h, 0) != 0,
        })
        n_beast += 1

    # ---- summary ----
    n_sh_list = [p for p in points if p["t"] == "shrine"]
    n_tw_list = [p for p in points if p["t"] == "tower"]
    n_ko_list = [p for p in points if p["t"] == "korok"]
    n_mem_list = [p for p in points if p["t"] == "memory"]
    n_beast_list = [p for p in points if p["t"] == "beast"]
    # 回忆拆档：照片12（不含最终）/ 最终1（照片13）/ 主线自动5（英杰4+大师剑1）/ DLC EX5
    mem_photo = [p for p in n_mem_list if p.get("tier") == "photo" and p.get("photoNo") != 13]
    mem_final = [p for p in n_mem_list if p.get("tier") == "photo" and p.get("photoNo") == 13]
    mem_auto = [p for p in n_mem_list if p.get("tier") == "auto"]
    mem_dlc = [p for p in n_mem_list if p.get("tier") == "dlc"]
    H_SPOT = crc32("PictureMemory_Spot_Int")   # s32 倒计时：还剩几处未造访
    spot_int = entries.get(H_SPOT, 0)
    obj = {
        "generated": time.strftime("%Y-%m-%d %H:%M:%S"),
        "save": path,
        "playtime": meta["playtime"],
        "korok_counter": meta["korok_counter"],
        "spot_int": spot_int,
        "counts": {
            "shrine": [sum(1 for p in n_sh_list if p["done"]), len(n_sh_list)],
            "tower": [sum(1 for p in n_tw_list if p["done"]), len(n_tw_list)],
            "korok": [sum(1 for p in n_ko_list if p["done"]), len(n_ko_list)],
            "memory": [sum(1 for p in n_mem_list if p["done"]), len(n_mem_list)],
            "beast": [sum(1 for p in n_beast_list if p["done"]), len(n_beast_list)],
            "memory_photo": [sum(1 for p in mem_photo if p["done"]), len(mem_photo)],
            "memory_final": [sum(1 for p in mem_final if p["done"]), len(mem_final)],
            "memory_auto": [sum(1 for p in mem_auto if p["done"]), len(mem_auto)],
            "memory_dlc": [sum(1 for p in mem_dlc if p["done"]), len(mem_dlc)],
        },
        "points": points,
    }
    with io.open(OUT, "w", encoding="utf-8") as f:
        json.dump(obj, f, ensure_ascii=False, separators=(",", ":"))

    print("wrote %s" % OUT)
    print("  save   : %s  playtime=%d  korokCounter=%d"
          % (path, meta["playtime"], meta["korok_counter"]))
    print("  spot   : PictureMemory_Spot_Int=%d (还剩 %d 处未造访)" % (spot_int, spot_int))
    print("  points : %d  (shrine %d, tower %d, korok %d, memory %d, beast %d)"
          % (len(points), len(n_sh_list), len(n_tw_list), len(n_ko_list),
             len(n_mem_list), len(n_beast_list)))
    print("  shrine name match: %d/%d" % (name_hits, len(shrine_objs)))
    print("  done   : shrines %d/%d, towers %d/%d, koroks %d/%d, memories %d/%d, beasts %d/%d (flags)"
          % (obj["counts"]["shrine"][0], obj["counts"]["shrine"][1],
             obj["counts"]["tower"][0], obj["counts"]["tower"][1],
             obj["counts"]["korok"][0], obj["counts"]["korok"][1],
             obj["counts"]["memory"][0], obj["counts"]["memory"][1],
             obj["counts"]["beast"][0], obj["counts"]["beast"][1]))
    print("  memory : photo %d/%d · final %d/%d · auto %d/%d · dlc %d/%d"
          % (obj["counts"]["memory_photo"][0], obj["counts"]["memory_photo"][1],
             obj["counts"]["memory_final"][0], obj["counts"]["memory_final"][1],
             obj["counts"]["memory_auto"][0], obj["counts"]["memory_auto"][1],
             obj["counts"]["memory_dlc"][0], obj["counts"]["memory_dlc"][1]))
    entered = sum(1 for p in n_sh_list if p["entered"])
    print("  entered: shrines %d/%d" % (entered, len(n_sh_list)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
