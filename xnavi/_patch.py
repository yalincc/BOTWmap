# -*- coding: utf-8 -*-
f = r'E:/WorkSpace/BOTWmap/xnavi/watch.go'
c = open(f, encoding='utf-8').read()

old = '''			// 2) 固定偏移失败，走全量扫描
			if useSaveScan && time.Since(lastScanAt) >= scanCooldown {
				lastScanAt = time.Now()
				res := locate(procPID, 2000.0, sessionBlocks(h), func(s string) { fmt.Println("    " + s) })
				if res != nil {
					setLock(res.Addr, false, res.Copies, "scan")
					inLocked = true
					resetFollow()
					lockSince = time.Now()
					probing = false
					probeGroups = nil
					fmt.Printf("  [sm] scan -> lock 0x%X copies=%d struct=%d mem=(%.1f, %.1f, %.1f) [waiting move confirm]\\n",
						res.Addr, res.Copies, res.Struct, res.Hud[0], res.Hud[1], res.Hud[2])
				} else {
					fmt.Println("  [sm] scan found nothing - retrying")
				}
				continue
			}'''

new = '''			// 2) 固定偏移失败，走全量结构扫描（不依赖存档锚点）
			if useSaveScan && time.Since(lastScanAt) >= scanCooldown {
				lastScanAt = time.Now()
				blocks := sessionBlocks(h)
				logf := func(s string) { fmt.Println("    " + s) }
				fmt.Println("  [sm] structural scan (no anchor window)...")
				hits := structuralScan(h, blocks, logf)
				if len(hits) > 0 {
					ranked, shortlist := groupAndRank(h, hits)
					for i := 0; i < len(ranked) && i < 10; i++ {
						g := ranked[i]
						logf(fmt.Sprintf("    rank %d: x%-5d struct %2d  mem=(%.1f, %.1f, %.1f)",
							i+1, g.Copies, g.Struct, g.X, g.Y, g.Z))
					}
					best := ranked[0]
					addr := best.Addrs[0]
					setLock(addr, false, best.Copies, "scan")
					inLocked = true
					resetFollow()
					lockSince = time.Now()
					probing = true
					probeGroups = shortlist
					probeMoved = make([]int, len(shortlist))
					probeBase = map[uintptr][3]float32{}
					probeStart = time.Now()
					probeLast = time.Now()
					probeLockGi = 0
					fmt.Printf("  [sm] scan -> lock 0x%X copies=%d [probing top %d, waiting move confirm]\\n",
						addr, best.Copies, len(shortlist))
				} else {
					fmt.Println("  [sm] structural scan found nothing - retrying")
				}
				continue
			}'''

if old in c:
    c = c.replace(old, new)
    open(f, 'w', encoding='utf-8').write(c)
    print('OK replaced')
else:
    print('NOT FOUND')
    # debug: find the line
    for i, line in enumerate(c.split('\n')):
        if '走全量扫描' in line:
            print(f'line {i+1}: {line.rstrip()}')
