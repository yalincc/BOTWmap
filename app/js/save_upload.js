/* ============ 存档上传解析（纯前端，不上传服务器） ============
 *
 * 任何人（部署站 / 本地 8766）都可以把自己的 game_data.sav 拖进网页：
 *   FileReader 读取 ArrayBuffer → 解析 12 字节头 + 0x0C 起 [hash u32][value u32]
 *   扁平表 → 对照 data/save_flags.json（flag 哈希 → 标记）→ 收集进度吸收进
 *   localStorage 完成集合（与 live 服务自动同步共用同一基底）。
 *
 * 覆盖：神庙 / 希卡塔 / 克洛格 / 主线回忆(13) / 神兽(4)。
 * 存档文件只在浏览器内存中处理，不离开本机。
 */
'use strict';

const SAVE_UPLOAD = (() => {
  let map = null;
  let flags = null;                 // { "hash": ["shrine", "marker_id"], ... }
  let loaded = false;               // 本会话是否已成功加载过存档

  const H_PLAYTIME = 0x73C29681;    // PlayTime
  const H_KOROK_CNT = 0x8A94E07A;   // HiddenKorok_Number（计数器，仅参考）

  const CAT_CN = { shrine: '神庙', tower: '希卡塔', korok: '克洛格', memory: '回忆', beast: '神兽' };

  function init(m) {
    map = m;
    const input = document.getElementById('saveFile');
    const btn = document.getElementById('btnSaveLoad');
    if (!input || !btn) return;
    input.addEventListener('change', () => {
      const f = input.files && input.files[0];
      if (f) handleFile(f);
      input.value = '';
    });
    btn.addEventListener('click', () => input.click());

    // 存档位置提示：悬停显示小气泡；点击打开完整帮助弹窗（内容可复制）
    const help = document.getElementById('suHelp');
    const tip = document.getElementById('suTip');
    const helpBox = document.getElementById('helpBox');
    if (help && tip) {
      const show = () => tip.classList.remove('hidden');
      const hide = () => tip.classList.add('hidden');
      help.addEventListener('mouseenter', show);
      help.addEventListener('mouseleave', hide);
      help.addEventListener('focus', show);
      help.addEventListener('blur', hide);
      help.addEventListener('click', e => {
        e.stopPropagation();
        hide();
        if (helpBox) helpBox.classList.remove('hidden');
      });
      document.addEventListener('click', e => {
        if (helpBox && !e.target.closest('#helpBox') && !e.target.closest('.save-upload')) {
          helpBox.classList.add('hidden');
        }
        if (!e.target.closest('.save-upload')) hide();
      });
      const hbClose = document.getElementById('hbClose');
      if (hbClose) hbClose.addEventListener('click', () => helpBox.classList.add('hidden'));
      document.addEventListener('keydown', e => {
        if (e.key === 'Escape' && helpBox && !helpBox.classList.contains('hidden')) {
          helpBox.classList.add('hidden');
        }
      });
    }

    // 拖拽：整个侧栏都可以放存档文件
    const zone = document.getElementById('sidebar') || document;
    zone.addEventListener('dragover', e => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; });
    zone.addEventListener('drop', e => {
      e.preventDefault();
      const f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
      if (f) handleFile(f);
    });

    // 查找表：随页面部署（app/data/ 下），多路径兜底（根部署 / app 子目录部署）
    const cands = ['data/save_flags.json?v=' + Date.now(),
                   '../data/save_flags.json?v=' + Date.now(),
                   './data/save_flags.json?v=' + Date.now()];
    (async () => {
      for (const c of cands) {
        try {
          const r = await fetch(c, { cache: 'no-store' });
          if (r.ok) { flags = await r.json(); return; }
        } catch (e) {}
      }
    })();
  }

  function handleFile(file) {
    if (!/\.sav$/i.test(file.name)) {
      UI.toast('请选择 Ryujinx/Cemu 的 game_data.sav 存档文件');
      return;
    }
    if (file.size < 1024) {
      UI.toast('文件太小，可能不是有效的存档');
      return;
    }
    const rd = new FileReader();
    rd.onerror = () => UI.toast('读取存档失败');
    rd.onload = () => {
      try { applySave(rd.result); }
      catch (e) { UI.toast('解析存档失败：' + (e && e.message ? e.message : e)); }
    };
    rd.readAsArrayBuffer(file);
  }

  function applySave(buf) {
    if (!flags) { UI.toast('标志查找表尚未就绪，请稍后重试'); return; }
    const dv = new DataView(buf);
    const done = new Map();         // hash -> type
    let playtime = 0, korokCnt = 0, entries = 0;
    for (let o = 0x0C; o + 8 <= dv.byteLength; o += 8) {
      const h = dv.getUint32(o, true);
      const v = dv.getUint32(o + 4, true);
      entries++;
      if (h === H_PLAYTIME) playtime = v;
      else if (h === H_KOROK_CNT) korokCnt = v;
      else if (v !== 0 && flags[h]) done.set(h, flags[h]);
    }
    if (entries < 100) { UI.toast('存档结构异常（条目过少），请确认是 game_data.sav'); return; }

    // 吸收式合并：写回本地完成集合
    // flag 引用：单值 [t,id] / [t,id,tier]；多值 [[t,id],[t,id,tier],...]（英杰回忆与神兽共用 Clear_Remains*）
    const expandRefs = v => (Array.isArray(v[0]) ? v : [v]);
    let grew = 0;
    const byT = {}, memByTier = {};
    for (const v of done.values()) {
      for (const ref of expandRefs(v)) {
        const t = ref[0], id = ref[1], tier = ref[2];
        byT[t] = (byT[t] || 0) + 1;
        if (t === 'memory') memByTier[tier || '?'] = (memByTier[tier || '?'] || 0) + 1;
        if (id && !map.doneSet.has(id)) { map.doneSet.add(id); grew++; }
      }
    }
    if (grew > 0) {
      UI.persistDone();
      map.draw();
      UI.renderStats();
      UI.updateLayerList();
    }
    loaded = true;
    window.__saveUploaded = true;   // 统计面板「存档已同步 ✓」行
    UI.renderStats();

    // 摘要
    const parts = [];
    for (const k of ['shrine', 'tower', 'korok', 'beast']) {
      if (byT[k]) parts.push(CAT_CN[k] + ' ' + byT[k]);
    }
    if (byT.memory) {
      const mp = [];
      if (memByTier.photo) mp.push('照片 ' + memByTier.photo);
      if (memByTier.auto) mp.push('主线 ' + memByTier.auto);
      if (memByTier.dlc) mp.push('DLC ' + memByTier.dlc);
      parts.push('回忆 ' + (mp.length ? mp.join('/') : byT.memory));
    }
    const hrs = (playtime / 3600).toFixed(1);
    UI.toast('已从存档加载 ✓ ' + parts.join(' · ') + (playtime ? '（游玩 ' + hrs + ' 小时）' : ''));
  }

  return { init, isLoaded: () => loaded };
})();

(function () {
  const m = window.__botwMap;
  if (m) SAVE_UPLOAD.init(m);
  else {
    const t = setInterval(() => {
      const mm = window.__botwMap;
      if (mm) { clearInterval(t); SAVE_UPLOAD.init(mm); }
    }, 50);
    setTimeout(() => clearInterval(t), 5000);
  }
})();
