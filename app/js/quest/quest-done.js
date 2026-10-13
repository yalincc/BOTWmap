/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 完成态（双源）
 * ------------------------------------------------------------
 * 完成状态两个来源：
 *   - saveSet   ：存档解析（权威）—— save_upload.js 解析任务完成标志后调 QuestDone.loadSave
 *   - manualSet ：手动标记（localStorage）—— 未映射到存档标志的任务以此兜底
 * 对外：window.QuestDone
 * ============================================================ */
(function (global) {
  'use strict';

  var D = global.QuestData;
  var QUESTS = D ? D.QUESTS : [];

  var K_DONE = 'botwmap.questDone.v1';
  var manualSet = new Set();   // 手动标记（localStorage）
  var saveSet = new Set();     // 存档解析（save_upload 注入）
  var listeners = [];

  function loadDone() {
    try {
      var d = JSON.parse(localStorage.getItem(K_DONE) || '[]');
      d.forEach(function (k) { manualSet.add(k); });
    } catch (e) {}
  }
  function persistDone() {
    try { localStorage.setItem(K_DONE, JSON.stringify(Array.from(manualSet))); } catch (e) {}
  }
  function notify() {
    listeners.forEach(function (fn) { try { fn(); } catch (e) {} });
  }

  var QuestDone = {
    /* 启动调用：把 localStorage 的手动标记读进内存 */
    init: function () { loadDone(); },
    isDone: function (key) { return !!key && (saveSet.has(key) || manualSet.has(key)); },
    /* 完成来源：save=存档（权威，不可取消） / manual=手动 / ''=未完成 */
    srcOf: function (key) {
      if (saveSet.has(key)) return 'save';
      if (manualSet.has(key)) return 'manual';
      return '';
    },
    toggle: function (key) {
      if (!key) return null;
      if (saveSet.has(key)) { /* 存档态不可手动取消 */ return { done: true, src: 'save' }; }
      var st;
      if (manualSet.has(key)) { manualSet.delete(key); st = { done: false, src: '' }; }
      else { manualSet.add(key); st = { done: true, src: 'manual' }; }
      persistDone();
      notify();
      return st;
    },
    /* 存档标志 → 任务完成集。flags: {flagName: true 表示已完成} */
    loadSave: function (flags) {
      saveSet = new Set();
      if (flags) {
        QUESTS.forEach(function (q) {
          if (q.flag && flags[q.flag]) saveSet.add(q.key);
        });
      }
      notify();
    },
    onChange: function (fn) { listeners.push(fn); },
    doneCount: function (cat) {
      var n = 0;
      QUESTS.forEach(function (q) { if ((!cat || q.cat === cat) && QuestDone.isDone(q.key)) n++; });
      return n;
    }
  };

  global.QuestDone = QuestDone;
})(window);
