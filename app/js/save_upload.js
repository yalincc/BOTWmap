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
 * 覆盖：神庙 / 希卡塔 / 克洛格 / 主线回忆(13) / 神兽(4) / 任务完成标志 / 宝箱已开(1312 查表) /
 * 武器收集(P4b：IsGet_Weapon 190 查表，注入装备完成集)。
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
    // V1.6.0 任务完成标志反查表：hash -> flagName（quest_save.js 提供 flagName -> hash）
    buildQuestHash();
    buildChestHash();
    // P4b：武器收集反查表：hash -> flagName（weapon_collect_save.js 的 isGet 表）
    buildWeaponHash();
    buildPbHash();
    buildEnemyHash();
    // V1.6.4：恢复上次存档同步的扩展计数（localStorage 展示缓存，权威每次随存档重注入——同 ChestSync 模式）
    try {
      const d = JSON.parse(localStorage.getItem('botwmap.saveCounts.v1') || 'null');
      if (d) { window.__saveCounts = d.counts || null; window.__saveUploaded = !!d.uploaded; }
    } catch (e) {}
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
        window.__saveCounts = null;   // V1.6.4：扩展同步行一并撤下
        window.__saveUploaded = false;
        try { localStorage.removeItem('botwmap.saveCounts.v1'); } catch (e) {}
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

    // 查找表：V1.4.4 改由 <script src="data/save_flags.js"> 内联引入（全局 SAVE_FLAGS）。
    // 此前用 fetch(data/save_flags.json)，file:// 本地部署下 fetch 被浏览器禁止 → 查找表永远
    // 加载不到 → 报「标志查找表尚未就绪」。script 标签在 file:// 下可用，线上行为不变。
    flags = (typeof SAVE_FLAGS !== 'undefined') ? SAVE_FLAGS : null;
    if (!flags) console.warn('[save_upload] SAVE_FLAGS 查找表未加载：index.html 未引入 data/save_flags.js？');
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
    /* V1.6.4：版本字段无法识别 → 返回 null（疑似非旷野之息存档，applySave 直接拒绝）。
       此前兜底按小端硬解析——拖入王国之泪等其它游戏的存档会静默产出垃圾同步数据。 */
    if (dv.byteLength >= 4) {
      const vLE = dv.getUint32(0, true);
      const vBE = dv.getUint32(0, false);
      if (VALID_VERSIONS.indexOf(vLE) >= 0) return true;    // little endian
      if (VALID_VERSIONS.indexOf(vBE) >= 0) return false;   // big endian
    }
    return null;
  }

  /* V1.6.0：任务完成标志反查表 hash -> flagName（存档扁平表按 hash 检索，需要反向索引） */
  let questHashToFlag = null;
  function buildQuestHash() {
    if (questHashToFlag) return;
    const QS = (typeof QUEST_SAVE !== 'undefined') ? QUEST_SAVE : null;
    if (!QS) { console.warn('[save_upload] QUEST_SAVE 任务标志表未加载：index.html 未引入 data/quest_save.js？'); return; }
    questHashToFlag = {};
    Object.keys(QS).forEach(f => { questHashToFlag[QS[f]] = f; });
  }

  /* V1.6.3：宝箱已开查找表（chest_save.js 提供 hash -> [px,py]，1312 条 romfs 权威派生） */
  let chestHash2pt = null;
  function buildChestHash() {
    if (chestHash2pt) return;
    const CS = (typeof CHEST_SAVE !== 'undefined') ? CHEST_SAVE : null;
    if (!CS || !CS.hash2pt) { console.warn('[save_upload] CHEST_SAVE 宝箱查找表未加载：index.html 未引入 data/chest_save.js？'); return; }
    chestHash2pt = CS.hash2pt;
  }

  /* P4b：武器收集查找表 hash -> flagName（weapon_collect_save.js 的 isGet 表，190 条）。
     以 IsGet_Weapon_* 为主口径（实测覆盖最宽）；flagName 去前缀后 = weapon_dex.js 的 key。 */
  let weaponHashToFlag = null;
  function buildWeaponHash() {
    if (weaponHashToFlag) return;
    const WS = (typeof WEAPON_COLLECT_SAVE !== 'undefined') ? WEAPON_COLLECT_SAVE : null;
    if (!WS || !WS.isGet) { console.warn('[save_upload] WEAPON_COLLECT_SAVE 武器收集表未加载：index.html 未引入 data/weapon_collect_save.js？'); return; }
    weaponHashToFlag = {};
    Object.keys(WS.isGet).forEach(f => { weaponHashToFlag[WS.isGet[f]] = f; });
  }

  /* V1.6.4：图鉴登记（PB）查找表——weapon_collect_save.js 的 pictureBook 表（flagName -> hash）。
     拍照图鉴登记与 IsGet（获得过）是两组独立 flag：IsGet 捡到即亮，PB 要相机拍照登记。 */
  let pbHashSet = null;
  function buildPbHash() {
    if (pbHashSet) return;
    const WS = (typeof WEAPON_COLLECT_SAVE !== 'undefined') ? WEAPON_COLLECT_SAVE : null;
    if (!WS || !WS.pictureBook) return;
    pbHashSet = new Set(Object.keys(WS.pictureBook).map(f => Number(WS.pictureBook[f])));
  }

  /* V1.6.4：敌人讨伐（IsDefeat）查找表——enemy_save.js hash2pt（92 条，不重生 Boss）。
     值 = 地图像素点；家族归属按最近的大类标记（hinox/talus/molduga）就近归类（分母守恒）。 */
  let enemyHash2pt = null;
  let enemyFamilies = null;
  function buildEnemyHash() {
    if (enemyHash2pt) return;
    const ES = (typeof ENEMY_SAVE !== 'undefined') ? ENEMY_SAVE : null;
    if (!ES || !ES.hash2pt) { console.warn('[save_upload] ENEMY_SAVE 讨伐查找表未加载：index.html 未引入 data/enemy_save.js？'); return; }
    enemyHash2pt = ES.hash2pt;
    const famDefs = [['西诺克斯', 'hinox'], ['岩石巨人', 'talus'], ['莫尔德拉吉克', 'molduga']];
    const mk = (typeof BOTW_DATA !== 'undefined' && BOTW_DATA.markers) ? BOTW_DATA.markers : [];
    enemyFamilies = famDefs.map(function (d) {
      return { name: d[0], mks: mk.filter(function (m) { return m.cat === d[1]; }), total: 0, done: 0 };
    });
    Object.keys(enemyHash2pt).forEach(function (k) {
      const p = enemyHash2pt[k];
      let bi = 0, bd = 1e18;
      enemyFamilies.forEach(function (f, i) {
        f.mks.forEach(function (m) {
          const d = (m.px[0] - p[0]) * (m.px[0] - p[0]) + (m.px[1] - p[1]) * (m.px[1] - p[1]);
          if (d < bd) { bd = d; bi = i; }
        });
      });
      enemyFamilies[bi].total++;
    });
  }

  function applySave(buf, fileName) {
    if (!flags) { UI.toast('标志查找表尚未就绪，请稍后重试'); return; }
    const dv = new DataView(buf);
    const littleEndian = detectEndian(dv, fileName);
    if (littleEndian === null) {
      UI.toast('这不是旷野之息的存档（版本标识不匹配）——王国之泪等其它游戏的存档无法解析', 6000);
      return;
    }
    const srcName = littleEndian ? 'Switch（Ryujinx）' : 'Wii U（Cemu）';
    const done = new Map();         // hash -> type
    const questFlags = {};          // V1.6.0: flagName -> true（任务完成标志）
    const chestPts = [];            // V1.6.3: 已开宝箱像素点（chest_save.js 反查）
    const weaponFlags = {};         // P4b: 武器收集 flagName -> true（IsGet_Weapon 反查）
    let pbCnt = 0;                  // V1.6.4: 图鉴登记命中
    const enemyPts = [];            // V1.6.4: 讨伐命中像素点（enemy_save 反查）
    let playtime = 0, korokCnt = 0, entries = 0;
    for (let o = 0x0C; o + 8 <= dv.byteLength; o += 8) {
      const h = dv.getUint32(o, littleEndian);
      const v = dv.getUint32(o + 4, littleEndian);
      entries++;
      if (h === H_PLAYTIME) playtime = v;
      else if (h === H_KOROK_CNT) korokCnt = v;
      else if (v !== 0 && questHashToFlag && questHashToFlag[h]) questFlags[questHashToFlag[h]] = true;
      else if (v !== 0 && chestHash2pt && chestHash2pt[h]) chestPts.push(chestHash2pt[h]);
      else if (v !== 0 && weaponHashToFlag && weaponHashToFlag[h]) weaponFlags[weaponHashToFlag[h]] = true;
      else if (v !== 0 && pbHashSet && pbHashSet.has(h)) pbCnt++;
      else if (v !== 0 && enemyHash2pt && enemyHash2pt[h]) enemyPts.push(enemyHash2pt[h]);
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
    // V1.6.0 任务完成同步（存档权威）：命中任务标志 → 注入任务板块 → 刷新任务行计数与绘制
    const qfNames = Object.keys(questFlags);
    if (qfNames.length && window.QuestPanel) {
      window.QuestPanel.QuestDone.loadSave(questFlags);
      if (window._questRefreshRows) window._questRefreshRows();
      map.draw();
    }
    // V1.6.3 宝箱完成同步（存档权威）：已开宝箱像素点 → 注入存档完成集 → 重绘
    if (chestPts.length && window.ChestSync) {
      window.ChestSync.loadSave(chestPts);
      map.draw();
    }
    // P4b 武器收集同步（存档权威）：IsGet_Weapon 命中 → 去前缀成 dex key
    // （IsGet_Weapon_Sword_043 → Weapon_Sword_043）→ 注入装备完成集（存档 ∪ 手动）
    const wfNames = Object.keys(weaponFlags);
    if (wfNames.length && window.EquipPanel && window.EquipPanel.EquipDone) {
      window.EquipPanel.EquipDone.loadSave(wfNames.map(n => n.replace(/^IsGet_Weapon_/, 'Weapon_')));
      map.draw();
    }
    loaded = true;
    // V1.6.4：扩展同步计数（武器获得 / 图鉴登记 / 讨伐三族）供统计面板展示
    const ef = enemyFamilies || [];
    ef.forEach(function (f) { f.done = 0; });
    const enemyHitIds = [];
    enemyPts.forEach(function (p) {
      let bi = 0, bd = 1e18, bmk = null;
      ef.forEach(function (f, i) {
        f.mks.forEach(function (m) {
          const d = (m.px[0] - p[0]) * (m.px[0] - p[0]) + (m.px[1] - p[1]) * (m.px[1] - p[1]);
          if (d < bd) { bd = d; bi = i; bmk = m; }
        });
      });
      if (ef[bi]) ef[bi].done++;
      /* V1.6.4：讨伐上地图——命中的固定点位标记点亮（<300px 才有标记对应，7/92 无标记不亮） */
      if (bmk && bd < 300 * 300) enemyHitIds.push(bmk.id);
    });
    if (enemyHitIds.length) {
      if (!map.liveDone) map.liveDone = new Set();
      enemyHitIds.forEach(function (id) {
        map.liveDone.add(id);
        if (!map.doneSet.has(id)) map.doneSet.add(id);   // 讨伐是永久成就，离线兜底也保持
      });
      UI.persistDone();
      UI.updateLayerList();
      map.draw();
    }
    const WSC = (typeof WEAPON_COLLECT_SAVE !== 'undefined') ? WEAPON_COLLECT_SAVE : null;
    window.__saveCounts = {
      weapon: wfNames.length,
      weaponTotal: (WSC && Object.keys(WSC.isGet).length) || 196,
      pb: pbCnt,
      pbTotal: (WSC && Object.keys(WSC.pictureBook).length) || 179,
      enemy: ef.map(function (f) { return { name: f.name, done: f.done, total: f.total }; })
    };
    try { localStorage.setItem('botwmap.saveCounts.v1', JSON.stringify({ uploaded: true, counts: window.__saveCounts })); } catch (e) {}
    window.__saveUploaded = true;   // 统计面板「存档已同步 ✓」行
    UI.renderStats();

    // 摘要
    const parts = [];
    for (const k of ['shrine', 'tower', 'korok', 'beast']) {
      if (byT[k]) parts.push(CAT_CN[k] + ' ' + byT[k]);
    }
    if (qfNames.length) parts.push('任务 ' + qfNames.length);
    if (chestPts.length) parts.push('宝箱 ' + chestPts.length);
    if (wfNames.length) parts.push('武器 ' + wfNames.length);
    if (pbCnt) parts.push('图鉴 ' + pbCnt);
    if (enemyPts.length) parts.push('讨伐 ' + enemyPts.length);
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
