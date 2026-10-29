/*
 * botw-core.js — BOTW game_data.sav 解析与改写核心（纯逻辑，无 UI）
 *
 * 关键修复（相对 marcrobledo 原版）：
 *   marcrobledo 用「ITEMS 全局槽号」去索引 FLAGS_ 与 FLAGSV_ 修饰数组，
 *   但 BOTW 的武器/弓/盾修饰数组是按「分类内本地序号」索引的，
 *   因此弓/盾的封印属性被读错、写入也写错位置（即“五连弓属性没对齐”）。
 *   本实现改用分类本地序号；箭矢单独归 ammo，不参与该数组。
 *
 * 安全边界：
 *   只允许写：卢比 / 最大心心 / 最大耐力 / 现有物品数量 /
 *   现有装备的 FLAGS+FLAGSV / 克洛格果实 / （实验）坐标与地图。
 *   导出前用 validateDiff() 逐字节检查，白名单外有任何变化即中止。
 */
(function (root, factory) {
  if (typeof module !== 'undefined' && module.exports) module.exports = { BotWSave: factory(require('./marc-file.js'), require('./botw-data.js')) };
  else root.BotWSave = factory({ MarcFile: root.MarcFile }, root.BOTW_DATA);
})(typeof self !== 'undefined' ? self : this, function (deps, DATA) {
  'use strict';
  const MarcFile = deps.MarcFile;
  const NAMES = DATA.NAMES, CATEGORIES = DATA.CATEGORIES, MODIFIERS = DATA.MODIFIERS, STAMINA_PRESETS = DATA.STAMINA_PRESETS;

  const MAX_ITEMS = 420;
  const ITEM_SIZE = 0x80;
  const QTY8 = 0x08;
  const FULL_DURABILITY = 65500;
  const MAX_DURABILITY = 65535;

  // 存档里真实的 u32 值 -> 字段名（marcrobledo Hashes + 坐标/地图）
  const HASHES = {
    0x0bee9e46: 'MAP',
    0x0cbf052a: 'FLAGS_BOW', 0x1e3fd294: 'FLAGSV_BOW',
    0x23149bf8: 'RUPEES', 0x2906f327: 'MAX_HEARTS', 0x3adff047: 'MAX_STAMINA',
    0x57ee221d: 'FLAGS_WEAPON', 0x5f283289: 'ITEMS', 0x69f17e8a: 'FLAGSV_SHIELD',
    0x6a09fc59: 'ITEMS_QUANTITY', 0x73c29681: 'PLAYTIME', 0x8a94e07a: 'KOROK_SEED_COUNTER',
    0xa40ba103: 'PLAYER_POSITION', 0xa6d926bc: 'FLAGSV_WEAPON', 0xc5238d2b: 'FLAGS_SHIELD',
    0xd913b769: 'MAPTYPE'
  };
  const VERSION = ['1.0', '1.1', '1.2', '1.3', '1.3.1', 'Kiosk', '1.3.3', '1.3.4', '1.4', '1.5', '1.5*', '1.6', '1.6*', '1.6**', '1.6***', '1.9'];
  const HEADER = [0x24e2, 0x24ee, 0x2588, 0x29c0, 0x2a46, 0x2f8e, 0x3ef8, 0x3ef9, 0x471a, 0x471b, 0x471b, 0x471e, 0x0f423d, 0x0f423e, 0x0f423f, 0x4730];
  const FILESIZE = [896976, 897160, 897112, 907824, 907824, 916576, 1020648, 1020648, 1027208, 1027208, 1027248, 1027216, 1027216, 1027216, 1027216, 1027248];

  const FLAGS_BASE = { weapons: 'FLAGS_WEAPON', bows: 'FLAGS_BOW', shields: 'FLAGS_SHIELD' };
  const FLAGSV_BASE = { weapons: 'FLAGSV_WEAPON', bows: 'FLAGSV_BOW', shields: 'FLAGSV_SHIELD' };
  const EQUIP_CATS = ['weapons', 'bows', 'shields'];
  const HIDDEN_MODS = new Set([0x1f, 0x3d, 0x20]); // 10发散射/10连狙击/未知 0x20，不进普通下拉

  function isArrow(id) {
    return /Arrow/.test(id) && id.indexOf('Weapon_Bow_') !== 0;
  }

  function catOf(id) {
    if (CATEGORIES[id]) {
      if (CATEGORIES[id] === 'bows' && isArrow(id)) return 'ammo';
      return CATEGORIES[id];
    }
    if (id.indexOf('Weapon_Bow_') === 0) return 'bows';
    if (id.indexOf('Weapon_Shield_') === 0) return 'shields';
    if (id.indexOf('Weapon_Sword_') === 0 || id.indexOf('Weapon_Lsword_') === 0 || id.indexOf('Weapon_Spear_') === 0) return 'weapons';
    if (isArrow(id)) return 'ammo';
    return 'other';
  }

  function clampInt(v, min, max, fallback) {
    var n = parseInt(v, 10);
    if (isNaN(n)) n = fallback;
    return Math.max(min, Math.min(max, n));
  }

  class BotWSave {
    constructor(marcFile) {
      this.file = marcFile;
      this.offsets = {};
      this.endian = null;
      this.platform = '';
      this.items = [];
      this.valid = false;
      this.version = '';
      this.addedKorokSlot = -1;
      this.teleportEnabled = false;
    }

    static fromBuffer(u8) { return new BotWSave(new MarcFile(u8)); }
    static fromFile(fileObj) {
      return new Promise(function (res) {
        const mf = new MarcFile(fileObj, function () { res(new BotWSave(mf)); });
      });
    }

    // 端序判定 + 版本校验
    detect() {
      for (var k = 0; k < 2; k++) {
        var le = k === 0;
        this.file.littleEndian = le;
        var sentinel = this.file.readU32(4);
        var vh = this.file.readU32(0);
        var sizeOk = FILESIZE.indexOf(this.file.fileSize) >= 0 || (this.file.fileSize >= 896976 && this.file.fileSize <= 1500000);
        if (sentinel === 0xffffffff && HEADER.indexOf(vh) >= 0 && sizeOk) {
          this.endian = le ? 'little' : 'big';
          this.platform = le ? 'Switch' : 'Wii U';
          var idx = HEADER.indexOf(vh);
          this.version = VERSION[idx] || ('v' + idx);
          return true;
        }
      }
      return false;
    }

    _locate() {
      this.offsets = {};
      for (var h in HASHES) {
        if (!Object.prototype.hasOwnProperty.call(HASHES, h)) continue;
        var target = Number(h);
        for (var j = 0x0c; j < this.file.fileSize; j += 8) {
          if (this.file.readU32(j) === target) { this.offsets[HASHES[h]] = j + 4; break; }
        }
      }
      return Object.keys(this.offsets).length;
    }

    // 读取全部背包物品 + 装备修饰（用分类本地序号；箭矢不参与）
    readInventory() {
      this._locate();
      this.items = [];
      var counters = { weapons: 0, bows: 0, shields: 0 };
      for (var gi = 0; gi < MAX_ITEMS; gi++) {
        var id = this.file.readString64(this.offsets.ITEMS, gi);
        if (id === '') continue;
        var cat = catOf(id);
        var qty = this.file.readU32(this.offsets.ITEMS_QUANTITY + gi * QTY8);
        var local = -1, flags = 0, flagsV = 0;
        if (cat === 'weapons' || cat === 'bows' || cat === 'shields') {
          local = counters[cat]++;
          if (this.offsets[FLAGS_BASE[cat]] !== undefined) flags = this.file.readU32(this.offsets[FLAGS_BASE[cat]] + local * 8);
          if (this.offsets[FLAGSV_BASE[cat]] !== undefined) flagsV = this.file.readU32(this.offsets[FLAGSV_BASE[cat]] + local * 8);
        }
        this.items.push({ gi: gi, id: id, cat: cat, qty: qty, local: local, flags: flags, flagsV: flagsV });
      }
      return this.items;
    }

    load() {
      if (!this.detect()) { this.valid = false; return false; }
      this._locate();
      if (this.offsets.ITEMS === undefined || this.offsets.ITEMS_QUANTITY === undefined) { this.valid = false; return false; }
      this.readInventory();
      this.valid = true;
      return true;
    }

    /* ===== 基础数值 ===== */
    getRupees() { return this.offsets.RUPEES !== undefined ? this.file.readU32(this.offsets.RUPEES) : 0; }
    setRupees(v) {
      if (this.offsets.RUPEES === undefined) return false;
      this.file.writeU32(this.offsets.RUPEES, clampInt(v, 0, 999999, 0) >>> 0);
      return true;
    }

    // 心数：存储值 = 心 × 4；游戏上限 30 心
    getHearts() { return this.offsets.MAX_HEARTS !== undefined ? this.file.readU32(this.offsets.MAX_HEARTS) / 4 : 0; }
    setHearts(n) {
      if (this.offsets.MAX_HEARTS === undefined) return false;
      var hearts = clampInt(n, 1, 30, 3);
      this.file.writeU32(this.offsets.MAX_HEARTS, (hearts * 4) >>> 0);
      return true;
    }

    getStaminaBits() { return this.offsets.MAX_STAMINA !== undefined ? this.file.readU32(this.offsets.MAX_STAMINA) : 0; }
    setStaminaBits(bits) {
      if (this.offsets.MAX_STAMINA === undefined) return false;
      this.file.writeU32(this.offsets.MAX_STAMINA, bits >>> 0);
      return true;
    }

    getPlaytime() { return this.offsets.PLAYTIME !== undefined ? this.file.readU32(this.offsets.PLAYTIME) : 0; }
    getKorokCounter() { return this.offsets.KOROK_SEED_COUNTER !== undefined ? this.file.readU32(this.offsets.KOROK_SEED_COUNTER) : 0; }

    static formatPlaytime(seconds) {
      seconds = Math.max(0, parseInt(seconds, 10) || 0);
      var h = Math.floor(seconds / 3600);
      var m = Math.floor((seconds % 3600) / 60);
      var s = seconds % 60;
      return h + ':' + (m < 10 ? '0' : '') + m + ':' + (s < 10 ? '0' : '') + s;
    }

    static staminaLabel(bits) {
      for (var i = 0; i < STAMINA_PRESETS.length; i++) if (STAMINA_PRESETS[i].bits === bits) return STAMINA_PRESETS[i].label;
      return '自定义';
    }

    /* ===== 克洛格果实（背包持有，不是收集计数） ===== */
    getKorokItem() {
      for (var i = 0; i < this.items.length; i++) if (this.items[i].id === 'Obj_KorokNuts') return this.items[i];
      return null;
    }
    getKorokHold() { var it = this.getKorokItem(); return it ? it.qty : 0; }
    setKorokHold(v) {
      var it = this.getKorokItem();
      if (!it) return false;
      var q = clampInt(v, 0, 900, 0);
      this.setQuantity(it.gi, q);
      it.qty = q;
      return true;
    }
    // 特例：背包里没有克洛格果实时，往第一个空槽写一个（受白名单约束）
    ensureKorokItem() {
      var exists = this.getKorokItem();
      if (exists) return exists;
      var used = {};
      for (var i = 0; i < this.items.length; i++) used[this.items[i].gi] = true;
      var slot = -1;
      for (var gi = 0; gi < MAX_ITEMS; gi++) {
        if (used[gi]) continue;
        if (!this.file.readString64(this.offsets.ITEMS, gi)) { slot = gi; break; }
      }
      if (slot < 0) return null;
      this._writeItemName(this.offsets.ITEMS + slot * ITEM_SIZE, 'Obj_KorokNuts');
      this.addedKorokSlot = slot;
      this.setQuantity(slot, 1);
      this.readInventory();
      return this.getKorokItem();
    }

    /* ===== 装备修饰（分类本地序号） ===== */
    setModifier(cat, local, flags, flagsV) {
      if (EQUIP_CATS.indexOf(cat) < 0) return false;
      if (!Number.isInteger(local) || local < 0 || local >= MAX_ITEMS) return false;
      var off = this.offsets[FLAGS_BASE[cat]], voff = this.offsets[FLAGSV_BASE[cat]];
      if (off === undefined || voff === undefined) return false;
      this.file.writeU32(off + local * 8, flags >>> 0);
      this.file.writeU32(voff + local * 8, flagsV >>> 0);
      return true;
    }

    /* ===== 耐久 / 数量（ITEMS_QUANTITY，全局槽号） ===== */
    setQuantity(gi, v) {
      if (!Number.isInteger(gi) || gi < 0 || gi >= MAX_ITEMS) return false;
      if (this.offsets.ITEMS_QUANTITY === undefined) return false;
      this.file.writeU32(this.offsets.ITEMS_QUANTITY + gi * 8, v >>> 0);
      return true;
    }

    /* ===== 耐久百分比 ===== */
    static durabilityToPercent(qty) {
      if (!qty) return 0;
      return Math.max(0, Math.min(100, Math.round(qty / 655)));
    }
    static percentToDurability(pct) {
      pct = clampInt(pct, 0, 100, 100);
      return pct >= 100 ? FULL_DURABILITY : Math.round(pct * 655);
    }

    /* ===== 坐标 / 地图 ===== */
    getPosition() {
      if (this.offsets.PLAYER_POSITION === undefined) return null;
      return {
        x: this.file.readF32(this.offsets.PLAYER_POSITION),
        y: this.file.readF32(this.offsets.PLAYER_POSITION + 8),
        z: this.file.readF32(this.offsets.PLAYER_POSITION + 16)
      };
    }
    setPosition(x, y, z) {
      if (this.offsets.PLAYER_POSITION === undefined) return false;
      x = Number(x); y = Number(y); z = Number(z);
      if (!isFinite(x) || !isFinite(y) || !isFinite(z)) return false;
      this.file.writeF32(this.offsets.PLAYER_POSITION, x);
      this.file.writeF32(this.offsets.PLAYER_POSITION + 8, y);
      this.file.writeF32(this.offsets.PLAYER_POSITION + 16, z);
      return true;
    }
    _readChunkedString(offset, chunks) {
      if (offset === undefined) return '';
      var s = '';
      for (var j = 0; j < chunks; j++) s += this.file.readString(offset + j * 8, 4);
      return s;
    }
    _writeChunkedString(offset, str, chunks) {
      if (offset === undefined) return false;
      str = String(str || '');
      for (var j = 0; j < chunks; j++) {
        var base = offset + j * 8;
        // 只写 4 个字符，保留后 4 字节（存档的 4 字节间隔/哈希）
        for (var k = 0; k < 4; k++) this.file.writeU8(base + k, j * 4 + k < str.length ? str.charCodeAt(j * 4 + k) & 0xff : 0);
      }
      return true;
    }
    // 物品名同样只写 4 字符，保留 4 字节间隔
    _writeItemName(offset, str) {
      str = String(str || '');
      for (var j = 0; j < 16; j++) {
        var base = offset + j * 8;
        for (var k = 0; k < 4; k++) this.file.writeU8(base + k, j * 4 + k < str.length ? str.charCodeAt(j * 4 + k) & 0xff : 0);
      }
    }
    getMap() { return this._readChunkedString(this.offsets.MAP, 8); }
    getMapType() { return this._readChunkedString(this.offsets.MAPTYPE, 8); }
    setMap(map, mapType) {
      return this._writeChunkedString(this.offsets.MAP, map, 8) && this._writeChunkedString(this.offsets.MAPTYPE, mapType, 8);
    }
    static guessMapGrid(x, z) {
      var gx = Math.min(10, Math.max(1, Math.trunc((Number(x) + 4974.629) / 9949.258 * 10 + 1)));
      var gz = Math.min(8, Math.max(1, Math.trunc((Number(z) + 3974.629) / 7949.258 * 8 + 1)));
      return String.fromCharCode(64 + gx) + '-' + gz;
    }
    setTeleportEnabled(on) { this.teleportEnabled = !!on; }

    /* ===== 修饰名解析 ===== */
    static modifierInfo(flags) {
      flags = flags >>> 0;
      var base = flags & 0x7fffffff;
      var enhanced = (flags & 0x80000000) !== 0;
      for (var i = 0; i < MODIFIERS.length; i++) {
        if ((MODIFIERS[i].value & 0x7fffffff) === base) {
          return { name: MODIFIERS[i].name, enhanced: enhanced, base: base, value: flags };
        }
      }
      return { name: '特殊属性 0x' + flags.toString(16), enhanced: enhanced, base: base, value: flags };
    }

    // 该分类可用的常规修饰（隐藏 0x1F/0x3D/0x20 特殊组合）
    static modifierOptions(cat) {
      var out = [];
      for (var i = 0; i < MODIFIERS.length; i++) {
        var m = MODIFIERS[i];
        var base = m.value & 0x7fffffff;
        if (HIDDEN_MODS.has(base)) continue;
        if (m.value === 0 || (m.cats && m.cats.indexOf(cat) >= 0)) out.push(m);
      }
      return out;
    }

    // 数值输入模式：bonus=攻击/耐久/防御；advanced=远投/速射；none=五连射/暴击/冲浪/无
    static modifierValueMode(flags) {
      var base = (flags >>> 0) & 0x7fffffff;
      if (base === 0x1 || base === 0x2 || base === 0x100) return 'bonus';
      if (base === 0x8 || base === 0x40) return 'advanced';
      return 'none';
    }
    static modifierDefaultValue(flags) {
      var base = (flags >>> 0) & 0x7fffffff;
      if (base === 0x1 || base === 0x2 || base === 0x100) return 10;
      if (base === 0x8) return 0x6db;   // 远投：样例里的常见值
      if (base === 0x40) return 0x497;  // 速射：样例里的常见值
      return 0;
    }

    /* ===== 白名单 / 导出校验 ===== */
    getAllowedRanges() {
      var ranges = [];
      var add = function (start, len) {
        if (typeof start === 'number' && start >= 0 && start + len <= this.file.fileSize) ranges.push([start, start + len]);
      }.bind(this);
      add(this.offsets.RUPEES, 4);
      add(this.offsets.MAX_HEARTS, 4);
      add(this.offsets.MAX_STAMINA, 4);

      for (var i = 0; i < this.items.length; i++) {
        var it = this.items[i];
        var allowedQty = EQUIP_CATS.indexOf(it.cat) >= 0 || it.cat === 'ammo' || it.cat === 'materials' || it.cat === 'food' || it.id === 'Obj_KorokNuts';
        if (allowedQty) add(this.offsets.ITEMS_QUANTITY + it.gi * 8, 4);
        if (EQUIP_CATS.indexOf(it.cat) >= 0 && it.local >= 0) {
          add(this.offsets[FLAGS_BASE[it.cat]] + it.local * 8, 4);
          add(this.offsets[FLAGSV_BASE[it.cat]] + it.local * 8, 4);
        }
      }
      // 克洛格特例：允许刚写入的物品名槽（仅这个槽）
      if (this.addedKorokSlot >= 0) add(this.offsets.ITEMS + this.addedKorokSlot * ITEM_SIZE, ITEM_SIZE);

      // 坐标实验：只有启用时才允许
      if (this.teleportEnabled) {
        add(this.offsets.PLAYER_POSITION, 4);
        add(this.offsets.PLAYER_POSITION + 8, 4);
        add(this.offsets.PLAYER_POSITION + 16, 4);
        add(this.offsets.MAP, 64);
        add(this.offsets.MAPTYPE, 64);
      }
      return ranges;
    }

    _inRanges(ranges, idx) {
      for (var i = 0; i < ranges.length; i++) if (idx >= ranges[i][0] && idx < ranges[i][1]) return true;
      return false;
    }

    // 导出前逐字节检查；bad 非空说明有白名单外的改动
    validateDiff(originalBytes) {
      var cur = this.file._u8;
      var len = Math.min(cur.length, originalBytes.length);
      var ranges = this.getAllowedRanges();
      var bad = [];
      var diff = 0;
      for (var i = 0; i < len; i++) {
        if (cur[i] !== originalBytes[i]) {
          diff++;
          if (!this._inRanges(ranges, i)) bad.push(i);
        }
      }
      if (cur.length !== originalBytes.length) bad.push(-1);
      return { ok: bad.length === 0, diff: diff, bad: bad };
    }

    // 按字段段统计“改动了几项”
    countChangedFields(originalBytes) {
      var cur = this.file._u8;
      var ranges = this.getAllowedRanges();
      var n = 0;
      for (var r = 0; r < ranges.length; r++) {
        var s = ranges[r][0], e = ranges[r][1];
        for (var i = s; i < e && i < cur.length; i++) {
          if (cur[i] !== originalBytes[i]) { n++; break; }
        }
      }
      return n;
    }

    nameOf(id) { return NAMES[id] || id; }
    static staminaPresets() { return STAMINA_PRESETS; }
    getArrayBuffer() { return this.file._u8.buffer.slice(this.file._u8.byteOffset, this.file._u8.byteOffset + this.file._u8.byteLength); }
  }

  BotWSave.MAX_ITEMS = MAX_ITEMS;
  BotWSave.FULL_DURABILITY = FULL_DURABILITY;
  BotWSave.MAX_DURABILITY = MAX_DURABILITY;
  return BotWSave;
});
