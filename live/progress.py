"""Read the BOTW save file (game_data.sav) and report collection progress.

Format (reverse-engineered from ZeldaMods + marcrobledo/savegame-editors +
lud99/botw-unexplored, verified against the real save on 2026-09-15):

  * 12-byte header: [version u32][0xFFFFFFFF][0x1]
  * then a flat table, 8 bytes per entry, from offset 0x0C:
        [uint32 hash][uint32 value]
    where hash = crc32(internal flag name) (standard CRC-32, zlib semantics).
  * vector3f values span three consecutive 8-byte entries:
        [hash][f0] [hash][f1] [hash][f2]   (same hash repeated)
    so the player position reads at hash_offset +4 / +12 / +20.

Completion semantics (BOTW):
  * Shrine "cleared"  = Clear_DungeonNNN != 0  (NNN from the shrine's
    Location_DungeonNNN flag name; 98 of 136 in the user's save)
  * Shrine "entered"  = Location_DungeonNNN != 0
  * Tower "activated" = MapTower_NN != 0
  * Korok "found"     = MainField_Npc_HiddenKorok(Ground|Fly)_NNN != 0
    (900 flags; the game's HiddenKorok_Number counter may exceed the flag
    count - it is reported as "counter" and NOT used for per-korok state)

usage:
    python progress.py                 # auto-pick the most-played save
    python progress.py --save PATH     # explicit file
    python progress.py --list          # list every game_data.sav found
"""
import glob
import os
import struct
import sys

SAVE_ROOT = os.path.expandvars(r"%APPDATA%\Ryujinx\bis\user\save")
PLAYTIME_HASH = 0x73C29681            # 'PlayTime' in hashes.csv
PLAYER_POSITION = 0xA40BA103          # 'PLAYER_POSITION' (marcrobledo constant)
KOROK_COUNTER = 0x8A94E07A            # 'HiddenKorok_Number' in hashes.csv


def u32(b, o):
    return struct.unpack_from("<I", b, o)[0]


def parse_save(path):
    """-> (entries: dict[hash]->int, meta)"""
    with open(path, "rb") as f:
        data = f.read()
    size = len(data)
    entries = {}
    o = 0x0C
    while o + 8 <= size:
        h = u32(data, o)
        entries[h] = u32(data, o + 4)
        o += 8
    # player position: vector3f, [id][f0] [id][f1] [id][f2]
    pos = None
    i = data.find(struct.pack("<I", PLAYER_POSITION))
    if i >= 0 and i + 24 <= size:
        f0 = struct.unpack_from("<f", data, i + 4)[0]
        f1 = struct.unpack_from("<f", data, i + 12)[0]
        f2 = struct.unpack_from("<f", data, i + 20)[0]
        pos = (f0, f1, f2)            # (X east, Y altitude, Z south+)
    meta = {
        "size": size,
        "entries": len(entries),
        "playtime": entries.get(PLAYTIME_HASH, 0),
        "korok_counter": entries.get(KOROK_COUNTER, 0),
        "pos": pos,
    }
    return entries, meta


def find_saves():
    pat = os.path.join(SAVE_ROOT, "**", "game_data.sav")
    return sorted(glob.glob(pat, recursive=True))


def pick_save(explicit=None):
    """Most-played save wins (all six slots share one mtime)."""
    if explicit:
        return explicit, None
    best, best_meta = None, None
    for p in find_saves():
        try:
            _e, meta = parse_save(p)
        except Exception:
            continue
        if best is None or meta["playtime"] > best_meta["playtime"]:
            best, best_meta = p, meta
    return best, best_meta


def main():
    argv = sys.argv[1:]
    if "--list" in argv:
        out = ["found %d game_data.sav" % len(find_saves())]
        for p in find_saves():
            try:
                _e, meta = parse_save(p)
                out.append("  pt=%-10d entries=%-6d koroks=%-4d  %s"
                           % (meta["playtime"], meta["entries"],
                              meta["korok_counter"], p))
            except Exception as ex:
                out.append("  ERR %s  %s" % (p, ex))
        print("\n".join(out))
        return 0

    explicit = None
    if "--save" in argv:
        explicit = argv[argv.index("--save") + 1]

    path, meta = pick_save(explicit)
    if not path:
        print("no game_data.sav found under %s" % SAVE_ROOT)
        return 1
    entries, meta = parse_save(path)
    print("save     : %s" % path)
    print("size     : %d bytes   playtime=%d   entries=%d"
          % (meta["size"], meta["playtime"], meta["entries"]))
    print("korok counter: %d   player pos: %s"
          % (meta["korok_counter"],
             ("(%.1f, %.1f, %.1f)" % meta["pos"]) if meta["pos"] else "-"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
