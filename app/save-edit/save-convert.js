/*
 * save-convert.js — BOTW game_data.sav 跨平台转换（Switch ↔ Wii U），纯前端、无依赖。
 *
 * 存档结构（实测）：
 *   12 字节头（u32 版本号 + 0xFFFFFFFF + u32 1）+ N 条 [hash(u32), 4 字节值] + 4 字节尾 0xFFFFFFFF
 *   按 hash 升序；同一 hash 可重复，代表数组/多值（坐标 X/Y/Z、物品名 16×4 字符…）。
 *
 * 转换规则（与 tools/save-convert/botw_save_convert.py 保持一致）：
 *   ① 目标版本骨架 = 目标版本号 + 目标端 hash 序列；
 *      1.5(0x471B) 与 1.6(0x471E) 的差异只有 Ichigeki_Pedestal_Appear 一条（真机存档实测）；
 *   ② 值按 hash + 出现次序搬运；字符串表（物品名/马名/相册地名…，共 35 张）原样搬 4 字节，不翻字节序；
 *      其余数值/浮点按目标平台字节序翻转；
 *   ③ 产物重新解析校验（版本、条数、逐条值）。
 *
 * 只支持 v1.5（Wii U 最终版）与 v1.6（Switch 最终版）互转；其余版本请先进游戏用这两个版本存一次。
 */
(function (root, factory) {
  if (typeof module !== 'undefined' && module.exports) module.exports = factory();
  else root.SaveConvert = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var V15 = 0x471B, V16 = 0x471E, V19 = 0x4730;
  var EXTRA_16 = 0xF3316ABB;   // Ichigeki_Pedestal_Appear：1.6 比 1.5 多的一条 flag
  var EXTRA_16_NAME = 'Ichigeki_Pedestal_Appear';

  // 1.9.0（2026-02-17，Switch 2 Edition 时期）比 1.6 多的 4 条 flag（实测 1.9 存档，值均为 0；romfs 1.6 字典里无名称）
  var EXTRA_19 = [0x62133C14, 0x8DAFA42C, 0xCF031EFD, 0xFA050B46];

  // 各版本相对 1.5 多出的 flag（嵌套关系：1.5 ⊂ 1.6 ⊂ 1.9）
  var EXTRAS = {
    '1.5': [],
    '1.6': [EXTRA_16],
    '1.9': [EXTRA_16].concat(EXTRA_19)
  };
  var FLAG_NAMES = {};
  FLAG_NAMES[EXTRA_16] = EXTRA_16_NAME;

  // 目标格式（版本 → 版本号 / 正式体积）；平台只决定字节序
  var FORMATS = {
    '1.5': { id: V15, size: 1027208 },
    '1.6': { id: V16, size: 1027216 },
    '1.9': { id: V19, size: 1027248 }
  };

  // 存档版本表（与 app/save-edit/botw-core.js 同源）：[版本号, 版本名, 体积]
  var VERSIONS = [
    [0x24E2, '1.0', 896976], [0x24EE, '1.1', 897160], [0x2588, '1.2', 897112],
    [0x29C0, '1.3', 907824], [0x2A46, '1.3.1', 907824], [0x2F8E, 'Kiosk', 916576],
    [0x3EF8, '1.3.3', 1020648], [0x3EF9, '1.3.4', 1020648],
    [0x471A, '1.4', 1027208], [0x471B, '1.5', 1027208], [0x471B, '1.5*', 1027248],
    [0x471E, '1.6', 1027216], [0x0F423D, '1.6*', 1027216],
    [0x0F423E, '1.6**', 1027216], [0x0F423F, '1.6***', 1027216],
    [0x4730, '1.9', 1027248]
  ];

  // 字符串表：值按 4 字节原样搬运（不含字节序翻转）。共 35 张（真机存档实测）。
  var STRING_HASHES = {};
  [
    0x00AC6BC9, // Horse_AmiiboUidHash
    0x0BEE9E46, // PlayerSavePosMapName
    0x0F9674FF, // EquipStandShield_Slot
    0x17CB2B11, // Horse_UsedAmiiboUidHash
    0x1C10782F, // WolfLink_TodayFirstDetectedUID
    0x22A510D1, // CookMaterialName2
    0x2F95768F, // AlbumSaveLocationName
    0x333AA6E5, // Horse_SaddleName
    0x33BB43BB, // ActorName_SeakSensor_Slot2
    0x3863B57D, // AmiiboTouchHistory
    0x39D08876, // DeadHorse_AmiiboUidHash
    0x43C96BC7, // DeadHorse_ManeName
    0x530DD198, // MemoryPhotoSaveDistrictName
    0x55A22047, // CookMaterialName3
    0x5F283289, // PorchItem（物品名）
    0x6150C6BE, // Horse_ReinsName
    0x708311BC, // EquipStandSword_Slot
    0x71FE692E, // DeadHorse_ActorName
    0x750E9D0E, // AmiiboTouchHistoryTotal
    0x7B74E117, // Horse_UserName
    0x81068A31, // PlayerName
    0x9B280A3B, // AlbumSaveDistrictName
    0x9C6CFD3F, // Horse_ManeName
    0xA4D177EF, // DeadHorse_UserName
    0xAAB21201, // ActorName_SeakSensor_Slot1
    0xBBAC416B, // CookMaterialName1
    0xC247B696, // Horse_ActorName
    0xCBC6B5E4, // CookMaterialName4
    0xCCAB71FD, // CookMaterialName0
    0xD2E91906, // DeadHorse_ReinsName
    0xDDB52297, // ActorName_SeakSensor_Slot0
    0xE7B0AD2C, // MemoryPhotoSaveLocationName
    0xF43EEBA6, // AlbumPictureActorName
    0xF6332484, // DeadHorse_SaddleName
    0xFACFD421  // EquipStandBow_Slot
  ].forEach(function (h) { STRING_HASHES[h] = true; });

  function versionName(id, size) {
    for (var i = 0; i < VERSIONS.length; i++) {
      if (VERSIONS[i][0] === id && VERSIONS[i][2] === size) return VERSIONS[i][1];
    }
    return null;
  }

  function fail(msg) { throw new Error(msg); }

  /** 解析存档字节 → { le, versionId, version, knownVersion, size, records:[{h,v,raw}] } */
  function parse(input) {
    var u8 = input instanceof Uint8Array ? input : new Uint8Array(input);
    var len = u8.length;
    if (len < 16) fail('文件太小（' + len + ' 字节），不是 game_data.sav');
    var n = (len - 16) / 8;
    if (n !== Math.floor(n) || n < 1000) fail('结构异常：条目数 ' + n + '，不是有效的 game_data.sav');
    var dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);

    // 端序判定：先看哨兵 0xFFFFFFFF，再用「hash 升序」验证——端序读错时顺序会立刻乱掉
    var cands = [];
    for (var k = 0; k < 2; k++) {
      var le = (k === 0);
      if (dv.getUint32(4, le) !== 0xFFFFFFFF) continue;
      var prev = -1, ok = true, i;
      for (i = 0; i < n; i++) {
        var h = dv.getUint32(12 + i * 8, le);
        if (h < prev) { ok = false; break; }
        prev = h;
      }
      if (!ok) continue;
      var vid = dv.getUint32(0, le);
      var nm = versionName(vid, len);
      cands.push({ le: le, vid: vid, name: nm, known: !!nm });
    }
    if (!cands.length) fail('无法识别：不是旷野之息存档，或哈希表顺序异常（文件可能损坏）');
    var pick = null, c;
    for (c = 0; c < cands.length; c++) { if (cands[c].known) { pick = cands[c]; break; } }
    if (!pick) pick = cands[0];

    var records = [];
    for (var j = 0; j < n; j++) {
      var off = 12 + j * 8;
      records.push({ h: dv.getUint32(off, pick.le), v: dv.getUint32(off + 4, pick.le), raw: u8.slice(off + 4, off + 8) });
    }
    return {
      le: pick.le,
      versionId: pick.vid,
      version: pick.name || ('0x' + pick.vid.toString(16).toUpperCase()),
      knownVersion: pick.known,
      size: len,
      records: records
    };
  }

  function platformOf(save) { return save.le ? 'Switch' : 'Wii U'; }

  function extrasOf(versionName) {
    return Object.prototype.hasOwnProperty.call(EXTRAS, versionName) ? EXTRAS[versionName] : null;
  }

  function flagName(h) {
    return FLAG_NAMES[h] || ('0x' + h.toString(16).toUpperCase());
  }

  /** 两个版本之间会丢弃/补齐哪些 flag（供 UI 提示；不支持返回 null） */
  function deltaInfo(srcVersion, targetVersion) {
    var s = extrasOf(srcVersion), t = extrasOf(targetVersion);
    if (!s || !t) return null;
    var ss = {}, ts = {};
    s.forEach(function (h) { ss[h] = true; });
    t.forEach(function (h) { ts[h] = true; });
    return {
      dropped: s.filter(function (h) { return !ts[h]; }).map(function (h) { return { hash: h, name: flagName(h) }; }),
      added: t.filter(function (h) { return !ss[h]; }).map(function (h) { return { hash: h, name: flagName(h) }; })
    };
  }

  /**
   * 转换到指定格式。
   * opts = { platform: 'Wii U' | 'Switch', version: '1.5' | '1.6' | 'keep' }
   *   - '1.5' / '1.6'：目标版本（1.5↔1.6 的差异已知：多/少一条 Ichigeki_Pedestal_Appear）
   *   - 'keep'：保持源版本，只换字节序（用于同版本跨平台，或旧档交给新游戏自行升级）
   * 返回 { bytes, info }。
   */
  function convert(input, opts) {
    var src = parse(input);
    opts = opts || {};
    var platform = (opts.platform === 'Wii U') ? 'Wii U' : 'Switch';
    var le = (platform === 'Switch');
    var want = opts.version || 'keep';

    var targetId, targetName, targetSize;
    if (want === 'keep') {
      targetId = src.versionId;
      targetName = src.version;
      targetSize = src.size;
    } else {
      var fmt = FORMATS[want];
      if (!fmt) fail('未知目标版本：' + want);
      targetId = fmt.id;
      targetName = want;
      targetSize = fmt.size;
      var srcFmt = FORMATS[src.version];
      if (!srcFmt || srcFmt.id !== src.versionId || srcFmt.size !== src.size) {
        fail('当前存档是 v' + src.version + '，暂只支持 v1.5 / v1.6 / v1.9 之间的版本转换；' +
             '旧版本请选「保持源版本（只换字节序）」，让目标游戏读档时自行升级。');
      }
    }
    if (targetId === src.versionId && le === src.le) {
      fail('源存档已经是该格式，不需要转换');
    }

    // 目标 hash 序列：源 hash 去掉目标版本没有的、补上目标版本新增的（升序插入）
    var hashes = [];
    var dropped = [], added = [];
    var i, h;
    var srcE = extrasOf(src.version);
    var tgtE = extrasOf(targetName);
    if (!srcE || !tgtE) fail('不支持的版本组合：v' + src.version + ' → v' + targetName);
    var srcSet = {}, tgtSet = {};
    for (i = 0; i < srcE.length; i++) srcSet[srcE[i]] = true;
    for (i = 0; i < tgtE.length; i++) tgtSet[tgtE[i]] = true;
    var addList = [];
    for (i = 0; i < tgtE.length; i++) if (!srcSet[tgtE[i]]) addList.push(tgtE[i]);
    for (i = 0; i < srcE.length; i++) if (!tgtSet[srcE[i]]) dropped.push({ hash: srcE[i], name: flagName(srcE[i]) });
    addList.sort(function (a, b) { return a - b; });
    var ai = 0;
    for (i = 0; i < src.records.length; i++) {
      h = src.records[i].h;
      if (srcSet[h] && !tgtSet[h]) continue;
      while (ai < addList.length && addList[ai] < h) {
        hashes.push(addList[ai]);
        added.push({ hash: addList[ai], name: flagName(addList[ai]) });
        ai++;
      }
      hashes.push(h);
    }
    while (ai < addList.length) {
      hashes.push(addList[ai]);
      added.push({ hash: addList[ai], name: flagName(addList[ai]) });
      ai++;
    }

    if (hashes.length * 8 + 16 !== targetSize) {
      fail('内部错误：目标条目数 ' + hashes.length + ' 与目标体积不符');
    }

    // 源值分组（保留出现次序）
    var groups = {};
    for (i = 0; i < src.records.length; i++) {
      var r = src.records[i];
      (groups[r.h] || (groups[r.h] = [])).push(r);
    }

    var out = new Uint8Array(targetSize);
    var dv = new DataView(out.buffer);
    dv.setUint32(0, targetId, le);
    dv.setUint32(4, 0xFFFFFFFF, le);
    dv.setUint32(8, 1, le);

    var expect = [];
    var seen = {};
    var off = 12;
    for (i = 0; i < hashes.length; i++) {
      var hh = hashes[i];
      var occ = seen[hh] || 0;
      seen[hh] = occ + 1;
      var g = groups[hh];
      var rec = g && occ < g.length ? g[occ] : null;
      dv.setUint32(off, hh, le);
      if (STRING_HASHES[hh]) {
        var raw = rec ? rec.raw : new Uint8Array(4);
        out.set(raw, off + 4);
        expect.push({ h: hh, kind: 'raw', payload: raw });
      } else {
        var vv = rec ? rec.v : 0;
        dv.setUint32(off + 4, vv >>> 0, le);
        expect.push({ h: hh, kind: 'num', payload: vv >>> 0 });
      }
      off += 8;
    }
    dv.setUint32(off, 0xFFFFFFFF, le);

    // 自校验：重新解析，逐条比对
    var check = parse(out);
    if (check.versionId !== targetId || check.size !== targetSize) fail('校验失败：产物版本/体积不符');
    if (check.records.length !== expect.length) fail('校验失败：条目数不符');
    for (i = 0; i < expect.length; i++) {
      var a = check.records[i], e = expect[i];
      if (a.h !== e.h) fail('校验失败：第 ' + i + ' 条 hash 不符');
      var got = e.kind === 'raw' ? a.raw : a.v;
      if (e.kind === 'raw') {
        if (got[0] !== e.payload[0] || got[1] !== e.payload[1] || got[2] !== e.payload[2] || got[3] !== e.payload[3]) {
          fail('校验失败：第 ' + i + ' 条字符串值不符');
        }
      } else if (got !== e.payload) {
        fail('校验失败：第 ' + i + ' 条数值不符');
      }
    }

    return {
      bytes: out,
      info: {
        srcVersion: src.version, srcPlatform: platformOf(src),
        targetVersion: targetName, targetPlatform: platform,
        count: hashes.length, size: targetSize,
        dropped: dropped, added: added,
        keepVersion: want === 'keep',
        fileName: (platform === 'Wii U' ? 'game_data_cemu_v' : 'game_data_switch_v') + targetName + '.sav'
      }
    };
  }

  return {
    parse: parse,
    convert: convert,
    deltaInfo: deltaInfo,
    STRING_HASHES: STRING_HASHES,
    versionName: versionName
  };
});
