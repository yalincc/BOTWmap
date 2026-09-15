/* ============ 旷野之息互动地图 · Live 实时层 ============
 *
 * 对接 live/start.py 服务（同源：本页即由该服务托管）：
 *   GET  /pos     玩家位置 + 目标 + 配置（600ms 轮询）
 *   POST /target  设置 / 清除导航目标
 *   POST /config  把当前图层勾选同步给服务（悬浮窗读取同一配置）
 *   data/progress_points.json  存档收集进度（保存变更后由服务重建）
 *
 * 能力：
 *   1. 玩家位置红点 + 移动轨迹
 *   2. 点击标注 →「导航到这里」→ 金色引导线 + 距离
 *   3. 存档收集进度吸收式合并：存档已收集的标注自动写回本地完成集合（持久化，离线可见）
 *   4. 统计面板统一为一套「收集进度」（存档同步 + 手动标记叠加）
 *   5. 服务离线时全站静默降级，普通地图功能不受影响
 */
'use strict';

const LIVE = (() => {
  let map = null;
  let lastPosKey = null;      // 玩家位置指纹（防无效重绘）
  let lastDrawKey = null;
  let cfgSent = null;         // 已同步给服务的类别签名
  let progLoading = false;
  const K_FOLLOW = 'botwmap.follow.v1';
  let follow = false;
  try { follow = localStorage.getItem(K_FOLLOW) !== '0'; } catch (e) {}

  const CAT_CN = {};                        // 类别 key -> 中文名
  const DONE_TYPES = { shrine: 'shrine', tower: 'tower', seed: 'korok' };

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
      const r = await fetch('/pos?who=web&t=' + Date.now(), { cache: 'no-store' });
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
    } else {
      lastPosKey = null;
    }
    updateStatus();
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
      txt = '● 未连接 · 运行 live/start.py'; cls = 'off';
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
    fetch('/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cats }),
    }).catch(() => { cfgSent = null; });
  }

  /* ---------- 存档进度加载（标点就近匹配 + 计数） ---------- */
  async function loadProgress() {
    if (progLoading) return;
    progLoading = true;
    try {
      const r = await fetch('../data/progress_points.json?t=' + Date.now(), { cache: 'no-store' });
      if (!r.ok) return;
      const j = await r.json();
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
    } catch (e) { /* 离线时静默 */ } finally { progLoading = false; }
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
    fetch('/target', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: mk.name || mk.en || '', x: mk.px[0], y: mk.px[1], type: mk.cat }),
    }).then(r => r.json()).then(() => {
      UI.toast('已设置导航：' + (mk.name || mk.en || '目标'));
    }).catch(() => UI.toast('导航设置失败'));
  }

  function clearNav() {
    fetch('/target', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ clear: true }),
    }).catch(() => {});
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

  return { init, navigate, clearNav, centerOnPlayer };
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
