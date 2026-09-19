/* ============ 旷野之息互动地图 · Live 实时层 ============
 *
 * 对接本地追踪服务（跨域模式，页面可在在线部署站 https://botw.yalin.site/ 打开）：
 *   GET  /pos     玩家位置 + 目标 + 配置（600ms 轮询）
 *   POST /target  设置 / 清除导航目标
 *   POST /config  把当前图层勾选同步给服务
 *   data/progress_points.json  存档收集进度（本地 Python 版服务重建；在线版走上传存档）
 *
 * 能力：
 *   1. 玩家位置红点 + 移动轨迹
 *   2. 点击标注 →「🧭 导航」→ 金色引导线 + 距离
 *   3. 存档收集进度吸收式合并：存档已收集的标注自动写回本地完成集合（持久化，离线可见）
 *   4. 统计面板统一为一套「收集进度」（存档同步 + 手动标记叠加）
 *   5. 服务离线时全站静默降级，普通地图功能不受影响
 */
'use strict';

const LIVE_API = 'http://127.0.0.1:8766';   // 本地追踪服务（Go 版"灵墟地图追踪助手"）

const LIVE = (() => {
  let map = null;
  let lastPosKey = null;      // 玩家位置指纹（防无效重绘）
  let lastDrawKey = null;
  let cfgSent = null;         // 已同步给服务的类别签名
  let progLoading = false, lastProgFetch = 0;
  const K_FOLLOW = 'botwmap.follow.v1';
  let follow = false;
  try { follow = localStorage.getItem(K_FOLLOW) !== '0'; } catch (e) {}

  const CAT_CN = {};                        // 类别 key -> 中文名
  const DONE_TYPES = { shrine: 'shrine', tower: 'tower', seed: 'korok', memory: 'memory', beast: 'beast' };

  function init(m) {
    map = m;
    m.data.categories.forEach(c => { if (c.cn) CAT_CN[c.key] = c.cn; });
    map.live = {
      mx: null, my: null, gx: 0, gy: 0, gz: 0,
      online: false, verified: false,
      trail: [], tgt: null, tdist: null, counts: null, progGen: '',
    };
    map.liveDone = new Set();

    const b = document.getElementById('btnFollow');
    if (b) {
      b.classList.toggle('on', follow);
      b.addEventListener('click', () => {
        follow = !follow;
        try { localStorage.setItem(K_FOLLOW, follow ? '1' : '0'); } catch (e) {}
        b.classList.toggle('on', follow);
        if (follow && map.live.mx != null) { centerOnPlayer(); map.draw(); }
        UI.toast(follow ? '已开启：视图跟随玩家' : '已关闭跟随');
      });
    }

    const cbStop = document.getElementById('collectStop');
    if (cbStop) cbStop.addEventListener('click', () => { stopCollectMode(); UI.toast('已结束收集模式'); });

    loadProgress();
    setInterval(poll, 600);
    setInterval(loadProgress, 20000);
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) loadProgress();
    });
  }

  /* ---------- 玩家位置轮询 ---------- */
  async function poll() {
    let p = null;
    try {
      const r = await fetch(LIVE_API + '/pos?who=web&t=' + Date.now(), { cache: 'no-store' });
      if (r.ok) p = await r.json();
    } catch (e) { p = null; }

    const L = map.live;
    L.online = !!(p && p.ok);
    if (L.online) {
      L.mx = p.mx; L.my = p.my;
      L.gx = p.gx; L.gy = p.gy; L.gz = p.gz;
      L.verified = !!p.verified;
      L.tgt = (p.target && typeof p.target.x === 'number') ? p.target : null;
      L.tdist = L.tgt ? Math.hypot(L.tgt.x - p.mx, L.tgt.y - p.my) * 0.5 : null;
      const key = Math.round(p.mx) + ',' + Math.round(p.my);
      if (key !== lastPosKey) {
        lastPosKey = key;
        L.trail.push([p.mx, p.my]);
        if (L.trail.length > 600) L.trail.shift();
        if (follow) { centerOnPlayer(); }
      }
      syncConfig();
      // 进度代次变化（游戏内保存 → 存档更新）→ 立即重新拉取收集进度
      if (typeof p.progressGen === 'string' && p.progressGen && p.progressGen !== map.live.progGen) {
        const now = Date.now();
        if (now - lastProgFetch > 3000) { lastProgFetch = now; loadProgress(); }
      }
    } else {
      lastPosKey = null;
    }
    updateStatus();
    tickCollectMode();
    const dk = (L.online ? '1' : '0') + (L.verified ? 'v' : '') +
               (L.mx != null ? Math.round(L.mx / 2) + ':' + Math.round(L.my / 2) : '') +
               (L.tgt ? 't' : '') + (L.tdist != null ? Math.round(L.tdist / 10) : '');
    if (dk !== lastDrawKey) {
      lastDrawKey = dk;
      map.draw();
    }
  }

  /* ---------- 状态胶囊 ---------- */
  function updateStatus() {
    const el = document.getElementById('liveStatus');
    if (!el) return;
    const L = map.live;
    let txt, cls;
    if (!L.online) {
      txt = '● 离线 · 可上传存档同步进度'; cls = 'off';
    } else if (!L.verified) {
      txt = '◐ 定位中… 请移动角色'; cls = 'warn';
    } else {
      txt = '● ' + Math.round(L.gx) + ',' + Math.round(L.gy) +
            (L.tdist != null ? ' · 目标 ' + Math.round(L.tdist) + 'm' : '');
      cls = 'on';
    }
    if (el.textContent !== txt || el.className.indexOf(cls) < 0) {
      el.textContent = txt;
      el.className = 'live-status ' + cls;
    }
  }

  /* ---------- 图层勾选 → 服务配置（悬浮窗读取同一份） ---------- */
  function syncConfig() {
    const cats = [...map.enabled].map(k => CAT_CN[k]).filter(Boolean);
    const sig = cats.slice().sort().join(',');
    if (sig === cfgSent) return;
    cfgSent = sig;
    fetch(LIVE_API + '/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cats }),
    }).catch(() => { cfgSent = null; });
  }

  /* ---------- 存档进度加载（标点就近匹配 + 计数） ---------- */
  // 优先从本地追踪服务 /progress 实时拉取（游戏内保存后自动更新）；
  // 服务不可用时降级到静态文件（旧 Python 版/本地构建产物）。
  async function loadProgress() {
    if (progLoading) return;
    progLoading = true;
    try {
      const r = await fetch(LIVE_API + '/progress?t=' + Date.now(), { cache: 'no-store' });
      if (r.ok) { applyProgress(await r.json()); }
      else {
        const r2 = await fetch('../data/progress_points.json?t=' + Date.now(), { cache: 'no-store' });
        if (r2.ok) applyProgress(await r2.json());
      }
    } catch (e) { /* 离线时静默 */ } finally { progLoading = false; }
  }

  function applyProgress(j) {
    const points = j.points || [];
    // 已完成点的空间网格（128px 格）
    const CELL = 128;
    const grid = new Map();
    for (const p of points) {
      if (!p.done) continue;
      const gk = Math.floor(p.x / CELL) + '_' + Math.floor(p.y / CELL);
      if (!grid.has(gk)) grid.set(gk, []);
      grid.get(gk).push(p);
    }
    // 每个可收集标注：附近 8px 内存在同类型存档点 → 存档已完成
    const done = new Set();
    for (const mk of map.markers) {
      const t = DONE_TYPES[mk.cat];
      if (!t) continue;
      const cx = Math.floor(mk.px[0] / CELL), cy = Math.floor(mk.px[1] / CELL);
      let hit = false;
      for (let dx = -1; dx <= 1 && !hit; dx++) {
        for (let dy = -1; dy <= 1 && !hit; dy++) {
          const arr = grid.get((cx + dx) + '_' + (cy + dy));
          if (!arr) continue;
          for (const p of arr) {
            if (p.t === t && Math.hypot(p.x - mk.px[0], p.y - mk.px[1]) <= 8) {
              hit = true; break;
            }
          }
        }
      }
      if (hit) done.add(mk.id);
    }
    const changed = map.liveDone.size !== done.size;
    map.liveDone = done;
    // 吸收式合并：存档已收集 → 写回本地完成集合（持久化，离线打开也保留；以存档为准）
    let grew = false;
    for (const id of done) {
      if (!map.doneSet.has(id)) { map.doneSet.add(id); grew = true; }
    }
    if (grew) UI.persistDone();
    map.live.counts = j.counts || null;
    map.live.progGen = j.generated || '';
    if (changed || grew) { map.draw(); UI.renderStats(); UI.updateLayerList(); }
  }

  /* ---------- 视图跟随玩家 ---------- */
  function centerOnPlayer() {
    const L = map.live;
    if (L.mx == null) return;
    const s = Math.max(map.view.scale, 0.55);
    map.view.x = map.w / 2 - L.mx * s;
    map.view.y = map.h / 2 - L.my * s;
    map.view.scale = s;
  }

  /* ---------- 导航目标 ---------- */
  function navigate(mk) {
    if (!map.live.online) { UI.toast('服务未连接，无法导航'); return; }
    // 收集模式中导航其他类别标注 → 退出收集模式（当前引导线保留）
    if (mode && mk.cat !== mode.cat) stopCollectMode();
    curId = mk.id;
    if (mode) { mode.arrivedShown = false; renderCollectBar(); }
    postTarget(mk, '已设置导航：' + (mk.name || mk.en || '目标'));
  }

  function clearNav() {
    fetch(LIVE_API + '/target', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ clear: true }),
    }).catch(() => {});
  }

  /* 导航到克洛格挑战起点（v1.1.1 第二标记）：直接设服务端目标，不进入收集模式 */
  function navigateToStart(ks) {
    if (!map.live.online) { UI.toast('服务未连接，无法导航'); return; }
    if (mode) stopCollectMode();
    curId = null;   // 避免收集模式 / 目标核验把它当成普通克洛格点
    postTarget({ name: ks.name, px: ks.start, cat: 'seed' }, '已设置导航：' + ks.name);
  }

  /* ---------- 收集模式（回忆 / 克洛格 连续自动导航） ----------
   * 点回忆 / 克洛格标注的「导航」时，由标注卡上的「收集」按钮开启：
   *   - 每 600ms 轮询：当前目标已收集（存档同步 / 手动勾选）→ 自动设下一个
   *     = 离玩家最近的未收集同类点；全部收集完 → 自动退出
   *   - 到达（≤15m）只提示不跳转：收集并保存后自动前进，避免还没解谜就被带走
   *   - 范围：克洛格 = 全部未收集；回忆 = 有存档 flag 的 13 个主线回忆
   *     （EX / 英杰回忆无收集记录，不自动排队，仍可手动导航单点）
   *   - 退出：横幅「结束」/ 手动导航其他类别标注 / 服务离线自动暂停（恢复后继续）
   */
  // 照片回忆 13 个（tier='photo'，含最终 #17）才进收集自动队列；EX / 英杰 / 大师剑自动获得不排队
  function isPhotoMemory(mk) {
    return !!(mk.extra && mk.extra.tier === 'photo');
  }
  const ARRIVE_M = 15;                 // 到达提示阈值（游戏米，1px = 0.5m）
  let mode = null;                     // { cat, arrivedShown } 当前收集模式
  let curId = null;                    // 当前导航目标标记 id

  function collectPool(cat) {
    const out = [];
    for (const mk of map.markers) {
      if (mk.cat !== cat) continue;
      if (cat === 'memory' && !isPhotoMemory(mk)) continue;  // EX / 英杰 / 大师剑不进自动队列
      if (map._mkDone(mk)) continue;                      // 已收集（存档同步 / 手动勾选）
      out.push(mk);
    }
    return out;
  }
  function nearestIn(pool) {
    const L = map.live;
    const px = L.mx != null ? L.mx : map.MW / 2;
    const py = L.my != null ? L.my : map.MH / 2;
    let best = null, bd = Infinity;
    for (const mk of pool) {
      const d = Math.hypot(mk.px[0] - px, mk.px[1] - py);
      if (d < bd) { bd = d; best = mk; }
    }
    return best;
  }
  function collectCounts(cat) {
    // 回忆按照片 13 个现算（存档逐点已同步到标记）；克洛格用服务返回的 900 总数
    const c = map.live.counts || {};
    if (cat === 'seed' && c.korok) return c.korok;
    let t = 0, d = 0;
    for (const mk of map.markers) {
      if (mk.cat !== cat) continue;
      if (cat === 'memory' && !isPhotoMemory(mk)) continue;
      t++;
      if (map._mkDone(mk)) d++;
    }
    return [d, t];
  }
  let _idMap = null, _coordMap = null;
  function byId(id) {
    if (!_idMap) { _idMap = new Map(); map.markers.forEach(mk => _idMap.set(mk.id, mk)); }
    return _idMap.get(id) || null;
  }
  function byCoords(x, y) {
    if (!_coordMap) {
      _coordMap = new Map();
      map.markers.forEach(mk => _coordMap.set(Math.round(mk.px[0]) + '_' + Math.round(mk.px[1]), mk));
    }
    return _coordMap.get(Math.round(x) + '_' + Math.round(y)) || null;
  }
  // 当前服务端目标对应的标记（先按记忆的 curId 坐标核验，再按坐标兜底）
  function targetMarker() {
    const t = map.live.tgt;
    if (!t) return null;
    if (curId) {
      const mk = byId(curId);
      if (mk && Math.abs(mk.px[0] - t.x) < 2 && Math.abs(mk.px[1] - t.y) < 2) return mk;
    }
    return byCoords(t.x, t.y);
  }
  function postTarget(mk, toastTxt) {
    fetch(LIVE_API + '/target', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: mk.name || mk.en || '', x: mk.px[0], y: mk.px[1], type: mk.cat }),
    }).then(r => r.json()).then(() => {
      if (toastTxt) UI.toast(toastTxt);
    }).catch(() => { if (toastTxt) UI.toast('导航设置失败'); });
  }
  function startCollectMode(cat, mk) {
    if (!map.live.online) { UI.toast('服务未连接，无法开启收集模式'); return; }
    if (mode) stopCollectMode();
    const pool = collectPool(cat);
    if (!pool.length) { UI.toast('🎉 ' + (CAT_CN[cat] || cat) + '已全部收集完成'); return; }
    let first = null;
    if (cat === 'seed' && mk && !map._mkDone(mk)) first = mk;      // 克洛格：点哪个导航哪个
    if (!first) first = nearestIn(pool);                           // 回忆：最近的未收集
    mode = { cat, arrivedShown: false };
    curId = first.id;
    UI.toast('🧭 收集模式开启 · 目标：' + first.name + '（剩余 ' + pool.length + ' 个）');
    postTarget(first, '');
    renderCollectBar();
    map.draw();
  }
  function stopCollectMode() {
    if (!mode) return;
    mode = null;
    renderCollectBar();               // 隐藏横幅；当前引导线保留
    if (UI && UI.syncCollectBtn) UI.syncCollectBtn();
  }
  function isModeActive(cat) { return !!(mode && mode.cat === cat); }

  // 每轮轮询的收集模式状态机
  function tickCollectMode() {
    if (!mode) return;
    const L = map.live;
    if (!L.online) { renderCollectBar(); return; }                // 离线暂停，恢复后继续
    const pool = collectPool(mode.cat);
    if (!pool.length) {                                            // 该类别全部收集完成
      UI.toast('🎉 ' + (CAT_CN[mode.cat] || mode.cat) + '已全部收集完成');
      fetch(LIVE_API + '/target', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ clear: true }),
      }).catch(() => {});
      stopCollectMode();
      return;
    }
    const cur = targetMarker();
    const curInPool = !!(cur && pool.some(x => x.id === cur.id));
    if (!cur || cur.id !== curId || !curInPool) {                  // 无目标 / 已收集 / 目标失效 → 自动前进
      const donePrev = !!(cur && cur.id === curId && map._mkDone(cur));
      const next = nearestIn(pool);
      if (!next) return;
      curId = next.id;
      mode.arrivedShown = false;
      if (donePrev) UI.toast('已收集：' + cur.name + ' · 前往下一个：' + next.name);
      postTarget(next, '');
      renderCollectBar();
      return;
    }
    // 到达提示：只提示不跳转，等收集（存档同步 / 手动勾选）后再自动前进
    if (mode.arrivedShown && L.tdist != null && L.tdist > ARRIVE_M * 1.5) mode.arrivedShown = false;
    if (L.tdist != null && L.tdist <= ARRIVE_M && !mode.arrivedShown) {
      mode.arrivedShown = true;
      UI.toast('已到达：' + cur.name + ' · 收集后自动前往下一个');
    }
    renderCollectBar();
  }

  function renderCollectBar() {
    const bar = document.getElementById('collectBar');
    if (!bar) return;
    if (!mode) { bar.classList.add('hidden'); return; }
    bar.classList.remove('hidden');
    document.getElementById('collectBarCat').textContent = CAT_CN[mode.cat] || mode.cat;
    const [d, t] = collectCounts(mode.cat);
    document.getElementById('collectBarProg').textContent = '已收集 ' + d + '/' + t;
    const curEl = document.getElementById('collectBarCur');
    bar.classList.remove('arrived', 'paused');
    if (!map.live.online) {
      bar.classList.add('paused');
      curEl.textContent = '离线暂停中…（恢复连接后继续）';
    } else {
      const cur = targetMarker();
      if (mode.arrivedShown && cur && cur.id === curId && !map._mkDone(cur)) {
        bar.classList.add('arrived');
        curEl.textContent = '已到达 · 收集后自动前往下一个';
      } else {
        curEl.textContent = (cur && cur.id === curId) ? '目标：' + cur.name : '正在定位目标…';
      }
    }
    if (UI && UI.syncCollectBtn) UI.syncCollectBtn();
  }

  /* ---------- 画布绘制（挂到 BotwMap.prototype._drawLive） ---------- */
  function drawLive(ctx) {
    const L = this.live;
    if (!L) return;
    const w = this.w, h = this.h;

    // 移动轨迹（虚线淡红）
    if (L.trail.length > 1) {
      ctx.save();
      ctx.lineWidth = 2;
      ctx.strokeStyle = 'rgba(255,90,90,.5)';
      ctx.setLineDash([5, 4]);
      ctx.beginPath();
      let started = false;
      for (const [mx, my] of L.trail) {
        const [sx, sy] = this.m2s([mx, my]);
        if (sx < -50 || sy < -50 || sx > w + 50 || sy > h + 50) continue;
        if (!started) { ctx.moveTo(sx, sy); started = true; }
        else ctx.lineTo(sx, sy);
      }
      ctx.stroke();
      ctx.restore();
    }

    if (L.mx == null) return;
    const [px, py] = this.m2s([L.mx, L.my]);
    if (px < -40 || py < -40 || px > w + 40 || py > h + 40) return;

    // 目标引导线（金色虚线 + 目标环）
    if (L.tgt) {
      const [tx, ty] = this.m2s([L.tgt.x, L.tgt.y]);
      ctx.save();
      ctx.lineWidth = 1.6;
      ctx.setLineDash([7, 5]);
      ctx.strokeStyle = 'rgba(255,183,3,.92)';
      ctx.beginPath(); ctx.moveTo(px, py); ctx.lineTo(tx, ty); ctx.stroke();
      ctx.setLineDash([]);
      ctx.beginPath(); ctx.arc(tx, ty, 6, 0, Math.PI * 2);
      ctx.fillStyle = 'rgba(255,183,3,.28)'; ctx.fill();
      ctx.lineWidth = 2; ctx.stroke();
      ctx.restore();
    }

    // 玩家红点（白描边 + 光晕；verified 后稍大）
    const rad = L.verified ? 7 : 5;
    ctx.save();
    ctx.shadowColor = 'rgba(255,60,60,.85)'; ctx.shadowBlur = 10;
    ctx.fillStyle = '#ff3b3b';
    ctx.beginPath(); ctx.arc(px, py, rad, 0, Math.PI * 2); ctx.fill();
    ctx.shadowBlur = 0;
    ctx.lineWidth = 2;
    ctx.strokeStyle = '#ffffff';
    ctx.beginPath(); ctx.arc(px, py, rad + 2.5, 0, Math.PI * 2); ctx.stroke();
    ctx.restore();

    // 目标距离
    if (L.tgt && L.tdist != null) {
      const txt = Math.round(L.tdist) + 'm';
      ctx.font = '600 12px "PingFang SC","Microsoft YaHei",sans-serif';
      ctx.textAlign = 'left'; ctx.textBaseline = 'bottom';
      ctx.lineWidth = 3; ctx.strokeStyle = 'rgba(8,12,8,.85)';
      ctx.strokeText(txt, px + rad + 6, py - rad - 4);
      ctx.fillStyle = '#ffd166';
      ctx.fillText(txt, px + rad + 6, py - rad - 4);
    }
  }

  if (typeof BotwMap !== 'undefined') {
    BotwMap.prototype._drawLive = drawLive;
  }

  return { init, navigate, navigateToStart, clearNav, centerOnPlayer, startCollectMode, stopCollectMode, isModeActive };
})();

(function () {
  const m = window.__botwMap;
  if (m) LIVE.init(m);
  else {
    // main.js 尚未执行完：等一小帧再挂
    const t = setInterval(() => {
      const mm = window.__botwMap;
      if (mm) { clearInterval(t); LIVE.init(mm); }
    }, 50);
    setTimeout(() => clearInterval(t), 5000);
  }
})();
