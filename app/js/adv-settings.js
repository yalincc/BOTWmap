/* ============ 旷野之息互动地图 · 高级设置模块 ============
 * 独立弹窗管理可进阶参数（刷新率、移动端 DPR 上限、视野扇形、导航箭头自定义）
 * 支持实时预览、撤销上一步（Undo，最多 30 步）、恢复默认（Reset，且可撤销）。
 * 全局暴露 window.BOTW_ADV 供 map.js / live.js 读取。
 */
'use strict';

window.BOTW_ADV = (function () {
  const STORAGE_KEY = 'botwmap.adv.v1';
  const MAX_UNDO = 30;

  const DEFAULT_ADV = {
    pcFps: 60,           // 30 | 60 | 90 | 120 | 0 (0 = 跟随显示器)
    mobileFps: 60,       // 30 | 60 | 90 | 120 | 0 (0 = 跟随屏幕)
    mobileDprCap: 2,     // 2 = 流畅 2x（默认防卡） | 0 = 原生不限制
    fov: {
      show: true,
      angle: 76,         // 视野总夹角（度，20~160）
      radius: 82,        // 视野半径（px，30~220）
      color: '#fff8d6',  // 视野色
      alphaStart: 56,    // 近端不透明度（0~100%）
      alphaEnd: 0,       // 远端不透明度（0~100%）
      fadeFrom: 0,       // 淡化起始距离比例（0~80%）
    },
    arrow: {
      shape: 'arrow',    // 'arrow' | 'triangle' | 'compass' | 'dot' | 'custom'
      customImg: '',     // DataURL（最大 128x128 PNG）
      customRot: 0,      // 自定义图朝向修正（0 | 90 | 180 | 270）
      size: 1.0,         // 缩放倍率（0.5 ~ 2.5）
      color: '#ff3b3b',  // 主色
      shadow: true,      // 阴影开关
      shadowBlur: 10,    // 阴影模糊度（0 ~ 24）
      shadowColor: '#ff3232',
    },
  };

  function clone(obj) {
    return JSON.parse(JSON.stringify(obj));
  }

  function clamp(v, lo, hi, fallback) {
    const n = Number(v);
    if (!Number.isFinite(n)) return fallback;
    return Math.min(hi, Math.max(lo, n));
  }

  function mergeDefaults(raw) {
    const d = clone(DEFAULT_ADV);
    if (!raw || typeof raw !== 'object') return d;
    if ([0, 30, 60, 90, 120].includes(Number(raw.pcFps))) d.pcFps = Number(raw.pcFps);
    if ([0, 30, 60, 90, 120].includes(Number(raw.mobileFps))) d.mobileFps = Number(raw.mobileFps);
    if ([0, 1, 2].includes(Number(raw.mobileDprCap))) d.mobileDprCap = Number(raw.mobileDprCap);
    if (raw.fov && typeof raw.fov === 'object') {
      d.fov.show = raw.fov.show !== false;
      d.fov.angle = clamp(raw.fov.angle, 20, 160, d.fov.angle);
      d.fov.radius = clamp(raw.fov.radius, 30, 220, d.fov.radius);
      if (typeof raw.fov.color === 'string' && /^#[0-9a-fA-F]{6}$/.test(raw.fov.color)) d.fov.color = raw.fov.color;
      d.fov.alphaStart = clamp(raw.fov.alphaStart, 0, 100, d.fov.alphaStart);
      d.fov.alphaEnd = clamp(raw.fov.alphaEnd, 0, 100, d.fov.alphaEnd);
      d.fov.fadeFrom = clamp(raw.fov.fadeFrom, 0, 80, d.fov.fadeFrom);
    }
    if (raw.arrow && typeof raw.arrow === 'object') {
      if (['arrow', 'triangle', 'compass', 'dot', 'custom'].includes(raw.arrow.shape)) d.arrow.shape = raw.arrow.shape;
      if (typeof raw.arrow.customImg === 'string' && raw.arrow.customImg.startsWith('data:image/')) {
        d.arrow.customImg = raw.arrow.customImg;
      }
      if ([0, 90, 180, 270].includes(Number(raw.arrow.customRot))) d.arrow.customRot = Number(raw.arrow.customRot);
      d.arrow.size = clamp(raw.arrow.size, 0.5, 2.5, d.arrow.size);
      if (typeof raw.arrow.color === 'string' && /^#[0-9a-fA-F]{6}$/.test(raw.arrow.color)) d.arrow.color = raw.arrow.color;
      d.arrow.shadow = raw.arrow.shadow !== false;
      d.arrow.shadowBlur = clamp(raw.arrow.shadowBlur, 0, 24, d.arrow.shadowBlur);
      if (typeof raw.arrow.shadowColor === 'string' && /^#[0-9a-fA-F]{6}$/.test(raw.arrow.shadowColor)) d.arrow.shadowColor = raw.arrow.shadowColor;
      if (d.arrow.shape === 'custom' && !d.arrow.customImg) d.arrow.shape = 'arrow';
    }
    return d;
  }

  let state = clone(DEFAULT_ADV);
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) state = mergeDefaults(JSON.parse(saved));
  } catch (e) {}

  let customImgEl = null;
  let customImgSrc = '';
  function getCustomImage() {
    const src = state.arrow.customImg;
    if (!src) { customImgEl = null; customImgSrc = ''; return null; }
    if (customImgEl && customImgSrc === src) return customImgEl;
    const img = new Image();
    img.onload = () => {
      renderPreview();
      if (window.__botwMap && window.__botwMap.draw) window.__botwMap.draw({ dynamicOnly: true });
    };
    img.src = src;
    customImgEl = img;
    customImgSrc = src;
    return img;
  }
  getCustomImage();

  function save() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch (e) {}
  }

  function isMobileDevice() {
    if (typeof window === 'undefined') return false;
    const ua = navigator.userAgent || '';
    if (/Android|iPhone|iPad|iPod|Mobile|HarmonyOS/i.test(ua)) return true;
    return window.innerWidth <= 820 && ('ontouchstart' in window || navigator.maxTouchPoints > 0);
  }

  function getFpsCap() {
    return isMobileDevice() ? state.mobileFps : state.pcFps;
  }

  function getEffectiveDpr() {
    const raw = (typeof window !== 'undefined' && window.devicePixelRatio) ? window.devicePixelRatio : 1;
    if (isMobileDevice() && state.mobileDprCap > 0) {
      return Math.min(raw, state.mobileDprCap);
    }
    return raw;
  }

  function hexToRgba(hex, alpha) {
    const h = (hex || '#ffffff').replace('#', '');
    const r = parseInt(h.slice(0, 2), 16) || 0;
    const g = parseInt(h.slice(2, 4), 16) || 0;
    const b = parseInt(h.slice(4, 6), 16) || 0;
    return `rgba(${r},${g},${b},${alpha})`;
  }

  /* 统一绘制「视野扇形 + 导航箭头」（live.js 地图层 & 弹窗实时预览共用，100% 一致） */
  function drawPlayer(ctx, px, py, opts) {
    const verified = !opts || opts.verified !== false;
    const heading = opts ? opts.heading : null;
    const fovAngle = opts ? opts.fovAngle : null;
    const F = state.fov;
    const A = state.arrow;

    // 1) 视野扇形
    if (F.show && typeof fovAngle === 'number') {
      const fovRad = F.radius;
      const halfCone = (F.angle / 2) * Math.PI / 180;
      const a0 = F.alphaStart / 100;
      const a1 = F.alphaEnd / 100;
      const f0 = Math.min(0.85, Math.max(0, F.fadeFrom / 100));
      const midStop = f0 + (1 - f0) * 0.5;
      const aMid = a0 + (a1 - a0) * 0.55;

      ctx.save();
      ctx.translate(px, py);
      ctx.rotate(fovAngle * Math.PI / 180);
      const grad = ctx.createRadialGradient(0, 0, 2, 0, 0, fovRad);
      grad.addColorStop(0, hexToRgba(F.color, a0));
      if (f0 > 0.02) grad.addColorStop(f0, hexToRgba(F.color, a0));
      grad.addColorStop(midStop, hexToRgba(F.color, aMid));
      grad.addColorStop(1, hexToRgba(F.color, a1));
      ctx.fillStyle = grad;
      ctx.beginPath();
      ctx.moveTo(0, 0);
      ctx.arc(0, 0, fovRad, -halfCone, halfCone);
      ctx.closePath();
      ctx.fill();
      ctx.restore();
    }

    // 2) 导航箭头 / 自定义图标
    const baseRad = (verified ? 8 : 6) * A.size;
    const baseDot = (verified ? 7 : 5) * A.size;
    ctx.save();
    if (A.shadow && A.shadowBlur > 0) {
      ctx.shadowColor = hexToRgba(A.shadowColor, 0.85);
      ctx.shadowBlur = A.shadowBlur;
    }

    const customEl = (A.shape === 'custom') ? getCustomImage() : null;
    if (A.shape === 'custom' && customEl && customEl.complete && customEl.naturalWidth > 0) {
      const sz = Math.round(24 * A.size);
      const rot = ((typeof heading === 'number' ? heading : -90) + A.customRot) * Math.PI / 180;
      ctx.translate(px, py);
      ctx.rotate(rot);
      ctx.drawImage(customEl, -sz / 2, -sz / 2, sz, sz);
    } else if (A.shape === 'dot' || typeof heading !== 'number') {
      ctx.fillStyle = A.color;
      ctx.beginPath();
      ctx.arc(px, py, baseDot, 0, Math.PI * 2);
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.lineWidth = Math.max(1.5, 2 * A.size);
      ctx.strokeStyle = '#ffffff';
      ctx.beginPath();
      ctx.arc(px, py, baseDot + 2.2 * A.size, 0, Math.PI * 2);
      ctx.stroke();
    } else {
      ctx.translate(px, py);
      ctx.rotate(heading * Math.PI / 180);
      ctx.beginPath();
      if (A.shape === 'triangle') {
        ctx.moveTo(baseRad * 1.55, 0);
        ctx.lineTo(-baseRad * 0.95, baseRad * 0.92);
        ctx.lineTo(-baseRad * 0.95, -baseRad * 0.92);
      } else if (A.shape === 'compass') {
        ctx.moveTo(baseRad * 1.65, 0);
        ctx.lineTo(0, baseRad * 0.72);
        ctx.lineTo(-baseRad * 1.15, 0);
        ctx.lineTo(0, -baseRad * 0.72);
      } else {
        // 默认尖箭头（arrow）
        ctx.moveTo(baseRad * 1.5, 0);
        ctx.lineTo(-baseRad * 0.9, baseRad * 0.85);
        ctx.lineTo(-baseRad * 0.4, 0);
        ctx.lineTo(-baseRad * 0.9, -baseRad * 0.85);
      }
      ctx.closePath();
      ctx.fillStyle = A.color;
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.lineWidth = Math.max(1.4, 1.8 * Math.min(1.4, A.size));
      ctx.strokeStyle = '#ffffff';
      ctx.stroke();
    }
    ctx.restore();
    return baseRad;
  }

  /* ---------- Undo 历史栈 ---------- */
  const undoStack = [];
  let dragSnapshot = null;

  function pushUndo(snap) {
    const s = snap || clone(state);
    const str = JSON.stringify(s);
    if (undoStack.length && JSON.stringify(undoStack[undoStack.length - 1]) === str) return;
    undoStack.push(s);
    if (undoStack.length > MAX_UNDO) undoStack.shift();
    updateUndoBtn();
  }

  function applyChange(needResize) {
    save();
    syncModalUI();
    renderPreview();
    if (window.__botwMap) {
      if (needResize && typeof window.__botwMap._resize === 'function') {
        window.__botwMap._resize();
      } else if (typeof window.__botwMap.draw === 'function') {
        window.__botwMap.draw({ dynamicOnly: true });
      }
    }
  }

  function undo() {
    if (!undoStack.length) return;
    const prevDpr = state.mobileDprCap;
    state = mergeDefaults(undoStack.pop());
    getCustomImage();
    applyChange(prevDpr !== state.mobileDprCap);
    updateUndoBtn();
    if (window.UI && window.UI.toast) window.UI.toast('已撤销上一步高级设置');
  }

  function resetToDefault() {
    const rsApi = window.__botwRegionStyleApi;
    const m = window.__botwMap;
    const regionChanged = !!(rsApi && m && m.regionStyle && JSON.stringify(m.regionStyle) !== JSON.stringify(rsApi.DEFAULT_STYLE));
    if (JSON.stringify(state) === JSON.stringify(DEFAULT_ADV) && !regionChanged) return;
    const prevDpr = state.mobileDprCap;
    pushUndo();
    state = clone(DEFAULT_ADV);
    getCustomImage();
    if (rsApi && m) {
      m.regionStyle = Object.assign({}, rsApi.DEFAULT_STYLE, {
        tx: rsApi.DEFAULT_STYLE.tx.slice(),
        bg: rsApi.DEFAULT_STYLE.bg.slice(),
        sc: rsApi.DEFAULT_STYLE.sc ? rsApi.DEFAULT_STYLE.sc.slice() : [63, 48, 16]
      });
      rsApi.saveStyle();
      if (m.draw) m.draw();
    }
    applyChange(prevDpr !== state.mobileDprCap);
    if (window.UI && window.UI.toast) window.UI.toast('高级设置已恢复默认（可点击撤销还原）');
  }

  /* ---------- 屏幕刷新率简测 ---------- */
  let measuredHz = 0;
  function measureDisplayHz(cb) {
    let frames = 0, t0 = 0;
    function step(now) {
      if (!t0) t0 = now;
      frames++;
      if (now - t0 >= 280) {
        measuredHz = Math.round((frames - 1) * 1000 / (now - t0));
        if (cb) cb(measuredHz);
        return;
      }
      requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
  }

  /* ---------- 弹窗 DOM 与预览 ---------- */
  let modalEl = null;
  let previewCanvas = null;

  function renderPreview() {
    if (!previewCanvas) return;
    const ctx = previewCanvas.getContext('2d');
    const w = 320, h = 150;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    if (previewCanvas.width !== w * dpr || previewCanvas.height !== h * dpr) {
      previewCanvas.width = w * dpr;
      previewCanvas.height = h * dpr;
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    // 深色地图网格背景
    ctx.fillStyle = '#131b14';
    ctx.fillRect(0, 0, w, h);
    ctx.strokeStyle = 'rgba(230, 205, 143, 0.08)';
    ctx.lineWidth = 1;
    for (let x = 20; x < w; x += 24) {
      ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, h); ctx.stroke();
    }
    for (let y = 20; y < h; y += 24) {
      ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
    }

    // 模拟玩家在偏左位置朝右上（-30°）
    drawPlayer(ctx, 118, 92, { verified: true, heading: -30, fovAngle: -30 });

    // 右下角状态提示
    ctx.fillStyle = 'rgba(232,240,228,.55)';
    ctx.font = '11px Consolas, monospace';
    ctx.textAlign = 'right';
    ctx.textBaseline = 'bottom';
    const fps = getFpsCap();
    const fpsTxt = fps ? `${fps}Hz` : (measuredHz ? `随系统(${measuredHz}Hz)` : '随系统');
    ctx.fillText(`预览 · ${isMobileDevice() ? '手机' : 'PC'} ${fpsTxt} · DPR ${getEffectiveDpr().toFixed(1)}`, w - 8, h - 6);
  }

  function updateUndoBtn() {
    if (!modalEl) return;
    const btn = modalEl.querySelector('#advUndo');
    if (!btn) return;
    btn.disabled = undoStack.length === 0;
    btn.textContent = undoStack.length ? `撤销 (${undoStack.length})` : '撤销';
  }

  function setGroupActive(groupId, val) {
    if (!modalEl) return;
    const wrap = modalEl.querySelector('#' + groupId);
    if (!wrap) return;
    let matched = false;
    wrap.querySelectorAll('.st-opt[data-v]').forEach(b => {
      const hit = String(b.dataset.v) === String(val);
      if (hit) matched = true;
      b.classList.toggle('sel', hit);
    });
    const picker = wrap.querySelector('.st-picker');
    if (picker) picker.classList.toggle('sel', !matched);
    const upload = wrap.querySelector('.adv-upload-btn');
    if (upload) upload.classList.toggle('sel', String(val) === 'custom');
  }

  function setRange(id, val, fmt) {
    if (!modalEl) return;
    const el = modalEl.querySelector('#' + id);
    const out = modalEl.querySelector('#' + id + 'V');
    if (el) el.value = val;
    if (out) out.textContent = fmt ? fmt(val) : String(val);
  }

  function syncModalUI() {
    if (!modalEl) return;
    setGroupActive('advPcFps', state.pcFps);
    setGroupActive('advMobFps', state.mobileFps);
    setGroupActive('advMobDpr', state.mobileDprCap);

    setGroupActive('advFovShow', state.fov.show ? '1' : '0');
    setRange('advFovAngle', state.fov.angle, v => v + '°');
    setRange('advFovRadius', state.fov.radius, v => v + 'px');
    setRange('advFovAlphaStart', state.fov.alphaStart, v => v + '%');
    setRange('advFovAlphaEnd', state.fov.alphaEnd, v => v + '%');
    setRange('advFovFadeFrom', state.fov.fadeFrom, v => v + '%');
    const fc = modalEl.querySelector('#advFovColor');
    if (fc) fc.value = state.fov.color;
    setGroupActive('advFovColorPresets', state.fov.color.toLowerCase());

    setGroupActive('advArrowShape', state.arrow.shape);
    setGroupActive('advArrowRot', state.arrow.customRot);
    setRange('advArrowSize', state.arrow.size, v => Number(v).toFixed(1) + 'x');
    const ac = modalEl.querySelector('#advArrowColor');
    if (ac) ac.value = state.arrow.color;
    setGroupActive('advArrowColorPresets', state.arrow.color.toLowerCase());

    setGroupActive('advArrowShadow', state.arrow.shadow ? '1' : '0');
    setRange('advArrowShadowBlur', state.arrow.shadowBlur, v => v + 'px');
    const sc = modalEl.querySelector('#advArrowShadowColor');
    if (sc) sc.value = state.arrow.shadowColor;

    const customWrap = modalEl.querySelector('#advCustomImgRow');
    if (customWrap) customWrap.classList.toggle('hidden', state.arrow.shape !== 'custom' && !state.arrow.customImg);
    const clearBtn = modalEl.querySelector('#advClearCustomImg');
    if (clearBtn) clearBtn.classList.toggle('hidden', !state.arrow.customImg);

    const hzHint = modalEl.querySelector('#advHzHint');
    if (hzHint) {
      hzHint.textContent = measuredHz
        ? `${isMobileDevice() ? '手机端' : 'PC端'} · 屏幕约 ${measuredHz}Hz`
        : `${isMobileDevice() ? '手机端' : 'PC端'}`;
    }
    syncRegionStyleUI();
    updateUndoBtn();
  }

  function syncRegionStyleUI() {
    if (!modalEl) return;
    const m = window.__botwMap;
    const api = window.__botwRegionStyleApi;
    if (!m || !m.regionStyle || !api) return;
    const st = m.regionStyle;
    const txHex = api.rgbToHex(st.tx);
    const defTxHex = api.rgbToHex(api.DEFAULT_STYLE.tx);
    const txVal = (txHex === defTxHex) ? 'default' : (txHex === '#000000' || txHex === '#ffffff' ? txHex : '__custom');
    setGroupActive('advRegTx', txVal);
    const txIn = modalEl.querySelector('#advRegTxColor');
    if (txIn) txIn.value = txHex;
    setRange('advRegTxAlpha', Math.round((st.txA != null ? st.txA : 1) * 100), v => v + '%');

    let sz = 'medium';
    for (const k in api.FONT_SIZES) if (Math.abs(api.FONT_SIZES[k] - (st.fsz || 1)) < 0.01) sz = k;
    setGroupActive('advRegSize', sz);

    const scHex = st.sc ? api.rgbToHex(st.sc) : '';
    const scVal = (!st.sw || st.sw <= 0 || !scHex) ? 'none' : (scHex === '#000000' || scHex === '#ffffff' ? scHex : '__custom');
    setGroupActive('advRegStroke', scVal);
    const scIn = modalEl.querySelector('#advRegStrokeColor');
    if (scIn && scHex) scIn.value = scHex;
    setRange('advRegStrokeW', st.sw || 0, v => (Number(v) % 1 === 0 ? String(Number(v)) : Number(v).toFixed(1)));

    const bgHex = api.rgbToHex(st.bg);
    const defBgHex = api.rgbToHex(api.DEFAULT_STYLE.bg);
    const bgVal = (bgHex === defBgHex) ? 'default' : (bgHex === '#000000' || bgHex === '#ffffff' ? bgHex : '__custom');
    setGroupActive('advRegBg', bgVal);
    const bgIn = modalEl.querySelector('#advRegBgColor');
    if (bgIn) bgIn.value = bgHex;
    setRange('advRegBgAlpha', Math.round((st.bgA || 0) * 100), v => v + '%');
  }

  function applyRegionStyle(fn) {
    const m = window.__botwMap;
    const api = window.__botwRegionStyleApi;
    if (!m || !m.regionStyle || !api) return;
    fn(m.regionStyle, api);
    api.saveStyle();
    if (m.draw) m.draw();
    syncRegionStyleUI();
  }

  function buildModalHTML() {
    return `
      <div class="adv-dialog" role="dialog" aria-modal="true" aria-label="更多高级设置">
        <div class="adv-head">
          <div class="adv-title-wrap">
            <span class="adv-title">高级设置</span>
            <span class="adv-sub" id="advHzHint"></span>
          </div>
          <button type="button" id="advClose" class="card-close" title="关闭">×</button>
        </div>

        <div class="adv-preview-bar">
          <canvas id="advPreviewCanvas" class="adv-preview-canvas"></canvas>
        </div>

        <div class="adv-body">
          <!-- 1. 刷新率与清晰度（打开时默认展开） -->
          <div class="adv-sec">
            <button type="button" class="adv-sec-title"><span>刷新率与画质</span><span class="adv-sec-tri">▾</span></button>
            <div class="adv-sec-body">
            <div class="adv-item">
              <div class="adv-item-label">PC地图刷新率 <span class="adv-def">默认 60Hz</span></div>
              <div class="adv-opts" id="advPcFps">
                <button type="button" class="st-opt" data-v="30">30Hz</button>
                <button type="button" class="st-opt" data-v="60">60Hz</button>
                <button type="button" class="st-opt" data-v="90">90Hz</button>
                <button type="button" class="st-opt" data-v="120">120Hz</button>
                <button type="button" class="st-opt" data-v="0">随系统</button>
              </div>
            </div>
            <div class="adv-item">
              <div class="adv-item-label">手机地图刷新率 <span class="adv-def">默认 60Hz</span></div>
              <div class="adv-opts" id="advMobFps">
                <button type="button" class="st-opt" data-v="30">30Hz</button>
                <button type="button" class="st-opt" data-v="60">60Hz</button>
                <button type="button" class="st-opt" data-v="90">90Hz</button>
                <button type="button" class="st-opt" data-v="120">120Hz</button>
                <button type="button" class="st-opt" data-v="0">随系统</button>
              </div>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">手机清晰度</span>
              <div class="adv-opts adv-opts-inline" id="advMobDpr">
                <button type="button" class="st-opt" data-v="1">1x</button>
                <button type="button" class="st-opt" data-v="2">2x</button>
                <button type="button" class="st-opt" data-v="0">原生</button>
              </div>
            </div>
            </div>
          </div>

          <!-- 2. 视野设置（默认折叠） -->
          <div class="adv-sec collapsed">
            <button type="button" class="adv-sec-title"><span>视野设置</span><span class="adv-sec-tri">▸</span></button>
            <div class="adv-sec-body">
            <div class="adv-row-inline">
              <span class="adv-rl">视野显示</span>
              <div class="adv-opts adv-opts-inline" id="advFovShow">
                <button type="button" class="st-opt" data-v="1">显示</button>
                <button type="button" class="st-opt" data-v="0">隐藏</button>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">视野角度</span>
              <input type="range" id="advFovAngle" min="20" max="160" step="2" value="76">
              <span id="advFovAngleV" class="st-sval">76°</span>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">视野距离</span>
              <input type="range" id="advFovRadius" min="30" max="220" step="2" value="82">
              <span id="advFovRadiusV" class="st-sval">82px</span>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">近端透明</span>
              <input type="range" id="advFovAlphaStart" min="0" max="100" step="1" value="56">
              <span id="advFovAlphaStartV" class="st-sval">56%</span>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">远端透明</span>
              <input type="range" id="advFovAlphaEnd" min="0" max="100" step="1" value="0">
              <span id="advFovAlphaEndV" class="st-sval">0%</span>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">淡化起点</span>
              <input type="range" id="advFovFadeFrom" min="0" max="80" step="2" value="0">
              <span id="advFovFadeFromV" class="st-sval">0%</span>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">视野颜色</span>
              <div class="adv-opts adv-opts-inline" id="advFovColorPresets">
                <button type="button" class="st-opt" data-v="#fff8d6">暖白</button>
                <button type="button" class="st-opt" data-v="#4cd4f2">蓝</button>
                <button type="button" class="st-opt" data-v="#9adc5c">绿</button>
                <button type="button" class="st-opt" data-v="#ffd166">金</button>
                <label class="st-opt st-picker"><span>自选</span><input type="color" id="advFovColor" value="#fff8d6"></label>
              </div>
            </div>
            </div>
          </div>

          <!-- 3. 导航箭头（默认折叠） -->
          <div class="adv-sec collapsed">
            <button type="button" class="adv-sec-title"><span>导航箭头</span><span class="adv-sec-tri">▸</span></button>
            <div class="adv-sec-body">
            <div class="adv-row-inline">
              <span class="adv-rl">图标样式</span>
              <div class="adv-opts adv-opts-inline" id="advArrowShape">
                <button type="button" class="st-opt" data-v="arrow">箭头</button>
                <button type="button" class="st-opt" data-v="triangle">三角</button>
                <button type="button" class="st-opt" data-v="compass">罗盘</button>
                <button type="button" class="st-opt" data-v="dot">圆点</button>
                <label class="st-opt adv-upload-btn"><span>上传</span><input type="file" id="advArrowUpload" accept="image/*" style="display:none"></label>
              </div>
            </div>
            <div class="adv-row-inline hidden" id="advCustomImgRow">
              <span class="adv-rl">图片朝向</span>
              <div class="adv-opts adv-opts-inline" id="advArrowRot">
                <button type="button" class="st-opt" data-v="0">0°</button>
                <button type="button" class="st-opt" data-v="90">90°</button>
                <button type="button" class="st-opt" data-v="180">180°</button>
                <button type="button" class="st-opt" data-v="270">270°</button>
                <button type="button" class="st-opt adv-danger-opt hidden" id="advClearCustomImg">清除</button>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">图标大小</span>
              <input type="range" id="advArrowSize" min="0.5" max="2.5" step="0.1" value="1.0">
              <span id="advArrowSizeV" class="st-sval">1.0x</span>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">图标颜色</span>
              <div class="adv-opts adv-opts-inline" id="advArrowColorPresets">
                <button type="button" class="st-opt" data-v="#ff3b3b">红</button>
                <button type="button" class="st-opt" data-v="#4cd4f2">蓝</button>
                <button type="button" class="st-opt" data-v="#f2c94c">金</button>
                <button type="button" class="st-opt" data-v="#4ade80">绿</button>
                <label class="st-opt st-picker"><span>自选</span><input type="color" id="advArrowColor" value="#ff3b3b"></label>
              </div>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">图标阴影</span>
              <div class="adv-opts adv-opts-inline" id="advArrowShadow">
                <button type="button" class="st-opt" data-v="1">开</button>
                <button type="button" class="st-opt" data-v="0">关</button>
                <label class="st-opt st-picker"><span>颜色</span><input type="color" id="advArrowShadowColor" value="#ff3232"></label>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">阴影范围</span>
              <input type="range" id="advArrowShadowBlur" min="0" max="24" step="1" value="10">
              <span id="advArrowShadowBlurV" class="st-sval">10px</span>
            </div>
            </div>
          </div>

          <!-- 4. 地名文字样式（从主设置迁入，默认折叠） -->
          <div class="adv-sec collapsed">
            <button type="button" class="adv-sec-title"><span>地名文字样式</span><span class="adv-sec-tri">▸</span></button>
            <div class="adv-sec-body">
            <div class="adv-row-inline">
              <span class="adv-rl">文字颜色</span>
              <div class="adv-opts adv-opts-inline" id="advRegTx">
                <button type="button" class="st-opt" data-v="default">金褐</button>
                <button type="button" class="st-opt" data-v="#000000">黑</button>
                <button type="button" class="st-opt" data-v="#ffffff">白</button>
                <label class="st-opt st-picker"><span>自选</span><input type="color" id="advRegTxColor" value="#bfae68"></label>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">文字透明</span>
              <input type="range" id="advRegTxAlpha" min="0" max="100" step="1" value="100">
              <span id="advRegTxAlphaV" class="st-sval">100%</span>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">文字大小</span>
              <div class="adv-opts adv-opts-inline" id="advRegSize">
                <button type="button" class="st-opt" data-v="small">小</button>
                <button type="button" class="st-opt" data-v="medium">中</button>
                <button type="button" class="st-opt" data-v="large">大</button>
              </div>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">文字描边</span>
              <div class="adv-opts adv-opts-inline" id="advRegStroke">
                <button type="button" class="st-opt" data-v="none">无</button>
                <button type="button" class="st-opt" data-v="#000000">黑</button>
                <button type="button" class="st-opt" data-v="#ffffff">白</button>
                <label class="st-opt st-picker"><span>自选</span><input type="color" id="advRegStrokeColor" value="#3f3010"></label>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">描边粗细</span>
              <input type="range" id="advRegStrokeW" min="0" max="6" step="0.5" value="3">
              <span id="advRegStrokeWV" class="st-sval">3</span>
            </div>
            <div class="adv-row-inline">
              <span class="adv-rl">地名底色</span>
              <div class="adv-opts adv-opts-inline" id="advRegBg">
                <button type="button" class="st-opt" data-v="default">无底</button>
                <button type="button" class="st-opt" data-v="#000000">黑</button>
                <button type="button" class="st-opt" data-v="#ffffff">白</button>
                <label class="st-opt st-picker"><span>自选</span><input type="color" id="advRegBgColor" value="#000000"></label>
              </div>
            </div>
            <div class="st-row adv-slider-row">
              <span class="adv-rl">底色透明</span>
              <input type="range" id="advRegBgAlpha" min="0" max="100" step="1" value="0">
              <span id="advRegBgAlphaV" class="st-sval">0%</span>
            </div>
            </div>
          </div>
        </div>

        <div class="adv-foot">
          <div class="adv-foot-left">
            <button type="button" id="advUndo" class="mini" disabled>撤销</button>
            <button type="button" id="advReset" class="mini">重置</button>
          </div>
          <button type="button" id="advDone" class="mini adv-primary">完成</button>
        </div>
      </div>
    `;
  }

  function bindGroup(id, setter, needResize) {
    const wrap = modalEl.querySelector('#' + id);
    if (!wrap) return;
    wrap.querySelectorAll('.st-opt[data-v]').forEach(btn => {
      btn.addEventListener('click', () => {
        pushUndo();
        setter(btn.dataset.v);
        applyChange(!!needResize);
      });
    });
  }

  function bindRange(id, setter) {
    const el = modalEl.querySelector('#' + id);
    if (!el) return;
    el.addEventListener('pointerdown', () => { dragSnapshot = clone(state); });
    el.addEventListener('input', () => {
      if (!dragSnapshot) dragSnapshot = clone(state);
      setter(Number(el.value));
      applyChange(false);
    });
    el.addEventListener('change', () => {
      if (dragSnapshot) {
        pushUndo(dragSnapshot);
        dragSnapshot = null;
      }
    });
  }

  function bindColor(id, setter) {
    const el = modalEl.querySelector('#' + id);
    if (!el) return;
    el.addEventListener('focus', () => { dragSnapshot = clone(state); });
    el.addEventListener('input', () => {
      if (!dragSnapshot) dragSnapshot = clone(state);
      setter(el.value);
      applyChange(false);
    });
    el.addEventListener('change', () => {
      if (dragSnapshot) {
        pushUndo(dragSnapshot);
        dragSnapshot = null;
      }
    });
  }

  function handleCustomUpload(file) {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      const img = new Image();
      img.onload = () => {
        // 缩放到最长边 <= 128px，防止撑爆 localStorage
        const maxSide = 128;
        const scale = Math.min(1, maxSide / Math.max(img.naturalWidth || 1, img.naturalHeight || 1));
        const cw = Math.max(16, Math.round(img.naturalWidth * scale));
        const ch = Math.max(16, Math.round(img.naturalHeight * scale));
        const c = document.createElement('canvas');
        c.width = cw; c.height = ch;
        c.getContext('2d').drawImage(img, 0, 0, cw, ch);
        const dataUrl = c.toDataURL('image/png');
        pushUndo();
        state.arrow.customImg = dataUrl;
        state.arrow.shape = 'custom';
        getCustomImage();
        applyChange(false);
        if (window.UI && window.UI.toast) window.UI.toast('已应用自定义导航图标');
      };
      img.src = reader.result;
    };
    reader.readAsDataURL(file);
  }

  function ensureModal() {
    if (modalEl) return;
    modalEl = document.getElementById('advModal');
    if (!modalEl) {
      modalEl = document.createElement('div');
      modalEl.id = 'advModal';
      modalEl.className = 'adv-overlay hidden';
      document.body.appendChild(modalEl);
    }
    modalEl.innerHTML = buildModalHTML();
    previewCanvas = modalEl.querySelector('#advPreviewCanvas');

    const closeModal = () => modalEl.classList.add('hidden');
    modalEl.querySelector('#advClose').addEventListener('click', closeModal);
    modalEl.querySelector('#advDone').addEventListener('click', closeModal);
    modalEl.addEventListener('click', e => { if (e.target === modalEl) closeModal(); });
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape' && modalEl && !modalEl.classList.contains('hidden')) closeModal();
    });

    modalEl.querySelector('#advUndo').addEventListener('click', undo);
    modalEl.querySelector('#advReset').addEventListener('click', resetToDefault);

    // 折叠/展开各大分类区块
    modalEl.querySelectorAll('.adv-sec-title').forEach(btn => {
      btn.addEventListener('click', () => {
        const sec = btn.closest('.adv-sec');
        if (!sec) return;
        const collapsed = sec.classList.toggle('collapsed');
        const tri = btn.querySelector('.adv-sec-tri');
        if (tri) tri.textContent = collapsed ? '▸' : '▾';
      });
    });

    bindGroup('advPcFps', v => { state.pcFps = Number(v); });
    bindGroup('advMobFps', v => { state.mobileFps = Number(v); });
    bindGroup('advMobDpr', v => { state.mobileDprCap = Number(v); }, true);

    bindGroup('advFovShow', v => { state.fov.show = (v === '1'); });
    bindRange('advFovAngle', v => { state.fov.angle = v; });
    bindRange('advFovRadius', v => { state.fov.radius = v; });
    bindRange('advFovAlphaStart', v => { state.fov.alphaStart = v; });
    bindRange('advFovAlphaEnd', v => { state.fov.alphaEnd = v; });
    bindRange('advFovFadeFrom', v => { state.fov.fadeFrom = v; });
    bindGroup('advFovColorPresets', v => { state.fov.color = v; });
    bindColor('advFovColor', v => { state.fov.color = v; });

    bindGroup('advArrowShape', v => { state.arrow.shape = v; });
    bindGroup('advArrowRot', v => { state.arrow.customRot = Number(v); });
    bindRange('advArrowSize', v => { state.arrow.size = v; });
    bindGroup('advArrowColorPresets', v => { state.arrow.color = v; });
    bindColor('advArrowColor', v => { state.arrow.color = v; });
    bindGroup('advArrowShadow', v => { state.arrow.shadow = (v === '1'); });
    bindRange('advArrowShadowBlur', v => { state.arrow.shadowBlur = v; });
    bindColor('advArrowShadowColor', v => { state.arrow.shadowColor = v; });

    const up = modalEl.querySelector('#advArrowUpload');
    if (up) {
      up.addEventListener('change', () => {
        if (up.files && up.files[0]) handleCustomUpload(up.files[0]);
        up.value = '';
      });
    }
    const clr = modalEl.querySelector('#advClearCustomImg');
    if (clr) {
      clr.addEventListener('click', () => {
        pushUndo();
        state.arrow.customImg = '';
        if (state.arrow.shape === 'custom') state.arrow.shape = 'arrow';
        getCustomImage();
        applyChange(false);
      });
    }

    // 地名文字样式事件绑定（复用 ui.js 持久化接口）
    modalEl.querySelectorAll('#advRegTx .st-opt[data-v]').forEach(b => {
      b.addEventListener('click', () => applyRegionStyle((st, api) => {
        if (b.dataset.v === 'default') st.tx = api.DEFAULT_STYLE.tx.slice();
        else { const c = api.hexToRgb(b.dataset.v); if (c) st.tx = c; }
      }));
    });
    const rTxC = modalEl.querySelector('#advRegTxColor');
    if (rTxC) rTxC.addEventListener('input', () => applyRegionStyle((st, api) => { const c = api.hexToRgb(rTxC.value); if (c) st.tx = c; }));
    const rTxA = modalEl.querySelector('#advRegTxAlpha');
    if (rTxA) rTxA.addEventListener('input', () => applyRegionStyle(st => { st.txA = Math.min(1, Math.max(0, Number(rTxA.value) / 100)); }));
    modalEl.querySelectorAll('#advRegSize .st-opt[data-v]').forEach(b => {
      b.addEventListener('click', () => applyRegionStyle((st, api) => { st.fsz = api.FONT_SIZES[b.dataset.v] || 1; }));
    });
    modalEl.querySelectorAll('#advRegStroke .st-opt[data-v]').forEach(b => {
      b.addEventListener('click', () => applyRegionStyle((st, api) => {
        if (b.dataset.v === 'none') st.sw = 0;
        else { const c = api.hexToRgb(b.dataset.v); if (c) { st.sc = c; st.sw = st.sw || 2; } }
      }));
    });
    const rScC = modalEl.querySelector('#advRegStrokeColor');
    if (rScC) rScC.addEventListener('input', () => applyRegionStyle((st, api) => { const c = api.hexToRgb(rScC.value); if (c) { st.sc = c; st.sw = st.sw || 2; } }));
    const rScW = modalEl.querySelector('#advRegStrokeW');
    if (rScW) rScW.addEventListener('input', () => applyRegionStyle(st => { st.sw = Math.max(0, Math.min(6, parseFloat(rScW.value) || 0)); }));
    modalEl.querySelectorAll('#advRegBg .st-opt[data-v]').forEach(b => {
      b.addEventListener('click', () => applyRegionStyle((st, api) => {
        if (b.dataset.v === 'default') st.bg = api.DEFAULT_STYLE.bg.slice();
        else { const c = api.hexToRgb(b.dataset.v); if (c) st.bg = c; }
      }));
    });
    const rBgC = modalEl.querySelector('#advRegBgColor');
    if (rBgC) rBgC.addEventListener('input', () => applyRegionStyle((st, api) => { const c = api.hexToRgb(rBgC.value); if (c) st.bg = c; }));
    const rBgA = modalEl.querySelector('#advRegBgAlpha');
    if (rBgA) rBgA.addEventListener('input', () => applyRegionStyle(st => { st.bgA = Math.min(1, Math.max(0, Number(rBgA.value) / 100)); }));
  }

  function open() {
    ensureModal();
    modalEl.classList.remove('hidden');
    syncModalUI();
    renderPreview();
    measureDisplayHz(() => { syncModalUI(); renderPreview(); });
  }

  function bindEntryButton() {
    const btn = document.getElementById('btnAdvSettings');
    if (btn && !btn._advBound) {
      btn._advBound = true;
      btn.addEventListener('click', open);
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', bindEntryButton);
  } else {
    bindEntryButton();
  }

  return {
    get s() { return state; },
    D: DEFAULT_ADV,
    open,
    undo,
    reset: resetToDefault,
    getFpsCap,
    getEffectiveDpr,
    isMobileDevice,
    drawPlayer,
  };
})();
