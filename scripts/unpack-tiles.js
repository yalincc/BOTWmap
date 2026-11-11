#!/usr/bin/env node
/* ============================================================
 * 瓦片反向解包工具（scripts/unpack-tiles.js）
 * ------------------------------------------------------------
 * 作用：
 *   从 6 个 .pack 文件与 tiles_idx.js 索引瞬间无损还原全部
 *   10,040 张 z4~z7 散装 .webp 瓦片。
 * 用途：
 *   供制作离线地图包（botwmap-vX.Y.Z.zip）时使用，使离线用户
 *   在 file:// 协议直接双击 index.html 时能原生加载高清瓦片。
 * 参数：
 *   - 无参：从 .pack 还原散装瓦片（默认）。
 *   - --clean：清理还原出的散装瓦片（保留 z1~z3 与 .pack）。
 * ============================================================ */
'use strict';

const fs = require('fs');
const path = require('path');

const tilesDir = path.resolve(__dirname, '..', 'app', 'assets', 'tiles');
const idxPath = path.join(tilesDir, 'tiles_idx.js');
const isClean = process.argv.includes('--clean');

if (!fs.existsSync(idxPath)) {
  console.error('[unpack-tiles] 未找到索引文件:', idxPath);
  process.exit(1);
}

if (isClean) {
  const allWebp = fs.readdirSync(tilesDir).filter(f => /^[4-7]_\d+_\d+\.webp$/.test(f));
  for (const f of allWebp) {
    fs.unlinkSync(path.join(tilesDir, f));
  }
  console.log(`[unpack-tiles] 已清理 ${allWebp.length} 张高阶散装瓦片`);
  process.exit(0);
}

const t0 = Date.now();
const idxContent = fs.readFileSync(idxPath, 'utf8');
const match = idxContent.match(/window\.BOTW_TILES_PACK\s*=\s*(\{[\s\S]*\});?/);
if (!match) {
  console.error('[unpack-tiles] 索引格式解析失败');
  process.exit(1);
}

const packData = JSON.parse(match[1]);
const packBuffers = packData.packs.map(name => {
  const p = path.join(tilesDir, name);
  return fs.readFileSync(p);
});

let count = 0;
for (const [key, entry] of Object.entries(packData.idx)) {
  const [packIdx, off, len] = entry;
  const chunk = packBuffers[packIdx].subarray(off, off + len);
  fs.writeFileSync(path.join(tilesDir, key + '.webp'), chunk);
  count++;
}

const dt = ((Date.now() - t0) / 1000).toFixed(2);
console.log(`[unpack-tiles] 成功解包还原 ${count} 张瓦片，耗时 ${dt}s`);
