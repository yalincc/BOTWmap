# -*- coding: utf-8 -*-
f = r'E:/WorkSpace/BOTWmap/xnavi/watch.go'
c = open(f, encoding='utf-8').read()
c = c.replace('\tvar probeRefPos [3]float32\n', '')
open(f, 'w', encoding='utf-8').write(c)
print('OK')
