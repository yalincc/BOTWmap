"""核对 botw-re-notes 权威 gamedata 表 vs marcrobledo 哈希表 vs 项目数据。

数据源（均在 live/_research/ 下，仓库外）：
  botw-re-notes/game_files/gamedata_by_reset_type.yaml   44767 个 flag（name+hash+类型+save），权威源
  zelda-botw.hashes.csv                                  marcrobledo 45052 条 hash;type;label
  koroks.json                                            lud99 botw-unexplored 提取的 900 克洛格（flag+坐标+hash）
  项目 data/progress_points.json / app/data/save_flags.json

运行：F:\Python\python.exe live\_research\verify_re_notes.py
"""
import re, json, csv, os, sys

ROOT = os.path.dirname(os.path.abspath(__file__))
RES = ROOT

def load_gamedata():
    pat = re.compile(r'- \{name: (\S+), t: (\w+).*?hash: (\d+), reset_type: (\d+)\}', re.S)
    text = open(os.path.join(RES, 'botw-re-notes', 'game_files', 'gamedata_by_reset_type.yaml'),
                encoding='utf-8').read()
    return {m.group(1): int(m.group(3)) for m in pat.finditer(text)}

def load_marcrobledo():
    mc = {}
    with open(os.path.join(RES, 'zelda-botw.hashes.csv'), encoding='utf-8', newline='') as f:
        for row in csv.reader(f, delimiter=';'):
            if len(row) >= 3 and row[0].isdigit():
                mc[row[0]] = row[2]
    return mc

def main():
    g = load_gamedata()
    mc = load_marcrobledo()
    print('gamedata(yaml):', len(g), '| marcrobledo:', len(mc))
    bad = sum(1 for n, h in g.items() if str(h) in mc and mc[str(h)] != n)
    miss = sum(1 for n, h in g.items() if str(h) not in mc)
    print('yaml vs csv: mismatches', bad, '| hash not in csv:', miss)

    kor = sorted(k for k in g if k.startswith('MainField_Npc_HiddenKorok'))
    print('korok flags:', len(kor), '(Ground', len([k for k in kor if 'Ground' in k]),
          'Fly', len([k for k in kor if 'Fly' in k]), ')')

    ks = json.load(open(os.path.join(RES, 'koroks.json'), encoding='utf-8'))
    checked = sum(1 for k in ks if k['flag'] in g)
    badk = sum(1 for k in ks if k['flag'] in g and g[k['flag']] != k['hash'])
    print('koroks.json:', len(ks), '| in yaml:', checked, '| hash mismatches:', badk)

    sfp = os.path.join(ROOT, '..', '..', 'app', 'data', 'save_flags.json')
    if os.path.exists(sfp):
        sf = json.load(open(sfp, encoding='utf-8'))
        matched = sum(1 for h in sf if mc.get(h) in g)
        print('save_flags.json:', len(sf), '| all present in yaml:', matched == len(sf))

if __name__ == '__main__':
    main()
