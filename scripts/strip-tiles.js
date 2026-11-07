#!/usr/bin/env node
/* ============ 部署前裁剪：移出瓦片目录 ============
   瓦片（10096 个 webp）已外置到独立站点 tiles.yalin.site。
   EdgeOne 单项目产物有 20000 文件上限（且按项目内保留产物累计），
   主站带着瓦片部署会撞线，因此构建时把 app/assets/tiles 从产物里删掉。

   只影响 EdgeOne 构建产物：
   - 仓库里的 app/assets/tiles 保持原样（离线地图包、本地开发、xnavi 局域网镜像都要用）
   - 线上页面由 app/js/map.js 的 TILE_BASE 指向 tiles.yalin.site

   用法：EdgeOne 项目的「编译命令」填 node tools/strip-tiles.js
*/
'use strict';

const fs = require('fs');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..');
const target = path.join(repoRoot, 'app', 'assets', 'tiles');

try {
  if (!fs.existsSync(target)) {
    console.log('[strip-tiles] app/assets/tiles 不存在，跳过（产物本来就没有瓦片）');
    process.exit(0);
  }
  const before = fs.readdirSync(target).length;
  fs.rmSync(target, { recursive: true, force: true });
  console.log(`[strip-tiles] 已移除 ${before} 个瓦片文件，产物从 11220 降到约 1124`);
} catch (err) {
  console.error(`[strip-tiles] 失败：${err.message}`);
  process.exit(1);
}
