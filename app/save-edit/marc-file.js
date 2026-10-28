/*
 * marc-file.js — BOTW 存档二进制读写层（从 Marc Robledo 的 MarcFile 忠实移植）
 * 纯数据层，无 UI、无 DOM 依赖，Node / 浏览器通用。
 * 读写方法逐字节复刻 marcrobledo 实现，确保定位一致。
 */
(function (root, factory) {
  if (typeof module !== 'undefined' && module.exports) module.exports = { MarcFile: factory() };
  else root.MarcFile = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  function MarcFile(source, onload) {
    var isFile = (typeof source === 'object' && source && source.name && source.size);
    if (isFile) {
      // 浏览器 File 对象：异步读取
      this.fileName = source.name;
      this.fileType = source.type;
      this.fileSize = source.size;
      var reader = new FileReader();
      var self = this;
      reader.onload = function () {
        self._u8 = new Uint8Array(reader.result);
        self._dv = new DataView(reader.result);
        if (onload) onload.call(self);
      };
      reader.readAsArrayBuffer(source);
      return;
    }
    if (typeof source === 'number') {
      // 指定大小，新建空缓冲（浏览器 / Node 通用）
      this.fileName = 'file.bin';
      this.fileType = 'application/octet-stream';
      this.fileSize = source;
      var buf = new ArrayBuffer(source);
      this._u8 = new Uint8Array(buf);
      this._dv = new DataView(buf);
      if (onload) onload.call(this);
      return;
    }
    if (source instanceof ArrayBuffer) {
      this.fileSize = source.byteLength;
      this._u8 = new Uint8Array(source);
      this._dv = new DataView(source);
      if (onload) onload.call(this);
      return;
    }
    if (source instanceof Uint8Array) {
      this.fileSize = source.byteLength;
      this._u8 = source;
      this._dv = new DataView(source.buffer, source.byteOffset, source.byteLength);
      if (onload) onload.call(this);
      return;
    }
    throw new Error('Invalid MarcFile source');
  }

  MarcFile.prototype.littleEndian = false;

  // 机器原生端序（用于决定默认端序；BOTW 存档按平台存，需显式判定）
  MarcFile.prototype.IS_MACHINE_LITTLE_ENDIAN = (function () {
    var a = new ArrayBuffer(2);
    new DataView(a).setInt16(0, 256, true);
    return new Int16Array(a)[0] === 256;
  })();

  MarcFile.prototype.readU8 = function (a) { return this._u8[a]; };

  MarcFile.prototype.readU16 = function (a) {
    return this.littleEndian
      ? (this._u8[a] + (this._u8[a + 1] << 8)) >>> 0
      : ((this._u8[a] << 8) + this._u8[a + 1]) >>> 0;
  };

  MarcFile.prototype.readU24 = function (a) {
    return this.littleEndian
      ? (this._u8[a] + (this._u8[a + 1] << 8) + (this._u8[a + 2] << 16)) >>> 0
      : ((this._u8[a] << 16) + (this._u8[a + 1] << 8) + this._u8[a + 2]) >>> 0;
  };

  MarcFile.prototype.readU32 = function (a) {
    return this.littleEndian
      ? (this._u8[a] + (this._u8[a + 1] << 8) + (this._u8[a + 2] << 16) + (this._u8[a + 3] << 24)) >>> 0
      : ((this._u8[a] << 24) + (this._u8[a + 1] << 16) + (this._u8[a + 2] << 8) + this._u8[a + 3]) >>> 0;
  };

  MarcFile.prototype.readU32Array = function (a, n) {
    var out = new Array(n);
    for (var i = 0; i < n; i++) out[i] = this.readU32(a + i * 4);
    return out;
  };

  MarcFile.prototype.readF32 = function (a) {
    return this._dv.getFloat32(a, this.littleEndian);
  };

  // 4 字节 ASCII 字符串，遇 \0 停止（逐字节复刻 marcrobledo readString）
  MarcFile.prototype.readString = function (a, maxLen) {
    var c = '';
    for (var d = 0; d < maxLen && a + d < this.fileSize && this._u8[a + d] > 0; d++) {
      c += String.fromCharCode(this._u8[a + d]);
    }
    return c;
  };

  // 物品名：16 段 × 4 字节，每段 stride 8（4 数据 + 4 间隔）= 128 字节/格
  MarcFile.prototype.readString64 = function (offset, arrayIndex) {
    if (typeof arrayIndex === 'number') offset += 0x80 * arrayIndex;
    var txt = '';
    for (var j = 0; j < 16; j++) {
      txt += this.readString(offset + j * 8, 4);
    }
    return txt;
  };

  MarcFile.prototype.writeU8 = function (a, b) { this._u8[a] = b & 0xff; };

  MarcFile.prototype.writeU16 = function (a, b) {
    if (this.littleEndian) {
      this._u8[a] = b & 0xff; this._u8[a + 1] = (b >> 8) & 0xff;
    } else {
      this._u8[a] = (b >> 8) & 0xff; this._u8[a + 1] = b & 0xff;
    }
  };

  MarcFile.prototype.writeU32 = function (a, b) {
    b = b >>> 0;
    if (this.littleEndian) {
      this._u8[a] = b & 0xff; this._u8[a + 1] = (b >>> 8) & 0xff;
      this._u8[a + 2] = (b >>> 16) & 0xff; this._u8[a + 3] = (b >>> 24) & 0xff;
    } else {
      this._u8[a] = (b >>> 24) & 0xff; this._u8[a + 1] = (b >>> 16) & 0xff;
      this._u8[a + 2] = (b >>> 8) & 0xff; this._u8[a + 3] = b & 0xff;
    }
  };

  MarcFile.prototype.writeU32Array = function (a, arr) {
    for (var i = 0; i < arr.length; i++) this.writeU32(a + i * 4, arr[i]);
  };

  MarcFile.prototype.writeF32 = function (a, v) {
    this._dv.setFloat32(a, v, this.littleEndian);
  };

  // 物品名写回：按相同 16×stride-8 布局，每段 4 字节 ASCII + 4 字节零间隔
  MarcFile.prototype.writeString64 = function (offset, arrayIndex, str) {
    if (typeof arrayIndex === 'number') offset += 0x80 * arrayIndex;
    for (var j = 0; j < 16; j++) {
      var base = offset + j * 8;
      for (var k = 0; k < 4; k++) {
        this._u8[base + k] = (j * 4 + k < str.length) ? str.charCodeAt(j * 4 + k) & 0xff : 0;
      }
      this._u8[base + 4] = 0; this._u8[base + 5] = 0; this._u8[base + 6] = 0; this._u8[base + 7] = 0;
    }
  };

  return MarcFile;
});
