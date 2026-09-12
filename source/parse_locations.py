# -*- coding: utf-8 -*-
"""解析 Location.xml，输出地名清单（用于主要区域名提取）"""
import re, json, sys
from collections import Counter

xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
items = re.findall(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)" Y="[^"]+" Z="([^"]+)" />\s*<Type>(\d+)</Type>', xml)
print('总条目:', len(items))
print('ShowLevel 分布:', dict(Counter(i[0] for i in items)))
print('Type 分布:', dict(Counter(i[4] for i in items)))
ids = [i[1] for i in items]
print('唯一 ID 数:', len(set(ids)))
print('--- 全部唯一 ID（按字母）---')
for i in sorted(set(ids)):
    print(i)
