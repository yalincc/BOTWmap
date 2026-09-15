# -*- coding: utf-8 -*-
"""研究: 回忆影片 / 宝箱 在真实存档中是否有对应 flag 及取值(只读,不写存档)。"""
import sys, os, zlib, struct
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
from progress import parse_save, pick_save

def h(name):
    return zlib.crc32(name.encode("utf-8"))

path, meta = pick_save()
print("save:", path)
print("meta: playtime=%d entries=%d size=%d korok_counter=%d" % (
    meta["playtime"], meta["entries"], meta["size"], meta["korok_counter"]))
entries = parse_save(path)[0]

# 1) 回忆影片
print("\n=== 回忆影片 IsGet_MemoryPhoto_000..013 ===")
got = []
for i in range(14):
    name = "IsGet_MemoryPhoto_%03d" % i
    v = entries.get(h(name))
    got.append(v)
    print("  %-26s crc=%u  value=%s" % (name, h(name), v if v is not None else "ABSENT"))
print("  已获得(值非0):", sum(1 for v in got if v))

# 2) 神兽/英杰回忆相关
print("\n=== 英杰/EX 回忆相关 flag ===")
for name in ["AoC_hero_memory_Ready", "AoC_hero_memory_Finish", "AoC_hero_memory_Activated",
             "AoC_hero_memory_Step01", "AoC_hero_memory_Step02", "PictureMemory_Finish"]:
    v = entries.get(h(name))
    print("  %-26s crc=%u  value=%s" % (name, h(name), v if v is not None else "ABSENT"))

# 3) 宝箱: CompleteTreasure_DungeonNNN(神庙全宝箱) 数量与取值
print("\n=== CompleteTreasure_DungeonNNN ===")
cnt = 0
for k, v in sorted(entries.items()):
    pass
# 直接按哈希名无法反查,需用哈希表;这里改为:从 hashes.csv 找出 CompleteTreasure_Dungeon 名单再查值
import csv
csvp = os.path.join(os.path.dirname(os.path.abspath(__file__)), "zelda-botw.hashes.csv")
ct = []
with open(csvp, "r", encoding="utf-8", errors="replace") as f:
    rd = csv.reader(f, delimiter=";")
    for row in rd:
        if len(row) >= 2 and row[1] == "1" and row[2].startswith("CompleteTreasure_Dungeon"):
            ct.append((int(row[0]), row[2]))
print("  hashes.csv 中 CompleteTreasure_Dungeon 数量:", len(ct))
dun_done = sum(1 for hv, _n in ct if entries.get(hv))
print("  存档中 value!=0 数量:", dun_done)
# 神庙全宝箱按神庙编号
dun = sorted((n for _hv, n in ct), key=lambda s: int(s.replace("CompleteTreasure_Dungeon", "")))
print("  神庙编号范围: %s ~ %s" % (dun[0], dun[-1]))

# 4) MainField_TreasureSpot 数量
print("\n=== MainField_TreasureSpot(任务宝箱点) ===")
ts = []
with open(csvp, "r", encoding="utf-8", errors="replace") as f:
    rd = csv.reader(f, delimiter=";")
    for row in rd:
        if len(row) >= 2 and row[1] == "1" and row[2].startswith("MainField_TreasureSpot"):
            ts.append((int(row[0]), row[2]))
print("  hashes.csv 中 TreasureSpot 数量:", len(ts))
print("  存档中 value!=0 数量:", sum(1 for hv, _n in ts if entries.get(hv)))
