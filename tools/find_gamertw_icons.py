# -*- coding: utf-8 -*-
"""在 gamertw chunk 中查找图标文件名与 URL"""
import re, json

src = open('source/gamertw_chunk_7872.js', encoding='utf-8').read()

png = re.findall(r'[\'"]([\w\-]+\.png)[\'"]', src)
print('png files:', sorted(set(png)))

urls = re.findall(r'https?://[^\s"\'\\]+\.png', src)
print('urls:', sorted(set(urls))[:10])

for name in ['tower.png', 'mainquest.png', 'objective.png']:
    idx = src.find(name)
    if idx > 0:
        print('\n---', name, 'ctx:', src[idx-160:idx+60])
