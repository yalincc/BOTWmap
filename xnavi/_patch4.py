# -*- coding: utf-8 -*-
import re

# 修复1: gameValidTriple 加 BOTW 坐标范围（排除 (30,80,100) 这种垃圾）
f = r'E:/WorkSpace/BOTWmap/xnavi/game.go'
c = open(f, encoding='utf-8').read()
old = '''	if x <= gameMinX || x >= gameMaxX || z <= gameMinZ || z >= gameMaxZ ||
		alt <= gameMinAlt || alt >= gameMaxAlt {
		return false
	}'''
new = '''	if x <= gameMinX || x >= gameMaxX || z <= gameMinZ || z >= gameMaxZ ||
		alt <= gameMinAlt || alt >= gameMaxAlt {
		return false
	}
	// BOTW 地图坐标在千位级，排除 (30,80,100) 这种引擎内部小向量
	if abs32(x) < 200 || abs32(z) < 200 {
		return false
	}'''
if old in c:
    c = c.replace(old, new)
    open(f, 'w', encoding='utf-8').write(c)
    print('OK game.go patched')
else:
    print('game.go NOT FOUND')

# 修复2: verified 后不做站桩核对（避免误杀）
f2 = r'E:/WorkSpace/BOTWmap/xnavi/watch.go'
c2 = open(f2, encoding='utf-8').read()
old2 = '''		if time.Since(lastVerifyAt) >= verifyEvery && time.Since(lastMoveAt) >= verifyEvery {
			lastVerifyAt = time.Now()
			res := locate(procPID, 400.0, sessionBlocks(h), nil)
			if res != nil {
				vd := dist3(cur, res.Hud)
				if vd > consensusDist {
					fmt.Printf("  [sm] verify: idle scan found player %.0fm away -> rescan\\n", vd)
					inLocked = false
					goUnlocked("verify: better candidate")
					continue
				}
			}
			fmt.Printf("  [sm] verify: idle %.0fs scan clean (keep lock)\\n",
				verifyEvery.Seconds())
		}'''
new2 = '''		// verified 后不做站桩核对：玩家站着不动时全量扫描会找到其他 Actor，误杀正确锁
		// 只有读数失效才重扫（上面 invalidN 逻辑已处理）
		_ = lastVerifyAt'''
if old2 in c2:
    c2 = c2.replace(old2, new2)
    open(f2, 'w', encoding='utf-8').write(c2)
    print('OK watch.go verify disabled')
else:
    print('watch.go verify NOT FOUND')
