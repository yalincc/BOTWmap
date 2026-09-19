/* ============ 旷野之息互动地图 · 地图引擎（Canvas） ============ */
'use strict';

/* 类别视觉配置：颜色 / 形状 / 绘制优先级 / 标签显示缩放阈值 / 游戏内图标 / 固定屏幕尺寸 */
const CAT_CFG = {
  tower:      { color:'#3d8bff', shape:'tower',     order:3, lz:0.5,  icon:'icons_01.png', isz:[28,39] },
  shrine:     { color:'#2ec4b6', shape:'shrine',    order:3, lz:0.55, icon:'icons_03.png', isz:[26,28] },
  beast:      { color:'#b18cff', shape:'beast',     order:3, lz:0.5,  icon:'icons_31.png', isz:[50,30] },
  lab:        { color:'#7fd4ff', shape:'lab',       order:0, lz:0.5,  icon:'icons_05.png', isz:[31,28] },
  seed:       { color:'#9adc5c', shape:'dot',       order:4, lz:0.85, icon:'icons_04.png', isz:[30,28] },
  memory:     { color:'#ffd166', shape:'memory',    order:3, lz:0.6,  icon:'icons_28.png', isz:[30,28] },
  treasure:   { color:'#f4b942', shape:'dot',       order:4, lz:2.2,  icon:'icons_14.png', isz:[29,30] },
  stable:     { color:'#c68b5c', shape:'house',     order:0, lz:0.55, icon:'icons_10.png', isz:[30,30] },
  village:    { color:'#ff8f6b', shape:'house',     order:0, lz:0.55, icon:'icons_08.png', isz:[30,30] },
  inn:        { color:'#9ad1ff', shape:'house',     order:0, lz:0.7,  icon:'icons_07.png', isz:[30,30] },
  store:      { color:'#f7a8c4', shape:'shop',      order:0, lz:0.7,  icon:'icons_06.png', isz:[30,30] },
  armor:      { color:'#8fb8ff', shape:'shield',    order:0, lz:0.8,  icon:'icons_09.png', isz:[30,30] },
  dye:        { color:'#ff8fd0', shape:'dye',       order:0, lz:0.9,  icon:'icons_11.png', isz:[30,30] },
  jewelry:    { color:'#c9a2ff', shape:'gem',       order:0, lz:0.9,  icon:'icons_33.png', isz:[30,30] },
  settlement: { color:'#9aa3ad', shape:'fort',      order:0, lz:0.6,  icon:'icons_34.png', isz:[30,30] },
  fountain:   { color:'#ff9ad5', shape:'fountain',  order:0, lz:0.6,  icon:'icons_19.png', isz:[29,30] },
  statue:     { color:'#b8e0ff', shape:'statue',    order:0, lz:0.8,  icon:'icons_22.png', isz:[25,30] },
  pot:        { color:'#7d6a5a', shape:'dot',       order:0, lz:0.9,  icon:'icons_16.png', isz:[29,30] },
  raft:       { color:'#a98f6e', shape:'raft',      order:0, lz:0.9,  icon:'icons_26.png', isz:[27,30] },
  talus:      { color:'#8d99a6', shape:'talus',     order:2, lz:0.8,  icon:'icons_20.png', isz:[26,30] },
  hinox:      { color:'#7ac97a', shape:'hinox',     order:2, lz:0.8,  icon:'icons_12.png', isz:[29,30] },
  lynel:      { color:'#ff6b5e', shape:'lynel',     order:2, lz:0.7,  icon:'icons_24.png', isz:[26,30] },
  molduga:    { color:'#f2d45c', shape:'molduga',   order:2, lz:0.8,  icon:'icons_17.png', isz:[29,30] },
  guardian:   { color:'#e05e5e', shape:'guardian',  order:2, lz:0.8,  icon:'icons_35.png', isz:[32,26] },
  mainquest:  { color:'#ffd166', shape:'star',      order:1, lz:0.6,  icon:'icons_31.png', isz:[50,30] },
  shrinequest:{ color:'#ffb347', shape:'qmark',     order:1, lz:0.6,  icon:'icons_30.png', isz:[30,30] },
  sidequest:  { color:'#6fc7ff', shape:'star',      order:1, lz:0.7,  icon:'icons_05.png', isz:[28,39] },
  objective:  { color:'#ffe066', shape:'excl',      order:1, lz:0.7,  icon:'icons_32.png', isz:[20,20] },
};
const CAT_GROUP = {
  '探索收集': ['shrine','tower','beast','seed','memory','treasure'],
  '设施地点': ['village','stable','inn','store','armor','dye','jewelry','lab','fountain','statue','pot','raft','settlement'],
  '敌人':     ['lynel','hinox','talus','molduga','guardian'],
  '任务':     ['mainquest','shrinequest','sidequest','objective'],
};

/* 材料追踪层（v1.1.0）：6 类采集材料的类目视觉配置 */
const MAT_CAT_CFG = {
  '植物': { color: '#7fd45f' },
  '蘑菇': { color: '#c98a4a' },
  '水果': { color: '#ff6b6b' },
  '昆虫': { color: '#f2d45c' },
  '鱼':   { color: '#4fc3f7' },
  '矿物': { color: '#b0a8e8' },
};

class BotwMap {
  constructor(canvas, data) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d');
    this.data = data;
    this.MW = data.meta.mapW;   // 6000
    this.MH = data.meta.mapH;   // 5000

    this.enabled = new Set();   // 启用的类别 key
    this.doneSet = new Set();   // 已完成标记 id
    this.customMarkers = [];    // 自定义标记 {id,name,color,px,note}
    this.measurePoints = [];    // 测量点 (map px)
    this.mode = 'browse';       // browse | measure | pin
    this.hover = null;          // hover 标记
    this.selected = null;       // 选中标记

    this.view = { x: 0, y: 0, scale: 0.3 };
    this.dpr = window.devicePixelRatio || 1;
    this.loaded = false;
    this._viewApplied = false; // 是否已应用外部（分享链接）视图
    this.regions = (typeof BOTW_REGIONS !== 'undefined') ? BOTW_REGIONS : []; // 主要地形/区域名
    this._regionScale = 0.05;  // 全图适配 scale（fit 时更新），用于地名分级切换
    // 地名标签样式（可由用户在“设置”面板调整，默认：米色半透明底 + 深褐色字，仿游戏内地图）
    this.regionStyle = { bg: [242, 232, 213], bgA: 0.6, tx: [92, 58, 30], txA: 0.92, fsz: 1, sc: [0, 0, 0], sw: 0 };
    this.showRegions = true;   // 地名显示开关（设置弹层可切换，持久化到 localStorage）
    this.tileCache = new Map();   // 瓦片 LRU 缓存：key -> HTMLImageElement
    this.tilePending = new Set(); // 正在加载的瓦片 key
    this._tileDrawPending = false;

    this._buildIndex();
    this._loadIcons();
    // 材料追踪层（v1.1.0）：数据源 BOTW_MATERIALS（materials.js）
    this.MATS = (typeof BOTW_MATERIALS !== 'undefined') ? BOTW_MATERIALS : null;
    this.matIds = new Set();       // 启用的材料 id（单材料勾选，b 方案）
    this.matSel = null;           // 选中的材料 id（高亮其全部点）
    this.matIcons = {};           // entry -> HTMLImageElement
    this.matByEntry = {};         // entry -> material
    this.matPointsBy = [];        // [id] -> [[px,py],...]
    this.matClusters = {};        // [id] -> Supercluster 实例（懒加载缓存）
    if (this.MATS) { this._buildMatIndex(); this._loadMatIcons(); }
    this._bindEvents();
    // 等两帧确保 CSS 布局完成；再监听 load 事件兜底（rAF 时 flex 布局可能尚未算高）
    requestAnimationFrame(() => requestAnimationFrame(() => this._resize()));
    window.addEventListener('load', () => this._resize(), { once: true });
  }

  /* ---------- 空间索引：256px 网格 ---------- */
  _buildIndex() {
    const CELL = 256;
    const cols = Math.ceil(this.MW / CELL), rows = Math.ceil(this.MH / CELL);
    this.grid = new Array(cols * rows).fill(null);
    this.byId = new Map();
    this.markers = this.data.markers;
    for (let i = 0; i < this.markers.length; i++) {
      const mk = this.markers[i];
      this.byId.set(mk.id, i);
      const cx = Math.floor(mk.px[0] / CELL), cy = Math.floor(mk.px[1] / CELL);
      const gi = cy * cols + cx;
      if (gi >= 0 && gi < this.grid.length) {
        (this.grid[gi] = this.grid[gi] || []).push(i);
      }
    }
  }

  /* 预加载游戏内图标（ali213） */
  _loadIcons() {
    this.icons = {};
    const seen = new Set();
    for (const key in CAT_CFG) {
      const ic = CAT_CFG[key].icon;
      if (!ic || seen.has(ic)) continue;
      seen.add(ic);
      const img = new Image();
      img.onload = () => this.draw();
      img.src = 'assets/icons/' + ic;
      this.icons[ic] = img;
    }
  }

  /* ---------- 瓦片（ali213 同款动态加载） ---------- */
  // 逻辑坐标(24000×20000) ↔ zoom7 瓦片像素偏移：tpx = px + (4384, 6384)
  // zoom z 的分辨率 = zoom7 的 2^(z-7)；scale=1 时为 1:1（对应 zoom7）
  _zoomForScale(scale) {
    const z = Math.round(Math.log2(scale)) + 7;
    return Math.min(7, Math.max(1, z));
  }

  _drawTiles(ctx) {
    const { w, h, view } = this;
    const z = this._zoomForScale(view.scale);
    const k = Math.pow(2, 7 - z);   // zoom7 像素 → 当前 zoom 像素的缩放因子
    const pad = 64;
    const x0 = Math.max(0, (-pad - view.x) / view.scale);
    const y0 = Math.max(0, (-pad - view.y) / view.scale);
    const x1 = Math.min(this.MW, (w + pad - view.x) / view.scale);
    const y1 = Math.min(this.MH, (h + pad - view.y) / view.scale);
    if (x0 >= x1 || y0 >= y1) return;
    const tx0 = Math.floor(((x0 + 4384) / k) / 256);
    const ty0 = Math.floor(((y0 + 6384) / k) / 256);
    const tx1 = Math.floor(((x1 + 4384) / k) / 256);
    const ty1 = Math.floor(((y1 + 6384) / k) / 256);
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = 'high';
    let requested = 0;
    const drawnFb = new Set();   // 本帧已绘制的降级瓦片（同一低阶瓦片只画一次，避免 2×2 重复绘制）
    for (let ty = ty0; ty <= ty1; ty++) {
      for (let tx = tx0; tx <= tx1; tx++) {
        const key = z + '_' + tx + '_' + ty;
        const img = this.tileCache.get(key);
        if (img && img.complete && img.naturalWidth > 0) {
          const lx = tx * 256 * k - 4384;              // 瓦片逻辑左上角
          const ly = ty * 256 * k - 6384;
          const sw = 256 * k * view.scale;             // 瓦片屏幕边长
          ctx.drawImage(img, view.x + lx * view.scale, view.y + ly * view.scale, sw + 0.6, sw + 0.6);
        } else {
          // 降级兜底：目标瓦片未就绪 → 用已缓存的低级别瓦片放大铺底，画面不空白
          const fb = this._findCachedFallback(z, tx, ty);
          if (fb) {
            const fk = fb.lz + '_' + fb.a + '_' + fb.b;
            if (!drawnFb.has(fk)) {
              drawnFb.add(fk);
              const tileLog = 256 * k * Math.pow(2, fb.d);   // 低阶瓦片覆盖的逻辑边长
              const lx = fb.a * tileLog - 4384;
              const ly = fb.b * tileLog - 6384;
              const sw = tileLog * view.scale;
              ctx.drawImage(fb.img, view.x + lx * view.scale, view.y + ly * view.scale, sw + 0.6, sw + 0.6);
            }
          }
          // 无论是否兜底，都继续请求目标瓦片（到达后自动替换为清晰图）
          if (requested < 48 && !this.tilePending.has(key)) {
            requested++;
            this._loadTile(z, key);
          }
        }
      }
    }
  }

  /* 查找已缓存的低级别瓦片：zoom z 的 (tx,ty) 缺失时，逐级向上找已缓存的低阶瓦片 */
  _findCachedFallback(z, tx, ty, maxLevels = 4) {
    for (let d = 1; d <= maxLevels; d++) {
      const lz = z - d;
      if (lz < 1) break;
      const c = Math.pow(2, d);
      const a = Math.floor(tx / c), b = Math.floor(ty / c);
      const img = this.tileCache.get(lz + '_' + a + '_' + b);
      if (img && img.complete && img.naturalWidth > 0) return { img, lz, a, b, d };
    }
    return null;
  }

  _loadTile(z, key) {
    if (this.tileCache.has(key) || this.tilePending.has(key)) return;
    this.tilePending.add(key);
    const img = new Image();
    img.onload = () => {
      this.tilePending.delete(key);
      if (this.tileCache.size >= 400) {
        const first = this.tileCache.keys().next().value;
        this.tileCache.delete(first);
      }
      this.tileCache.set(key, img);
      // 合并重绘（瓦片批量到达时避免频繁重绘）
      if (!this._tileDrawPending) {
        this._tileDrawPending = true;
        requestAnimationFrame(() => { this._tileDrawPending = false; this.draw(); });
      }
    };
    img.onerror = () => this.tilePending.delete(key);
    img.src = 'assets/tiles/' + key + '.webp';
  }

  /* ---------- 视口 ---------- */
  _resize() {
    const r = this.canvas.getBoundingClientRect();
    this.w = r.width; this.h = r.height;
    this.canvas.width = Math.round(r.width * this.dpr);
    this.canvas.height = Math.round(r.height * this.dpr);
    this.ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    this.fit();
    this.draw();
  }

  fit() {
    if (!this.w) return;
    const s = Math.min(this.w / this.MW, this.h / this.MH);
    this.minScale = s * 0.85;
    this.maxScale = 1; // 统一最大缩放：手机与电脑都能放到最高清瓦片(z7)，缩放级数一致
    this._regionScale = s;   // 全图适配 scale（地名分级切换基准）
    this.view.scale = Math.min(Math.max(s, this.minScale), this.maxScale);
    this.view.x = (this.w - this.MW * this.view.scale) / 2;
    this.view.y = (this.h - this.MH * this.view.scale) / 2;
  }

  /* 地图坐标 -> 屏幕坐标 */
  m2s(px) { return [this.view.x + px[0] * this.view.scale, this.view.y + px[1] * this.view.scale]; }
  s2m(sx, sy) { return [(sx - this.view.x) / this.view.scale, (sy - this.view.y) / this.view.scale]; }
  gameCoord(px) {
    // 24000×20000 高清底图：gameX = (pxX - 12000) / 2, gameZ = (pxY - 10000) / 2
    const gx = Math.round((px[0] - 12000) / 2);
    const gz = Math.round((px[1] - 10000) / 2);
    return [gx, gz];
  }

  setView(x, y, scale, animate) {
    scale = Math.min(Math.max(scale, this.minScale), this.maxScale);
    this.view.x = x; this.view.y = y; this.view.scale = scale;
    this.draw();
  }

  zoomAt(sx, sy, factor) {
    const ns = Math.min(Math.max(this.view.scale * factor, this.minScale), this.maxScale);
    const k = ns / this.view.scale;
    this.view.x = sx - (sx - this.view.x) * k;
    this.view.y = sy - (sy - this.view.y) * k;
    this.view.scale = ns;
    this.draw();
  }

  /* ---------- 视口内标记收集 ---------- */
  _visibleMarkers(pad = 40) {
    const CELL = 256, cols = Math.ceil(this.MW / CELL);
    const x0 = Math.max(0, (-pad - this.view.x) / this.view.scale);
    const y0 = Math.max(0, (-pad - this.view.y) / this.view.scale);
    const x1 = Math.min(this.MW, (this.w + pad - this.view.x) / this.view.scale);
    const y1 = Math.min(this.MH, (this.h + pad - this.view.y) / this.view.scale);
    const c0x = Math.floor(x0 / CELL), c0y = Math.floor(y0 / CELL);
    const c1x = Math.min(Math.floor(x1 / CELL), cols - 1), c1y = Math.min(Math.floor(y1 / CELL), Math.ceil(this.MH / CELL) - 1);
    const out = [];
    for (let cy = c0y; cy <= c1y; cy++) {
      for (let cx = c0x; cx <= c1x; cx++) {
        const cell = this.grid[cy * cols + cx];
        if (cell) for (const idx of cell) {
          const mk = this.markers[idx];
        if (!this.enabled.has(mk.cat)) continue;
        // 回忆：地图上只标注 13 个照片回忆点（照片12 + 最终1）；主线自动 / DLC 回忆随任务触发，不标注
        if (mk.cat === 'memory' && !(mk.extra && (mk.extra.tier === 'photo' || mk.extra.tier === 'final'))) continue;
        out.push(mk);
        }
      }
    }
    return out;
  }

  /* ---------- 绘制 ---------- */
  draw() {
    const ctx = this.ctx;
    const { w, h, view } = this;
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#10150f';
    ctx.fillRect(0, 0, w, h);

    this._drawTiles(ctx);

    this._drawRegions(ctx);   // 地形/区域名（瓦片之上、标记之下，作为背景标注）

    this._drawMeasure(ctx);
    this._drawMaterials(ctx); // 材料追踪层（下层：不遮挡神庙/塔等主标记）
    this._drawMarkers(ctx);
    this._drawMatSel(ctx);    // 材料选中高亮（最上层）
    this._drawCustom(ctx);
    // live 层：玩家位置 / 轨迹 / 导航线（live.js 注入；live.js 加载前的早期 draw 可能未挂载，防御）
    if (typeof this._drawLive === 'function') this._drawLive(ctx);
  }

  /* ---------- 地形/区域名（模仿游戏内地图：半透明底深字；字号随缩放放大，随缩放分级切换；高层出现后低层弱化保留） ---------- */
  _drawRegions(ctx) {
    if (!this.regions.length || !this.showRegions) return;
    const { w, h, view } = this;
    const f = this._regionScale || 0.02;      // 全图适配 scale
    const z = this._zoomForScale(view.scale); // 瓦片 zoom，用于清晰度兜底
    const g1 = f * 2, g2 = f * 4, g3 = f * 8; // 分级阈值（整数倍）：地形区(2x)→细地名(4x)→更细地名(8x)
    const showR2 = view.scale >= g1;
    const showR3 = view.scale >= g2;                 // r3：4x 起显（弱化保留）
    const showR4 = view.scale >= g3 && z >= 6;       // r4：8x 且瓦片至少 z6（确保最细地名在清晰瓦片上）
    const showR5 = view.scale >= this.maxScale * 0.95 && z >= 7; // r5（城堡内部房间）：缩放到最大且最高清瓦片才显示
    const st = this.regionStyle || { bg: [242, 232, 213], bgA: 0.6, tx: [92, 58, 30], txA: 0.92, fsz: 1, sc: [0, 0, 0], sw: 0 };
    const fsz = st.fsz || 1;
    const kMax = { r1: 1.5, r2: 1.7, r3: 1.7, r4: 2.0, r5: 2.2 }; // 各层字号上限：低层封顶弱化，r4/r5 细节可继续放大
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    for (const rg of this.regions) {
      const lv = rg.lv;
      if (lv === 'r1' && showR2) continue;                          // 大区：全图~放大2x
      if (lv === 'r2' && (!showR2 || showR3)) continue;             // 地形区：2x~4x，r3 出现后隐藏
      if (lv === 'r3' && !showR3) continue;                         // 细地名：4x 起显，之后弱化保留
      if (lv === 'r4' && !showR4) continue;                         // 更细地名：8x 之后
      if (lv === 'r5' && !showR5) continue;                         // 城堡内部房间：最大缩放才显示
      const [sx, sy] = this.m2s(rg.px);
      if (sx < -140 || sy < -140 || sx > w + 140 || sy > h + 140) continue;
      const base = lv === 'r1' ? 13 : lv === 'r2' ? 11 : lv === 'r3' ? 9.5 : lv === 'r4' ? 8 : 7;  // 层级字号：r1 大区 > r2 地形区 > r3 细地名 > r4 更细 > r5 房间
      const k = Math.min(view.scale / f, kMax[lv] || 2.2);
      const fs = base * k * fsz;
      // 弱化保留：r3 在 r4 出现后、r4 在 r5 出现后透明度降低，聚焦更细层级
      let alpha = 1;
      if (lv === 'r3' && showR4) alpha = 0.8;
      if (lv === 'r4' && showR5) alpha = 0.75;
      ctx.font = (lv === 'r1' ? '700 ' : '600 ') + fs + 'px "PingFang SC","Microsoft YaHei",sans-serif';
      const tw = ctx.measureText(rg.n).width;
      const padX = fs * 0.55, padY = fs * 0.28;
      const bw = tw + padX * 2, bh = fs + padY * 2;
      const x = sx - bw / 2, y = sy - bh / 2;
      if (st.bgA > 0) {
        ctx.fillStyle = `rgba(${st.bg[0]},${st.bg[1]},${st.bg[2]},${st.bgA * alpha})`;
        this._roundRect(ctx, x, y, bw, bh, 4);
        ctx.fill();
      }
      const sw = st.sw || 0;
      if (sw > 0 && st.sc) {
        ctx.strokeStyle = `rgba(${st.sc[0]},${st.sc[1]},${st.sc[2]},${st.txA * alpha})`;
        ctx.lineWidth = sw;
        ctx.lineJoin = 'round';
        ctx.strokeText(rg.n, sx, sy + 0.5);
      }
      ctx.fillStyle = `rgba(${st.tx[0]},${st.tx[1]},${st.tx[2]},${st.txA * alpha})`;
      ctx.fillText(rg.n, sx, sy + 0.5);
    }
  }

  _roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  _markerScreenSize(mk) {
    const cfg = CAT_CFG[mk.cat] || CAT_CFG.sidequest;
    // 游戏内图标：固定屏幕尺寸（不随地图缩放变化）
    if (cfg.isz) return Math.max(cfg.isz[0], cfg.isz[1]);
    // fallback（未配置图标时）：随缩放的矢量尺寸
    const s = cfg.base * this.view.scale;
    const isDot = cfg.shape === 'dot';
    const min = isDot ? 2 : (mk.cat === 'beast' ? 12 : 7);
    const max = mk.cat === 'beast' ? 34 : (isDot ? 9 : 22);
    return Math.min(Math.max(s, min), max);
  }

  /* 是否已载入存档进度（live 服务 /progress 或手动上传存档） */
  saveLoaded() {
    return !!((this.live && this.live.counts)) || !!window.__saveUploaded;
  }

  /* 是否已完成：本地标记完成 或 存档进度（live.js 注入的 liveDone） */
  _mkDone(mk) {
    // 回忆类别与统计面板同口径：已载入存档进度时以存档为准（忽略本地手动标记），
    // 避免手动标记的回忆点与面板统计/地图图标不一致（照片/主线/DLC 逐点 flag）
    if (mk.cat === "memory" && this.saveLoaded()) {
      return !!(this.liveDone && this.liveDone.has(mk.id));
    }
    return this.doneSet.has(mk.id) ||
           (!!this.liveDone && this.liveDone.has(mk.id));
  }

  _drawMarkers(ctx) {
    const vis = this._visibleMarkers();
    // 按绘制优先级排序，保证重要标记画在最上层
    vis.sort((a, b) => (CAT_CFG[a.cat]?.order || 0) - (CAT_CFG[b.cat]?.order || 0));

    const labelList = [];
    const scale = this.view.scale;
    let drawn = 0;
    for (const mk of vis) {
      const cfg = CAT_CFG[mk.cat] || CAT_CFG.sidequest;
      const [sx, sy] = this.m2s(mk.px);
      if (sx < -60 || sy < -60 || sx > this.w + 60 || sy > this.h + 60) continue;
      const size = this._markerScreenSize(mk);
      const done = this._mkDone(mk);
      this._glyph(ctx, mk, sx, sy, size, done);
      drawn++;
      // 标签
      if (scale >= cfg.lz && mk.cat !== 'treasure') {
        labelList.push({ mk, sx, sy, size });
      }
    }
    this.lastStats = { drawn, labels: labelList.length, visible: vis.length };
    // 标签绘制
    for (const { mk, sx, sy, size } of labelList) {
      const done = this._mkDone(mk);
      const label = mk.cat === 'seed' ? 'No.' + (mk.extra?.korok_no || mk.id.replace(/\D/g, '')) : mk.name;
      if (!label) continue;
      const fs = Math.min(Math.max(9, 11 * scale), 15);
      ctx.font = `600 ${fs}px "PingFang SC","Microsoft YaHei",sans-serif`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'bottom';
      const ly = sy - size / 2 - 3;
      ctx.lineWidth = 3;
      ctx.strokeStyle = 'rgba(8,12,8,.85)';
      ctx.strokeText(label, sx, ly);
      ctx.fillStyle = done ? 'rgba(180,195,180,.75)' : (mk.cat === 'shrine' ? '#eafffb' : '#ffffff');
      ctx.fillText(label, sx, ly);
    }

    // 克洛格挑战起点第二标记（v1.1.1）：类型色菱形 + 与收获点虚线连线
    this._drawKorokStarts(ctx, vis);

    // 悬停高亮
    if (this.hover) {
      const [hsx, hsy] = this.m2s(this.hover.px);
      ctx.beginPath();
      ctx.arc(hsx, hsy, this._markerScreenSize(this.hover) / 2 + 5, 0, Math.PI * 2);
      ctx.strokeStyle = 'rgba(255,255,255,.9)';
      ctx.lineWidth = 2;
      ctx.stroke();
    }
  }

  /* ---------- 克洛格挑战起点第二标记（v1.1.1） ---------- */
  // 懒建索引：BOTW_KOROKS 里有 has_start_diff 的条目 -> {start, mk, color, no}
  _buildKorokStarts() {
    if (this._korokStarts) return;
    this._korokStarts = new Map();
    if (typeof BOTW_KOROKS === 'undefined' || !BOTW_KOROKS.items) return;
    const colors = window.KOROK_TYPE_COLORS || {};
    const byId = new Map();
    for (const mk of this.markers) if (mk.cat === 'seed') byId.set(mk.id, mk);
    for (const k of BOTW_KOROKS.items) {
      if (!k.has_start_diff || !k.start) continue;
      const mk = byId.get(k.id);
      if (!mk) continue;
      this._korokStarts.set(k.id, {
        start: k.start, mk, no: k.no, type_cn: k.type_cn,
        color: colors[k.type] || '#9aa5a0',
      });
    }
  }

  _drawKorokStarts(ctx, vis) {
    const scale = this.view.scale;
    if (scale < 0.2) return;              // 全局小视图不画，避免 168 条连线盖住地图；放大到找克洛格的尺度才显示
    this._buildKorokStarts();
    if (!this._korokStarts || !this._korokStarts.size) return;
    const { w, h } = this;
    ctx.save();
    for (const mk of vis) {
      if (mk.cat !== 'seed') continue;
      const ks = this._korokStarts.get(mk.id);
      if (!ks) continue;
      const [kx, ky] = this.m2s(ks.start);
      if (kx < -40 || ky < -40 || kx > w + 40 || ky > h + 40) continue;
      const [sx, sy] = this.m2s(mk.px);
      // 收获点 → 起点 虚线连线（类型色，低透明）
      ctx.globalAlpha = 0.4;
      ctx.strokeStyle = ks.color;
      ctx.lineWidth = 1.3;
      ctx.setLineDash([4, 4]);
      ctx.beginPath(); ctx.moveTo(sx, sy); ctx.lineTo(kx, ky); ctx.stroke();
      ctx.setLineDash([]);
      // 起点：类型色菱形 + 深色描边（随缩放 3~8px）
      const r = Math.min(Math.max(3 * scale * 20, 2.6), 8);
      ctx.globalAlpha = 0.95;
      ctx.beginPath();
      ctx.moveTo(kx, ky - r); ctx.lineTo(kx + r * 0.75, ky); ctx.lineTo(kx, ky + r); ctx.lineTo(kx - r * 0.75, ky);
      ctx.closePath();
      ctx.fillStyle = ks.color;
      ctx.fill();
      ctx.strokeStyle = 'rgba(8,12,8,.92)';
      ctx.lineWidth = 1.2;
      ctx.stroke();
      // 深缩放时标注"起点"
      if (scale >= 0.8) {
        ctx.globalAlpha = 1;
        ctx.font = '600 10px "PingFang SC","Microsoft YaHei",sans-serif';
        ctx.textAlign = 'center'; ctx.textBaseline = 'bottom';
        ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(8,12,8,.85)';
        ctx.strokeText('起点', kx, ky - r - 2);
        ctx.fillStyle = ks.color;
        ctx.fillText('起点', kx, ky - r - 2);
      }
    }
    ctx.restore();
  }

  /* 点击起点第二标记 → 选中对应克洛格（同 hitTest 容差） */
  _hitKorokStart(mx, my) {
    if (this.view.scale < 0.2) return null;   // 与绘制阈值同步：小视图不画也不响应
    if (!this.enabled.has('seed')) return null;
    this._buildKorokStarts();
    if (!this._korokStarts || !this._korokStarts.size) return null;
    const tol = 12 / this.view.scale;
    let best = null, bd = 1e18;
    for (const ks of this._korokStarts.values()) {
      const dx = ks.start[0] - mx, dy = ks.start[1] - my;
      if (Math.abs(dx) <= tol && Math.abs(dy) <= tol) {
        const d = dx * dx + dy * dy;
        if (d < bd) { bd = d; best = ks.mk; }
      }
    }
    return best;
  }

  /* 标记图形：优先绘制游戏内图标，缺失时回退矢量 */
  _glyph(ctx, mk, x, y, size, done) {
    const cfg = CAT_CFG[mk.cat] || CAT_CFG.sidequest;
    const isFinalMem = mk.cat === 'memory' && mk.extra && mk.extra.tier === 'final';
    const img = (cfg.icon && !isFinalMem) ? this.icons[cfg.icon] : null;
    ctx.save();
    if (done) ctx.globalAlpha = .45;
    if (img && img.complete && img.naturalWidth > 0) {
      const iw = cfg.isz ? cfg.isz[0] : size;
      const ih = cfg.isz ? cfg.isz[1] : size;
      ctx.drawImage(img, x - iw / 2, y - ih / 2, iw, ih);
      ctx.restore();
      if (done) {
        // 完成标记：绿色小对勾
        ctx.strokeStyle = '#7dffa0';
        ctx.lineWidth = Math.max(1.5, size * .16);
        ctx.beginPath();
        ctx.moveTo(x - size * .22, y); ctx.lineTo(x - size * .05, y + size * .18); ctx.lineTo(x + size * .26, y - size * .16);
        ctx.stroke();
      }
      return;
    }
    const color = cfg.color, shape = cfg.shape;
    const r = size / 2;
    if (done) {
      ctx.fillStyle = '#7a8a7a';
      ctx.strokeStyle = '#9fb09f';
    } else {
      ctx.fillStyle = color;
      ctx.strokeStyle = '#fff';
    }
    ctx.lineWidth = Math.max(1, size * .12);
    ctx.beginPath();
    switch (shape) {
      case 'shrine': { // 菱形
        ctx.moveTo(x, y - r); ctx.lineTo(x + r * .85, y); ctx.lineTo(x, y + r); ctx.lineTo(x - r * .85, y);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        if (!done) { // 内部小菱形
          ctx.fillStyle = '#ffffff';
          ctx.beginPath(); ctx.moveTo(x, y - r * .42); ctx.lineTo(x + r * .36, y); ctx.lineTo(x, y + r * .42); ctx.lineTo(x - r * .36, y);
          ctx.closePath(); ctx.fill();
        }
        break;
      }
      case 'tower': { // 塔：三角顶 + 方身
        ctx.moveTo(x, y - r); ctx.lineTo(x + r * .62, y - r * .15); ctx.lineTo(x + r * .38, y + r); ctx.lineTo(x - r * .38, y + r); ctx.lineTo(x - r * .62, y - r * .15);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'beast': { // 神兽：圆 + 外环
        ctx.arc(x, y, r * .72, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y, r * .34, 0, Math.PI * 2);
        ctx.fillStyle = done ? '#7a8a7a' : '#ffffff'; ctx.fill();
        break;
      }
      case 'lab': { // 研究所：烧瓶
        ctx.moveTo(x - r * .45, y - r); ctx.lineTo(x + r * .45, y - r); ctx.lineTo(x + r * .15, y - r * .1);
        ctx.lineTo(x + r * .15, y + r * .55); ctx.lineTo(x + r * .55, y + r * .95); ctx.lineTo(x - r * .55, y + r * .95); ctx.lineTo(x - r * .15, y + r * .55); ctx.lineTo(x - r * .15, y - r * .1);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'memory': { // 回忆：圆内小方（照片）；最终回忆 #17：金色五角星
        if (isFinalMem) { // 金色五角星（外 r，内 r*.42）
          ctx.beginPath();
          for (let k = 0; k < 10; k++) {
            const ang = -Math.PI / 2 + k * Math.PI / 5;
            const rad = k % 2 === 0 ? r : r * .42;
            const px2 = x + Math.cos(ang) * rad, py2 = y + Math.sin(ang) * rad;
            if (k === 0) ctx.moveTo(px2, py2); else ctx.lineTo(px2, py2);
          }
          ctx.closePath(); ctx.fill(); ctx.stroke();
          break;
        }
        ctx.arc(x, y, r * .8, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#26302a';
        ctx.fillRect(x - r * .3, y - r * .3, r * .6, r * .6);
        ctx.strokeRect(x - r * .3, y - r * .3, r * .6, r * .6);
        break;
      }
      case 'house': {
        ctx.moveTo(x, y - r); ctx.lineTo(x + r, y - r * .1); ctx.lineTo(x + r, y + r); ctx.lineTo(x - r, y + r); ctx.lineTo(x - r, y - r * .1);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#fff';
        ctx.fillRect(x - r * .3, y - r * .2, r * .6, r * .55);
        break;
      }
      case 'shop': {
        ctx.rect(x - r * .85, y - r * .6, r * 1.7, r * 1.6); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#fff';
        ctx.fillRect(x - r * .35, y - r * .45, r * .7, r * 1.3);
        break;
      }
      case 'shield': {
        ctx.moveTo(x - r * .8, y - r); ctx.lineTo(x + r * .8, y - r); ctx.lineTo(x + r * .55, y + r * .5); ctx.lineTo(x, y + r); ctx.lineTo(x - r * .55, y + r * .5);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'dye': {
        ctx.arc(x, y, r * .72, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y, r * .3, 0, Math.PI * 2); ctx.fillStyle = done ? '#7a8a7a' : '#fff'; ctx.fill();
        break;
      }
      case 'gem': {
        ctx.moveTo(x, y - r); ctx.lineTo(x + r * .9, y - r * .25); ctx.lineTo(x + r * .5, y + r); ctx.lineTo(x - r * .5, y + r); ctx.lineTo(x - r * .9, y - r * .25);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'fort': {
        ctx.rect(x - r * .85, y - r * .55, r * 1.7, r * 1.55); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#fff';
        ctx.fillRect(x - r * .5, y - r * .35, r * .3, r * .5);
        ctx.fillRect(x + r * .2, y - r * .35, r * .3, r * .5);
        break;
      }
      case 'fountain': {
        ctx.arc(x, y - r * .3, r * .55, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y - r * .3, r * .22, 0, Math.PI * 2); ctx.fillStyle = done ? '#7a8a7a' : '#fff'; ctx.fill();
        ctx.strokeRect(x - r * .3, y + r * .25, r * .6, r * .45);
        break;
      }
      case 'statue': {
        ctx.arc(x, y - r * .35, r * .35, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.fillRect(x - r * .3, y - r * .05, r * .6, r * .75);
        ctx.strokeRect(x - r * .3, y - r * .05, r * .6, r * .75);
        break;
      }
      case 'raft': {
        ctx.fillRect(x - r, y - r * .35, r * 2, r * .5);
        ctx.strokeRect(x - r, y - r * .35, r * 2, r * .5);
        ctx.fillRect(x - r * .1, y - r, r * .2, r * .8);
        break;
      }
      case 'talus': {
        ctx.arc(x, y, r * .8, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y, r * .3, 0, Math.PI * 2); ctx.fillStyle = done ? '#7a8a7a' : '#fff'; ctx.fill();
        break;
      }
      case 'hinox': {
        ctx.beginPath(); ctx.arc(x, y, r * .75, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y, r * .28, 0, Math.PI * 2); ctx.fillStyle = done ? '#7a8a7a' : '#fff'; ctx.fill();
        break;
      }
      case 'lynel': {
        ctx.moveTo(x, y - r); ctx.lineTo(x + r, y - r * .4); ctx.lineTo(x + r * .6, y + r); ctx.lineTo(x - r * .6, y + r); ctx.lineTo(x - r, y - r * .4);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#fff';
        ctx.fillRect(x - r * .25, y - r * .5, r * .5, r * .4);
        break;
      }
      case 'molduga': {
        ctx.moveTo(x - r, y); ctx.quadraticCurveTo(x - r * .3, y - r, x, y); ctx.quadraticCurveTo(x + r * .3, y + r, x + r, y);
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'guardian': {
        ctx.arc(x, y, r * .62, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.strokeStyle = done ? '#9fb09f' : '#fff';
        ctx.lineWidth = Math.max(1, size * .09);
        for (let i = 0; i < 4; i++) {
          const a = i * Math.PI / 2;
          ctx.beginPath(); ctx.moveTo(x + Math.cos(a) * r * .62, y + Math.sin(a) * r * .62);
          ctx.lineTo(x + Math.cos(a) * r * .95, y + Math.sin(a) * r * .95); ctx.stroke();
        }
        ctx.beginPath(); ctx.arc(x, y, r * .22, 0, Math.PI * 2); ctx.fillStyle = done ? '#7a8a7a' : '#fff'; ctx.fill();
        break;
      }
      case 'star': {
        ctx.moveTo(x, y - r);
        for (let i = 0; i < 10; i++) {
          const ang = -Math.PI / 2 + i * Math.PI / 5;
          const rr = i % 2 === 0 ? r : r * .45;
          ctx.lineTo(x + Math.cos(ang) * rr, y + Math.sin(ang) * rr);
        }
        ctx.closePath(); ctx.fill(); ctx.stroke();
        break;
      }
      case 'qmark': {
        ctx.arc(x, y - r * .2, r * .72, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.fillStyle = done ? '#7a8a7a' : '#26302a';
        ctx.font = `800 ${size * .9}px sans-serif`; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
        ctx.fillText('?', x, y - r * .15);
        break;
      }
      case 'excl': {
        ctx.arc(x, y, r * .8, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
        ctx.strokeStyle = done ? '#9fb09f' : '#fff';
        ctx.lineWidth = Math.max(1, size * .14);
        ctx.beginPath(); ctx.moveTo(x, y - r * .4); ctx.lineTo(x, y + r * .15); ctx.stroke();
        ctx.beginPath(); ctx.arc(x, y + r * .45, Math.max(1.2, size * .1), 0, Math.PI * 2); ctx.stroke();
        break;
      }
      default: { // dot
        ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
      }
    }
    ctx.restore();
    if (done) {
      // 完成标记：绿色小对勾
      ctx.strokeStyle = '#7dffa0';
      ctx.lineWidth = Math.max(1.5, size * .16);
      ctx.beginPath();
      ctx.moveTo(x - size * .22, y); ctx.lineTo(x - size * .05, y + size * .18); ctx.lineTo(x + size * .26, y - size * .16);
      ctx.stroke();
    }
  }

  /* ---------- 测量 ---------- */
  _drawMeasure(ctx) {
    if (!this.measurePoints.length) return;
    const pts = this.measurePoints;
    ctx.save();
    ctx.strokeStyle = '#ffd166';
    ctx.lineWidth = 2;
    ctx.setLineDash([6, 5]);
    ctx.beginPath();
    for (let i = 0; i < pts.length; i++) {
      const [sx, sy] = this.m2s(pts[i]);
      if (i === 0) ctx.moveTo(sx, sy); else ctx.lineTo(sx, sy);
    }
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.fillStyle = '#ffd166';
    for (const p of pts) {
      const [sx, sy] = this.m2s(p);
      ctx.beginPath(); ctx.arc(sx, sy, 4, 0, Math.PI * 2); ctx.fill();
    }
    // 分段距离标注
    ctx.font = '11px sans-serif';
    ctx.textAlign = 'center';
    let total = 0;
    for (let i = 1; i < pts.length; i++) {
      const dx = (pts[i][0] - pts[i-1][0]) * 0.5, dy = (pts[i][1] - pts[i-1][1]) * 0.5;
      const seg = Math.sqrt(dx * dx + dy * dy);
      total += seg;
      const mx = (pts[i-1][0] + pts[i][0]) / 2, my = (pts[i-1][1] + pts[i][1]) / 2;
      const [sx, sy] = this.m2s([mx, my]);
      ctx.fillStyle = 'rgba(8,12,8,.8)';
      ctx.strokeStyle = 'rgba(8,12,8,.8)';
      ctx.lineWidth = 3;
      const txt = (seg >= 1000 ? (seg / 1000).toFixed(1) + ' km' : Math.round(seg) + ' m');
      ctx.strokeText(txt, sx, sy - 6);
      ctx.fillStyle = '#ffd166';
      ctx.fillText(txt, sx, sy - 6);
    }
    const [lsx, lsy] = this.m2s(pts[pts.length - 1]);
    const totTxt = '合计 ' + (total >= 1000 ? (total / 1000).toFixed(2) + ' km' : Math.round(total) + ' m');
    ctx.strokeText(totTxt, lsx + 10, lsy - 14);
    ctx.fillStyle = '#fff';
    ctx.fillText(totTxt, lsx + 10, lsy - 14);
    ctx.restore();
  }

  _drawCustom(ctx) {
    for (const mk of this.customMarkers) {
      const [sx, sy] = this.m2s(mk.px);
      ctx.save();
      ctx.fillStyle = mk.color;
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 2;
      const r = 9;
      ctx.beginPath();
      ctx.moveTo(sx, sy + r + 4);
      ctx.arc(sx, sy - r * .2, r, 0, Math.PI * 2);
      ctx.closePath();
      ctx.fill(); ctx.stroke();
      ctx.fillStyle = '#fff';
      ctx.font = '11px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'bottom';
      ctx.strokeStyle = 'rgba(8,12,8,.85)';
      ctx.lineWidth = 3;
      ctx.strokeText(mk.name, sx, sy - r - 4);
      ctx.fillText(mk.name, sx, sy - r - 4);
      ctx.restore();
    }
  }

  /* ---------- 命中测试 ---------- */
  hitTest(sx, sy) {
    const mx = (sx - this.view.x) / this.view.scale, my = (sy - this.view.y) / this.view.scale;
    // 自定义标记优先
    for (let i = this.customMarkers.length - 1; i >= 0; i--) {
      const c = this.customMarkers[i];
      if (Math.abs(c.px[0] - mx) < 12 / this.view.scale && Math.abs(c.px[1] - my) < 12 / this.view.scale) {
        return { custom: c };
      }
    }
    const vis = this._visibleMarkers(50);
    // 屏幕距离优先，同屏距离内选优先级最高的类别
    let best = null, bestOrder = -1, bestDist = 1e18;
    for (const mk of vis) {
      if (!this.enabled.has(mk.cat)) continue;
      const dx = mk.px[0] - mx, dy = mk.px[1] - my;
      const scrD = Math.hypot(dx, dy) * this.view.scale;
      const size = this._markerScreenSize(mk);
      if (scrD <= 15 + size / 2) {
        const order = CAT_CFG[mk.cat]?.order || 0;
        if (order > bestOrder || (order === bestOrder && scrD < bestDist)) {
          bestOrder = order; bestDist = scrD; best = mk;
        }
      }
    }
    const mhit = this._matHitTest(mx, my);
    if (mhit) return mhit;
    // 克洛格挑战起点第二标记（v1.1.1）：点在起点上 → 选中该克洛格
    if (!best) {
      const startMk = this._hitKorokStart(mx, my);
      if (startMk) return { marker: startMk };
    }
    return best ? { marker: best } : null;
  }

  /* ---------- 事件 ---------- */
  _bindEvents() {
    const cv = this.canvas;
    const self = this;
    let dragging = false, lastX = 0, lastY = 0, downX = 0, downY = 0, moved = 0;

    /* ---------- 触摸：单指拖拽 / 双指捏合缩放 / 双击放大 ---------- */
    const touches = new Map();        // identifier -> {x, y}
    let pinchDist = 0, pinchMid = [0, 0], tDragging = false;
    let tLast = [0, 0], tMoved = 0, tTapAt = 0, tTapX = 0, tTapY = 0;

    function tapSelect(sx, sy) {
      // 轻点 = 与 click 相同的选中逻辑（pin/measure 模式另行处理）
      if (self.mode === 'pin' || self.mode === 'measure') return;
      const hit = self.hitTest(sx, sy);
      if (hit) {
        if (hit.custom) self._selectCustom(hit.custom);
        else if (hit.material != null) {
          if (hit.cluster != null) self._expandCluster(hit.material, hit.cluster, hit.matPx[0], hit.matPx[1], sx, sy);
          else self._selectMaterial(hit.material, hit.matPx);
        }
        else self._selectMarker(hit.marker);
      } else {
        self._selectMarker(null);
        if (self.matSel != null) self._selectMaterial(null);
      }
    }

    cv.addEventListener('touchstart', e => {
      e.preventDefault();
      const r = cv.getBoundingClientRect();
      for (const t of e.changedTouches) {
        touches.set(t.identifier, { x: t.clientX - r.left, y: t.clientY - r.top });
      }
      if (touches.size === 1) {
        const pt = [...touches.values()][0];
        tLast = [pt.x, pt.y]; tMoved = 0;
        if (this.mode === 'pin') {
          // 手指落点即放置标记
          const m = this.s2m(pt.x, pt.y);
          if (m[0] >= 0 && m[1] >= 0 && m[0] <= this.MW && m[1] <= this.MH) {
            if (this._onPinClick) this._onPinClick(pt.x, pt.y, [Math.round(m[0]), Math.round(m[1])]);
          }
        } else if (this.mode === 'measure') {
          const m = this.s2m(pt.x, pt.y);
          this.measurePoints.push([Math.max(0, Math.min(this.MW, m[0])), Math.max(0, Math.min(this.MH, m[1]))]);
          this.draw();
        } else {
          tDragging = true;
        }
      } else if (touches.size === 2) {
        tDragging = false;
        const [a, b] = [...touches.values()];
        pinchDist = Math.hypot(a.x - b.x, a.y - b.y);
        pinchMid = [(a.x + b.x) / 2, (a.y + b.y) / 2];
      }
    }, { passive: false });

    cv.addEventListener('touchmove', e => {
      e.preventDefault();
      const r = cv.getBoundingClientRect();
      for (const t of e.changedTouches) {
        if (touches.has(t.identifier)) {
          touches.set(t.identifier, { x: t.clientX - r.left, y: t.clientY - r.top });
        }
      }
      if (touches.size >= 2) {
        const [a, b] = [...touches.values()];
        const d = Math.hypot(a.x - b.x, a.y - b.y);
        const mid = [(a.x + b.x) / 2, (a.y + b.y) / 2];
        if (pinchDist > 0 && d > 0) this.zoomAt(mid[0], mid[1], d / pinchDist);
        this.view.x += (mid[0] - pinchMid[0]);
        this.view.y += (mid[1] - pinchMid[1]);
        pinchDist = d; pinchMid = mid;
        this._onViewChange && this._onViewChange();
        this.draw();
      } else if (touches.size === 1 && tDragging) {
        const pt = [...touches.values()][0];
        const dx = pt.x - tLast[0], dy = pt.y - tLast[1];
        tMoved += Math.abs(dx) + Math.abs(dy);
        this.view.x += dx; this.view.y += dy;
        tLast = [pt.x, pt.y];
        this._onViewChange && this._onViewChange();
        this.draw();
      }
    }, { passive: false });

    cv.addEventListener('touchend', e => {
      e.preventDefault();
      const now = Date.now();
      const r = cv.getBoundingClientRect();
      let tapPt = null;
      for (const t of e.changedTouches) {
        if (touches.has(t.identifier)) tapPt = touches.get(t.identifier);
        touches.delete(t.identifier);
      }
      if (touches.size === 0) {
        if (tDragging && tMoved < 8 && tapPt) {
          // 未拖动 = 轻点：双击放大 or 单击选中
          if (now - tTapAt < 320 && Math.hypot(tapPt.x - tTapX, tapPt.y - tTapY) < 24) {
            this.zoomAt(tapPt.x, tapPt.y, 1.8);
            this._onViewChange && this._onViewChange();
            tTapAt = 0;
          } else {
            tTapAt = now; tTapX = tapPt.x; tTapY = tapPt.y;
            setTimeout(() => { tTapAt = 0; }, 340);
            tapSelect(tapPt.x, tapPt.y);
          }
        }
        if (tDragging) {
          tDragging = false;
          if (tMoved > 8) this._onViewChange && this._onViewChange();
        }
        pinchDist = 0;
      }
    }, { passive: false });

    cv.addEventListener('touchcancel', () => {
      touches.clear(); tDragging = false; pinchDist = 0;
    }, { passive: true });

    cv.addEventListener('mousedown', e => {
      if (e.button !== 0) return;
      const r = cv.getBoundingClientRect();
      const sx = e.clientX - r.left, sy = e.clientY - r.top;
      if (this.mode === 'measure') {
        const m = this.s2m(sx, sy);
        this.measurePoints.push([Math.max(0, Math.min(this.MW, m[0])), Math.max(0, Math.min(this.MH, m[1]))]);
        this.draw();
        return;
      }
      if (this.mode === 'pin') {
        const m = this.s2m(sx, sy);
        if (m[0] >= 0 && m[1] >= 0 && m[0] <= this.MW && m[1] <= this.MH) {
          if (this._onPinClick) this._onPinClick(sx, sy, [Math.round(m[0]), Math.round(m[1])]);
        }
        return;
      }
      dragging = true; moved = 0;
      downX = e.clientX; downY = e.clientY;
      lastX = e.clientX; lastY = e.clientY;
      cv.classList.add('dragging');
    });

    window.addEventListener('mousemove', e => {
      const r = cv.getBoundingClientRect();
      const sx = e.clientX - r.left, sy = e.clientY - r.top;
      if (dragging) {
        moved += Math.abs(e.clientX - lastX) + Math.abs(e.clientY - lastY);
        this.view.x += (e.clientX - lastX);
        this.view.y += (e.clientY - lastY);
        lastX = e.clientX; lastY = e.clientY;
        this.draw();
      } else {
        // 悬停 + 坐标读数
        const m = this.s2m(sx, sy);
        if (this._onCoord) this._onCoord(this.gameCoord(m), m);
        const hit = this.mode === 'browse' ? this.hitTest(sx, sy) : null;
        this.hover = hit && hit.marker ? hit.marker : null;
        if (this._onHover) this._onHover(this.hover);
        if (this.mode === 'browse') this.draw();
      }
    });

    window.addEventListener('mouseup', () => {
      if (!dragging) return;
      dragging = false;
      cv.classList.remove('dragging');
      this._onViewChange && this._onViewChange();
    });

    cv.addEventListener('click', e => {
      if (this.mode !== 'browse') return;
      if (moved > 6) return; // 拖拽后不触发点击
      const r = cv.getBoundingClientRect();
      const hit = this.hitTest(e.clientX - r.left, e.clientY - r.top);
      if (hit) {
        if (hit.custom) this._selectCustom(hit.custom);
        else if (hit.material != null) {
          if (hit.cluster != null) this._expandCluster(hit.material, hit.cluster, hit.matPx[0], hit.matPx[1], e.clientX - r.left, e.clientY - r.top);
          else this._selectMaterial(hit.material, hit.matPx);
        }
        else this._selectMarker(hit.marker);
      } else {
        this._selectMarker(null);
        if (this.matSel != null) this._selectMaterial(null);
      }
    });

    cv.addEventListener('wheel', e => {
      e.preventDefault();
      const r = cv.getBoundingClientRect();
      const sx = e.clientX - r.left, sy = e.clientY - r.top;
      const factor = e.deltaY < 0 ? 1.22 : 1 / 1.22;
      this.zoomAt(sx, sy, factor);
      this._onViewChange && this._onViewChange();
    }, { passive: false });

    cv.addEventListener('dblclick', e => {
      if (this.mode === 'measure') {
        if (this.measurePoints.length >= 2) {
          // 结束测量：保留折线展示，切回浏览模式
          this.mode = 'browse';
          this.canvas.classList.remove('measuring');
          this._onModeChange && this._onModeChange('browse');
          this.draw();
        }
        return;
      }
      const r = cv.getBoundingClientRect();
      this.zoomAt(e.clientX - r.left, e.clientY - r.top, 1.8);
      this._onViewChange && this._onViewChange();
    });

    window.addEventListener('keydown', e => {
      if (e.key === 'Escape') {
        if (this.mode === 'measure' && this.measurePoints.length) { this.measurePoints = []; this.draw(); }
        else if (this.mode !== 'browse') { this.setMode('browse'); }
        else if (this.measurePoints.length) { this.measurePoints = []; this.draw(); }
      }
    });
  }

  setMode(mode) {
    this.mode = mode;
    this.canvas.classList.toggle('measuring', mode === 'measure');
    this.canvas.classList.toggle('pinning', mode === 'pin');
    if (mode === 'measure') this.measurePoints = []; // 进入测量时清空旧折线
    this.draw();
    this._onModeChange && this._onModeChange(mode);
  }

  addCustomMarker(name, color, px) {
    const mk = { id: 'custom_' + Date.now(), name: name || '标记', color: color || '#ff5252', px: [Math.round(px[0]), Math.round(px[1])] };
    this.customMarkers.push(mk);
    this._onCustomChange();
    this.draw();
    return mk;
  }

  _selectMarker(mk) {
    this.selected = mk || null;
    this._onSelect && this._onSelect(mk ? { marker: mk } : null);
    if (mk) this.draw();
  }
  _selectCustom(mk) {
    this._onSelect && this._onSelect({ custom: mk });
  }

  _onCustomChange() { /* ui 覆盖 */ }

  /* ================= 材料追踪层（v1.1.0） ================= */

  /* 材料索引：entry 查找 + 每材料点列表（用于高亮/搜索/详情） */
  _buildMatIndex() {
    const ms = this.MATS.materials;
    for (const m of ms) this.matByEntry[m.entry] = m;
    const pts = this.MATS.points;
    this.matPointsBy = new Array(ms.length);
    for (let i = 0; i < pts.length; i += 3) {
      const id = pts[i], px = pts[i + 1], py = pts[i + 2];
      (this.matPointsBy[id] = this.matPointsBy[id] || []).push([px, py]);
    }
  }

  /* 预加载 74 个材料图标（app/assets/materials/<entry>.webp） */
  _loadMatIcons() {
    if (!this.MATS) return;
    const seen = new Set();
    for (const m of this.MATS.materials) {
      if (seen.has(m.entry)) continue;
      seen.add(m.entry);
      const img = new Image();
      img.onload = () => this.draw();
      img.src = 'assets/materials/' + m.entry + '.webp';
      this.matIcons[m.entry] = img;
    }
  }

  /* Supercluster 坐标归一化：地图像素 → [lng,lat]（lat 压到 ±85 防 mercator 爆） */
  _pxLng(px) { return (px / this.MW) * 360 - 180; }
  _pyLat(py) { return (py / this.MH) * 170 - 85; }
  _lngPx(lng) { return (lng + 180) / 360 * this.MW; }
  _latPy(lat) { return (lat + 85) / 170 * this.MH; }

  /* 屏幕 scale → supercluster zoom（0=最聚合 … 16=全单点） */
  _mapZoom() {
    const s = Math.max(0.03, this.view.scale);
    return Math.max(0, Math.min(16, Math.log2(s / 0.03) * 2.84));
  }
  _unmapZoom(ez) { return 0.03 * Math.pow(2, ez / 2.84); }

  /* 点击聚合点：以点击点为中心放大到 supercluster 建议 zoom，自动散开 */
  _expandCluster(materialId, clusterId, mx, my, sx, sy) {
    const ez = this._getClusterer(materialId).getClusterExpansionZoom(clusterId);
    const newScale = Math.min(this.maxScale, this._unmapZoom(ez) * 1.1);
    this.view.x = sx - mx * newScale;
    this.view.y = sy - my * newScale;
    this.view.scale = newScale;
    this.draw();
  }

  /* 懒加载：每种材料一个 Supercluster 实例，首次勾选时建立并缓存 */
  _getClusterer(id) {
    if (this.matClusters[id]) return this.matClusters[id];
    const pts = this.matPointsBy[id] || [];
    const feats = pts.map((pt) => ({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [this._pxLng(pt[0]), this._pyLat(pt[1])] },
      properties: { id: id }
    }));
    const c = new Supercluster({ radius: 52, maxZoom: 16, minZoom: 0, nodeSize: 64 });
    c.load(feats);  // 此版 load 直接接受 features 数组
    this.matClusters[id] = c;
    return c;
  }

  /* 材料图标：官方 StockItem 缩略图，缺失时回退类目色圆点 */
  _matIcon(ctx, id, sx, sy, size) {
    const m = this.MATS.materials[id];
    const img = this.matIcons[m.entry];
    if (img && img.complete && img.naturalWidth > 0) {
      ctx.drawImage(img, sx - size / 2, sy - size / 2, size, size);
    } else {
      const cfg = MAT_CAT_CFG[m.cat] || {};
      ctx.beginPath();
      ctx.arc(sx, sy, size / 2, 0, Math.PI * 2);
      ctx.fillStyle = cfg.color || '#888';
      ctx.fill();
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 1.5;
      ctx.stroke();
    }
  }

  /* 数量徽标：小圆角底 + 数字 */
  _matBadge(ctx, sx, sy, n) {
    const t = String(n);
    ctx.font = '700 10px sans-serif';
    const tw = ctx.measureText(t).width;
    const bw = Math.max(16, tw + 8), bh = 13;
    const x = sx - bw / 2, y = sy - bh / 2;
    ctx.fillStyle = 'rgba(10,14,10,.88)';
    this._roundRect(ctx, x, y, bw, bh, 6);
    ctx.fill();
    ctx.strokeStyle = 'rgba(255,255,255,.55)';
    ctx.lineWidth = 1;
    ctx.stroke();
    ctx.fillStyle = '#fff';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(t, sx, y + bh / 2 + 0.5);
  }

  /* 材料层主绘制（Supercluster）：每种启用材料各自聚合，互不混合 */
  _drawMaterials(ctx) {
    if (!this.MATS || this.matIds.size === 0) return;
    const z = this._mapZoom();
    const pad = 60;
    const x0 = Math.max(0, (-pad - this.view.x) / this.view.scale);
    const y0 = Math.max(0, (-pad - this.view.y) / this.view.scale);
    const x1 = Math.min(this.MW, (this.w + pad - this.view.x) / this.view.scale);
    const y1 = Math.min(this.MH, (this.h + pad - this.view.y) / this.view.scale);
    const bbox = [this._pxLng(x0), this._pyLat(y0), this._pxLng(x1), this._pyLat(y1)];
    for (const id of this.matIds) {
      const feats = this._getClusterer(id).getClusters(bbox, z);
      for (const f of feats) {
        const [lng, lat] = f.geometry.coordinates;
        const px = this._lngPx(lng), py = this._latPy(lat);
        const [sx, sy] = this.m2s([px, py]);
        if (sx < -60 || sy < -60 || sx > this.w + 60 || sy > this.h + 60) continue;
        const isCl = !!f.properties.cluster;
        const size = (id === this.matSel) ? 36 : (isCl ? 30 : 26);
        this._matIcon(ctx, id, sx, sy, size);
        if (isCl) this._matBadge(ctx, sx, sy - size / 2 - 9, f.properties.point_count);
      }
    }
  }

  /* 选中材料高亮：该材料所有点加白色描边环（画在最上层） */
  _drawMatSel(ctx) {
    if (this.matSel == null) return;
    const pts = this.matPointsBy[this.matSel];
    if (!pts || !pts.length) return;
    const pad = 24;
    const x0 = (-pad - this.view.x) / this.view.scale;
    const y0 = (-pad - this.view.y) / this.view.scale;
    const x1 = (this.w + pad - this.view.x) / this.view.scale;
    const y1 = (this.h + pad - this.view.y) / this.view.scale;
    for (let i = 0; i < pts.length; i++) {
      const px = pts[i][0], py = pts[i][1];
      if (px < x0 || py < y0 || px > x1 || py > y1) continue;
      const [sx, sy] = this.m2s([px, py]);
      ctx.beginPath();
      ctx.arc(sx, sy, 15, 0, Math.PI * 2);
      ctx.strokeStyle = 'rgba(255,255,255,.95)';
      ctx.lineWidth = 2;
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(sx, sy, 19, 0, Math.PI * 2);
      ctx.strokeStyle = 'rgba(0,0,0,.45)';
      ctx.lineWidth = 1;
      ctx.stroke();
    }
  }

  /* 材料层命中测试（Supercluster）：查各启用材料当前 zoom 的簇/点，屏幕距离最近者 */
  _matHitTest(mx, my) {
    if (!this.MATS || this.matIds.size === 0) return null;
    const z = this._mapZoom();
    const R = 44 / this.view.scale;  // 屏幕命中半径 44px → 世界
    const bbox = [this._pxLng(mx - R), this._pyLat(my - R), this._pxLng(mx + R), this._pyLat(my + R)];
    let best = null, bestD = 26;
    for (const id of this.matIds) {
      const feats = this._getClusterer(id).getClusters(bbox, z);
      for (const f of feats) {
        const [lng, lat] = f.geometry.coordinates;
        const px = this._lngPx(lng), py = this._latPy(lat);
        const d = Math.hypot(px - mx, py - my) * this.view.scale;
        if (d <= bestD) {
          bestD = d;
          best = { material: id, matPx: [px, py], cluster: f.properties.cluster ? f.properties.cluster_id : null };
        }
      }
    }
    return best;
  }

  /* 选中材料（id=null 取消）：通知 UI 显示材料详情卡 */
  _selectMaterial(id, matPx) {
    this.matSel = (id != null) ? id : null;
    if (this._onSelect) {
      this._onSelect(id != null ? { material: this.MATS.materials[id], matPx: matPx || null } : null);
    }
    this.draw();
  }
}
