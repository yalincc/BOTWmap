/* ============ 存档上传解析（纯前端，不上传服务器） ============
 *
 * 任何人（部署站 / 本地 8766）都可以把自己的存档拖进网页：
 *   FileReader 读取 ArrayBuffer → 解析 12 字节头 + 0x0C 起 [hash u32][value u32]
 *   扁平表 → 对照 data/save_flags.json（flag 哈希 → 标记）→ 收集进度吸收进
 *   localStorage 完成集合（与 live 服务自动同步共用同一基底）。
 *
 * 双平台（v1.4.0）：
 *   Switch（Ryujinx / game_data.sav）：小端（little endian）
 *   Wii U（Cemu / 同样叫 game_data.sav）：大端（big endian）—— 与 Switch 同一扁平结构、
 *   flag 哈希一致（crc32 of flag name），仅字节序不同；按头部版本字段自动识别。
 *   注：真实目录里 Cemu 的存档文件也叫 game_data.sav（位于 mlc01\usr\save\00050000\101c9300\user\80000001\<槽位>\），
 *   与 Switch 区分靠版本字段字节序，文件名不可靠。
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

  // 存档格式版本号（Switch/Wii U 共用同一版本表，仅字节序相反）
  const VALID_VERSIONS = [0x24E2, 0x24EE, 0x2588, 0x29C0, 0x2A46, 0x3EF8, 0x3EF9, 0x471A, 0x471B, 0x471E];

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

    // 清空本地探索度记录（v1.4.2）：清除 localStorage 累计的完成标记（历史存档累计与手动勾选），
    // 自定义标记与在线存档同步不受影响；navi 在线时面板仍以存档权威 counts 为准
    const clearBtn = document.getElementById('btnClearDone');
    if (clearBtn) {
      // 悬停提示（v1.4.2）：原生 title 在部分浏览器不显示，用自定义气泡替代
      const clearTip = document.getElementById('suClearTip');
      if (clearTip) {
        clearBtn.addEventListener('mouseenter', () => clearTip.classList.remove('hidden'));
        clearBtn.addEventListener('mouseleave', () => clearTip.classList.add('hidden'));
        clearBtn.addEventListener('focus', () => clearTip.classList.remove('hidden'));
        clearBtn.addEventListener('blur', () => clearTip.classList.add('hidden'));
      }
      clearBtn.addEventListener('click', () => {
        if (!confirm('确定清空本地探索度记录？\n将清除本机保存的完成标记（历史存档累计与手动勾选）。\n自定义标记与在线存档同步不受影响。')) return;
        map.doneSet = new Set();
        try { localStorage.removeItem('botwmap.done.v1'); } catch (e) {}
        map.draw();
        UI.renderStats();
        UI.updateLayerList();
        UI.toast('已清空本地探索度记录');
      });
    }

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
      UI.toast('请选择 Ryujinx / Cemu 的 game_data.sav 存档文件');
      return;
    }
    if (file.size < 1024) {
      UI.toast('文件太小，可能不是有效的存档');
      return;
    }
    const rd = new FileReader();
    rd.onerror = () => UI.toast('读取存档失败');
    rd.onload = () => {
      try { applySave(rd.result, file.name); }
      catch (e) { UI.toast('解析存档失败：' + (e && e.message ? e.message : e)); }
    };
    rd.readAsArrayBuffer(file);
  }

  /* 字节序识别（v1.4.0）：按头部版本字段判定，Switch=小端 / Wii U(Cemu)=大端。
   * 两个平台的存档文件都叫 game_data.sav，文件名无法区分（真实 Cemu 存档即 BE 的 game_data.sav），
   * 识别以版本字段为准；兜底默认小端。 */
  function detectEndian(dv, fileName) {
    if (dv.byteLength >= 4) {
      const vLE = dv.getUint32(0, true);
      const vBE = dv.getUint32(0, false);
      if (VALID_VERSIONS.indexOf(vLE) >= 0) return true;    // little endian
      if (VALID_VERSIONS.indexOf(vBE) >= 0) return false;   // big endian
    }
    return true;
  }

  function applySave(buf, fileName) {
    if (!flags) { UI.toast('标志查找表尚未就绪，请稍后重试'); return; }
    const dv = new DataView(buf);
    const littleEndian = detectEndian(dv, fileName);
    const srcName = littleEndian ? 'Switch（Ryujinx）' : 'Wii U（Cemu）';
    const done = new Map();         // hash -> type
    let playtime = 0, korokCnt = 0, entries = 0;
    for (let o = 0x0C; o + 8 <= dv.byteLength; o += 8) {
      const h = dv.getUint32(o, littleEndian);
      const v = dv.getUint32(o + 4, littleEndian);
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
        if (id) {
          if (!map.liveDone) map.liveDone = new Set();
          map.liveDone.add(id);          // 存档来源已完成（供 _mkDone 回忆类存档优先判定）
          if (!map.doneSet.has(id)) { map.doneSet.add(id); grew++; }
        }
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
    UI.toast('已从 ' + srcName + ' 存档加载 ✓ ' + parts.join(' · ') + (playtime ? '（游玩 ' + hrs + ' 小时）' : ''));
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
