# -*- coding: utf-8 -*-
f = r'E:/WorkSpace/BOTWmap/xnavi/watch.go'
c = open(f, encoding='utf-8').read()

# 改探针切换逻辑：去掉 50m 距离限制，哪个 group 在动就切到哪个
old = '''				if bestGi >= 0 {
					adr := probeGroups[bestGi].Addrs[0]
					if d := decodeTripleAt(h, adr); d != nil &&
						dist3([3]float32{d[0], d[1], d[2]}, probeRefPos) < probeNearDist {
						setLock(adr, false, probeGroups[bestGi].Copies, "probe")
						saveKnown([]uintptr{adr}, guestRamBase(), [3]float32{})
						probing = false
						resetFollow()
						a = adr
						probeRefPos = [3]float32{d[0], d[1], d[2]}
						fmt.Printf("  [sm] probe -> switched to moving group #%d copies=%d addr=0x%X\\n",
							bestGi, probeGroups[bestGi].Copies, adr)
					}
				}'''

new = '''				if bestGi >= 0 {
					adr := probeGroups[bestGi].Addrs[0]
					if d := decodeTripleAt(h, adr); d != nil {
						setLock(adr, true, probeGroups[bestGi].Copies, "probe")
						saveKnown([]uintptr{adr}, guestRamBase(), [3]float32{})
						probing = false
						resetFollow()
						a = adr
						fmt.Printf("  [sm] probe -> switched to moving group #%d copies=%d addr=0x%X mem=(%.1f,%.1f,%.1f)\\n",
							bestGi, probeGroups[bestGi].Copies, adr, d[0], d[1], d[2])
					}
				}'''

if old in c:
    c = c.replace(old, new)
    open(f, 'w', encoding='utf-8').write(c)
    print('OK probe patched')
else:
    print('NOT FOUND')
