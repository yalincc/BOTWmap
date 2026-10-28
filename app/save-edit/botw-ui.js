/*
 * botw-ui.js — BOTW 存档修改 UI（TOTKmap 风格双栏 + 六模块）
 * 依赖：marc-file.js / botw-data.js / botw-core.js / botw-icons.js
 */
(function () {
  'use strict';

  var BotWSave = window.BotWSave;
  var DATA = window.BOTW_DATA;
  var ICONS = window.BOTW_ICONS;

  var save = null;
  var originalBytes = null;
  var fileName = 'game_data.sav';
  var fileSize = 0;
  var fileModified = 0;
  var saveSource = 'file';
  var savePath = '';
  var saveSlot = '';
  var loadedAt = 0;
  var state = {
    module: 'info',
    equipCat: 'weapons',
    equipCols: 2,
    itemCat: 'ingredient',
    foodCat: 'dish',
    itemSearch: '',
    foodSearch: '',
    teleportEnabled: false,
    teleportAutoMap: true,
    changeOpen: false
  };
  var teleportOriginal = null;
  // 载入时的原始存档对象（解析一次常驻），改动明细靠它对比，避免每次输入重解析 1MB
  var origSave = null;
  var origIndex = {};
  var toastTimer = null;

  var $ = function (id) { return document.getElementById(id); };

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c];
    });
  }
  function clamp(n, min, max) {
    n = Number(n);
    if (!isFinite(n)) n = min;
    return Math.max(min, Math.min(max, Math.round(n)));
  }
  function num(v) { return Number(v) || 0; }
  function formatBytes(n) {
    n = Number(n) || 0;
    if (n >= 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
    if (n >= 1024) return Math.round(n / 1024) + ' KB';
    return n + ' B';
  }
  function formatTime(ms) {
    if (!ms) return '—';
    try {
      return new Date(ms).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
    } catch (e) { return '—'; }
  }
  function slotFromPath(path) {
    var m = String(path || '').match(/[\\/](\d+)[\\/]game_data\.sav$/i);
    return m ? m[1] : '';
  }

  function toast(msg, isErr, duration) {
    var t = $('toast');
    if (!t) return;
    t.textContent = msg;
    t.classList.toggle('err', !!isErr);
    t.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { t.classList.remove('show'); }, duration || 2600);
  }

  function iconHtml(id, size, cls) {
    var st = ICONS && ICONS.iconStyle ? ICONS.iconStyle(id, size) : null;
    var style = (st
      ? 'background-image:' + st.backgroundImage + ';background-position:' + st.backgroundPosition + ';background-size:' + st.backgroundSize + ';background-repeat:no-repeat;'
      : '') +
      'width:' + size + 'px;height:' + size + 'px;';
    var fallback = st ? '' : esc((DATA.NAMES[id] || id || '?').charAt(0));
    return '<div class="' + cls + '" style="' + style + '">' + fallback + '</div>';
  }

  function moduleHead(no, title, desc, badges) {
    return '<div class="module-head">' +
      '<div><div class="mh-kicker">MODULE ' + no + '</div>' +
      '<h2>' + esc(title) + '</h2><p>' + desc + '</p></div>' +
      '<div class="mh-badges">' + (badges || '') + '</div></div>';
  }

  function statTile(label, value, unit) {
    return '<div class="stat-tile"><div class="st-label">' + esc(label) + '</div>' +
      '<div class="st-value">' + esc(value) + '<span class="st-unit">' + esc(unit || '') + '</span></div></div>';
  }

  function getDirty() {
    if (!save || !originalBytes) return 0;
    return save.countChangedFields(originalBytes);
  }

  /* ================= 改动明细 ================= */
  function fmtRupee(n) { return Number(n || 0).toLocaleString('zh-CN'); }
  function posText(p) { return p.x.toFixed(2) + ' / ' + p.y.toFixed(2) + ' / ' + p.z.toFixed(2); }
  function qtyGroup(it) {
    if (it.cat === 'food') return '料理和药物';
    if (it.cat === 'ammo') return '箭矢';
    return '材料和道具';
  }

  // 结构化改动清单（原始档 vs 当前档）；面板与导出摘要共用这一个数据源
  // 每一项带 undo()，支持「只撤销这一项」
  function collectChanges() {
    var out = [];
    if (!save || !origSave) return out;

    if (origSave.getRupees() !== save.getRupees()) {
      var ru = origSave.getRupees();
      out.push({
        g: '基础数值', n: '卢比', from: fmtRupee(ru), to: fmtRupee(save.getRupees()),
        undo: function () { save.setRupees(ru); }
      });
    }
    if (origSave.getHearts() !== save.getHearts()) {
      var he = origSave.getHearts();
      out.push({
        g: '基础数值', n: '生命上限', from: he + ' 心', to: save.getHearts() + ' 心',
        undo: function () { save.setHearts(he); }
      });
    }
    if (origSave.getStaminaBits() !== save.getStaminaBits()) {
      var st = origSave.getStaminaBits();
      out.push({
        g: '基础数值', n: '精力上限',
        from: BotWSave.staminaLabel(st), to: BotWSave.staminaLabel(save.getStaminaBits()),
        undo: function () { save.setStaminaBits(st); }
      });
    }

    save.items.forEach(function (it) {
      var o = origIndex[it.gi];
      var name = save.nameOf(it.id);
      // 克洛格特例新增的物品没有「原值」，不给撤销按钮
      if (!o) { out.push({ g: qtyGroup(it), n: name, from: '—', to: '新增 ' + it.qty }); return; }

      if (o.qty !== it.qty) {
        var oq = o.qty;
        var undoQty = function () { save.setQuantity(it.gi, oq); it.qty = oq; };
        if (it.cat === 'weapons' || it.cat === 'bows' || it.cat === 'shields') {
          out.push({
            g: '装备强化', n: name + ' 耐久',
            from: BotWSave.durabilityToPercent(oq) + '%',
            to: BotWSave.durabilityToPercent(it.qty) + '%',
            undo: undoQty
          });
        } else {
          out.push({ g: qtyGroup(it), n: name, from: String(oq), to: String(it.qty), undo: undoQty });
        }
      }
      if (o.flags !== it.flags || o.flagsV !== it.flagsV) {
        var of = o.flags, ov = o.flagsV;
        var undoMod = function () {
          save.setModifier(it.cat, it.local, of, ov);
          it.flags = of;
          it.flagsV = ov;
        };
        if (o.flags !== it.flags) {
          out.push({
            g: '装备强化', n: name + ' 附加属性',
            from: BotWSave.modifierInfo(of).name,
            to: BotWSave.modifierInfo(it.flags).name,
            undo: undoMod
          });
        } else {
          out.push({
            g: '装备强化', n: name + ' 属性值',
            from: String(ov), to: String(it.flagsV),
            undo: undoMod
          });
        }
      }
    });

    var op = origSave.getPosition(), np = save.getPosition();
    if (op && np && (op.x !== np.x || op.y !== np.y || op.z !== np.z)) {
      var ox = op.x, oy = op.y, oz = op.z;
      out.push({
        g: '坐标瞬移', n: '坐标', from: posText(op), to: posText(np),
        undo: function () {
          save.setPosition(ox, oy, oz);
          save.setMap(origSave.getMap(), origSave.getMapType());
          save.setTeleportEnabled(false);
          state.teleportEnabled = false;
        }
      });
    }
    if (origSave.getMap() !== save.getMap() || origSave.getMapType() !== save.getMapType()) {
      var om = origSave.getMap(), ot = origSave.getMapType();
      out.push({
        g: '坐标瞬移', n: '地图格', from: om || '—', to: save.getMap() || '—',
        undo: function () { save.setMap(om, ot); }
      });
    }
    return out;
  }

  function changePanelHtml(list) {
    if (!list.length) return '';
    var order = [], groups = {};
    list.forEach(function (c, i) {
      c._i = i;
      if (!groups[c.g]) { groups[c.g] = []; order.push(c.g); }
      groups[c.g].push(c);
    });
    var body = order.map(function (g) {
      return '<div class="cgroup">' + esc(g) + '</div>' + groups[g].map(function (c) {
        var btn = c.undo
          ? '<button type="button" class="c-undo" data-undo="' + c._i + '" title="只还原这一项">撤销</button>'
          : '';
        return '<div class="citem"><span class="cn" title="' + esc(c.n) + '">' + esc(c.n) + '</span>' +
          '<span class="citem-right"><span class="cval">' + esc(c.from) + ' → ' + esc(c.to) + '</span>' + btn + '</span></div>';
      }).join('');
    }).join('');
    return '<div class="change-box' + (state.changeOpen ? ' open' : '') + '" id="changeBox">' +
      '<div class="change-head" id="changeHead" role="button" tabindex="0">改动明细（<b>' + list.length + '</b>）' +
      '<span class="ch-hint">' + (state.changeOpen ? '点击收起' : '点击展开核对 / 单项撤销') + '</span>' +
      '<span class="chev">›</span></div>' +
      '<div class="change-list">' + (state.changeOpen ? body : '') + '</div></div>';
  }

  // 只重绘明细面板，不碰模块内容 → 输入框焦点不丢
  function refreshChangePanel() {
    var slot = $('changeSlot');
    if (!slot) return;
    var list = collectChanges();
    var old = slot.querySelector('.change-list');
    var keep = old ? old.scrollTop : 0;
    slot.innerHTML = changePanelHtml(list);
    var box = slot.querySelector('.change-box');
    if (!box) return;
    var head = box.querySelector('.change-head');
    function toggle() { state.changeOpen = !state.changeOpen; refreshChangePanel(); }
    head.onclick = toggle;
    head.onkeydown = function (e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); } };

    box.querySelectorAll('.c-undo').forEach(function (btn) {
      btn.onclick = function (e) {
        e.stopPropagation();
        var target = list[Number(btn.getAttribute('data-undo'))];
        if (!target || !target.undo) return;
        target.undo();
        // 重建当前模块让输入框回填，但保留滚动位置（撤销时不跳回顶部）
        renderModule(true);
        updateDirty();
        toast('已撤销：' + target.n);
      };
    });

    var ls = box.querySelector('.change-list');
    if (ls) ls.scrollTop = keep;
  }

  function typeLabel(id) {
    if (/^Weapon_Sword_/.test(id)) return '单手剑';
    if (/^Weapon_Lsword_/.test(id)) return '双手剑';
    if (/^Weapon_Spear_/.test(id)) return '枪';
    if (/^Weapon_Bow_/.test(id)) return '弓';
    if (/^Weapon_Shield_/.test(id)) return '盾';
    return '装备';
  }

  function itemCatOf(it) {
    if (it.cat === 'ammo') return 'arrow';
    if (it.cat !== 'materials') return null;
    var id = it.id;
    if (/^Item_(Fruit|Mushroom(Get)?|Plant(Get)?|Meat|FishGet|Material)_/.test(id) || id === 'BeeHome') return 'ingredient';
    if (/^Item_Enemy_/.test(id)) return 'monster';
    if (/^Item_Ore_/.test(id)) return 'ore';
    if (/^(Animal_Insect_|Item_InsectGet_)/.test(id)) return 'bug';
    return 'other';
  }

  var ITEM_CATS = [
    { id: 'ingredient', name: '食材' },
    { id: 'monster', name: '怪物素材' },
    { id: 'ore', name: '矿石宝石' },
    { id: 'bug', name: '虫与生物' },
    { id: 'other', name: '其他' },
    { id: 'arrow', name: '箭矢' }
  ];

  function foodCatOf(it) {
    return /^Item_Cook_C_/.test(it.id) ? 'drug' : 'dish';
  }
  var FOOD_CATS = [
    { id: 'dish', name: '料理' },
    { id: 'drug', name: '药物' }
  ];

  function itemPool() {
    return save.items.filter(function (it) { return it.cat === 'materials' || it.cat === 'ammo'; });
  }
  function foodPool() {
    return save.items.filter(function (it) { return it.cat === 'food'; });
  }
  function equipPool() {
    return save.items.filter(function (it) { return it.cat === state.equipCat; });
  }

  function catCount(pool, catFn, id) {
    return pool.filter(function (it) { return catFn(it) === id; }).length;
  }

  /* ================= 导航 / 外壳 ================= */
  var MODULES = [
    { id: 'info', no: '01', title: '存档信息', sub: '游戏时长 / 卢比 / 生命 / 精力 / 克洛格', render: renderInfo },
    { id: 'items', no: '02', title: '材料和道具', sub: '食材 / 素材 / 矿石 / 虫 / 其他 / 箭矢', render: renderItems },
    { id: 'food', no: '03', title: '料理和药物', sub: '料理 / 药物', render: renderFood },
    { id: 'equip', no: '04', title: '装备强化', sub: '武器 / 弓 / 盾', render: renderEquip },
    { id: 'teleport', no: '05', title: '坐标瞬移', sub: '实验性 · 读档后生效', render: renderTeleport }
  ];

  function navCount(id) {
    if (!save) return '';
    if (id === 'info') return save.items.length + ' 件';
    if (id === 'items') return itemPool().length + ' 件';
    if (id === 'food') return foodPool().length + ' 件';
    if (id === 'equip') return save.items.filter(function (it) { return it.cat === 'weapons' || it.cat === 'bows' || it.cat === 'shields'; }).length + ' 件';
    if (id === 'teleport') return '实验';
    return '';
  }

  function renderNav() {
    var nav = $('moduleNav');
    nav.innerHTML = MODULES.map(function (m) {
      return '<button type="button" data-module="' + m.id + '" class="' + (state.module === m.id ? 'active' : '') + '"' + (!save ? ' disabled' : '') + '>' +
        '<span class="mn-no">' + m.no + '</span>' +
        '<span class="mn-main"><span class="mn-title">' + esc(m.title) + '</span><span class="mn-sub">' + esc(m.sub) + '</span></span>' +
        '<span class="mn-count">' + esc(navCount(m.id)) + '</span>' +
        '</button>';
    }).join('');
    nav.querySelectorAll('button').forEach(function (b) {
      b.onclick = function () {
        if (!save) return;
        state.module = b.getAttribute('data-module');
        closeRail();
        renderNav();
        renderModule();
      };
    });
  }

  function renderSaveCard() {
    var el = $('saveCard');
    if (!save) {
      el.innerHTML = '<div class="sc-empty">未载入存档</div>';
      return;
    }
    var n = getDirty();
    var nonStandard = fileName !== 'game_data.sav';
    var sourceBadge = saveSource === 'xnavi'
      ? '<span class="badge xnavi">xnavi' + (saveSlot ? ' · 槽 ' + esc(saveSlot) : '') + '</span>'
      : '';
    el.innerHTML = '<div class="sc-file" title="' + esc(savePath || fileName) + '">' + esc(fileName) + '</div>' +
      '<div class="sc-meta">v' + esc(save.version) + ' · ' + esc(save.platform) + (fileSize ? ' · ' + formatBytes(fileSize) : '') + '</div>' +
      '<div class="sc-chips"><span class="badge dim">' + save.items.length + ' 件</span>' +
      '<span class="badge ' + (n ? '' : 'ok') + '">' + (n ? n + ' 项修改' : '未修改') + '</span>' +
      sourceBadge +
      (nonStandard ? '<span class="badge warn">非标准文件名</span>' : '') + '</div>';
  }

  function updateDirty() {
    var n = getDirty();
    var line = $('dirtyLine');
    if (!save) {
      line.textContent = '未载入存档';
      line.classList.remove('dirty');
    } else {
      line.textContent = n ? (n + ' 项修改待导出') : '没有修改';
      line.classList.toggle('dirty', n > 0);
    }
    $('btnDownload').disabled = !save || n === 0;
    $('btnMobileDownload').disabled = !save || n === 0;
    $('btnReset').disabled = !save || n === 0;
    $('btnBackup').disabled = !save;
    var hb = $('headDirty');
    if (hb) {
      hb.textContent = n ? n + ' 项修改' : '未修改';
      hb.className = 'badge ' + (n ? '' : 'ok');
    }
    var ds = $('dirtyState');
    if (ds) ds.textContent = n ? n + ' 项待导出' : '没有修改';
    renderSaveCard();
    refreshChangePanel();
  }

  function renderModule(keepScroll) {
    var view = $('moduleView');
    if (!save) {
      view.classList.add('hidden');
      $('emptyState').classList.remove('hidden');
      var slot = $('changeSlot');
      if (slot) slot.innerHTML = '';
      return;
    }
    $('emptyState').classList.add('hidden');
    view.classList.remove('hidden');
    var m = MODULES.filter(function (x) { return x.id === state.module; })[0] || MODULES[0];
    var keep = keepScroll ? $('main').scrollTop : 0;
    m.render();
    $('main').scrollTop = keep;
  }

  function renderAll() {
    renderNav();
    renderSaveCard();
    renderModule();
    updateDirty();
  }

  function openRail() {
    $('rail').classList.add('open');
    $('railScrim').classList.add('show');
  }
  function closeRail() {
    $('rail').classList.remove('open');
    $('railScrim').classList.remove('show');
  }

  /* ================= 01 存档信息（概览 + 基础数值修改） ================= */
  function renderInfo() {
    var pos = save.getPosition();
    var dirty = getDirty();
    var korokItem = save.getKorokItem();
    var bits = save.getStaminaBits();
    var badges = '<span class="badge dim">v' + esc(save.version) + ' · ' + esc(save.platform) + '</span>' +
      '<span id="headDirty" class="badge ' + (dirty ? '' : 'ok') + '">' + (dirty ? dirty + ' 项修改' : '未修改') + '</span>';
    var nameWarn = fileName !== 'game_data.sav'
      ? '<div class="warn-box">当前载入的是 <b>' + esc(fileName) + '</b>，不是标准的 <b>game_data.sav</b>。测试副本 / 备份改了不会进游戏；请确认载入的是模拟器实际读取的存档。</div>'
      : '';

    var staminaOpts = BotWSave.staminaPresets().map(function (p) {
      return '<option value="' + p.bits + '"' + (p.bits === bits ? ' selected' : '') + '>' + esc(p.label) + '</option>';
    }).join('');
    if (!BotWSave.staminaPresets().some(function (p) { return p.bits === bits; })) {
      staminaOpts = '<option value="' + bits + '" selected>当前值（自定义）</option>' + staminaOpts;
    }

    var korokTile;
    if (korokItem) {
      korokTile = '<div class="tile-field"><div class="tf-k"><label for="inpKorok">克洛格果实</label></div>' +
        '<input id="inpKorok" type="number" min="0" max="900" value="' + korokItem.qty + '">' +
        '<div class="tf-h">背包持有数量 · 0–900</div></div>';
    } else {
      korokTile = '<div class="tile-field"><div class="tf-k">克洛格果实</div>' +
        '<div class="tf-h">背包里没有（可能已全交给伯库林）</div>' +
        '<button id="btnAddKorok" class="btn ghost mini" type="button" style="margin-top:6px">放入 1 个</button></div>';
    }

    $('moduleView').innerHTML =
      moduleHead('01', '存档信息', '只读概览 + 基础数值修改；只改上限和持有数量，不碰任务、神庙、事件标记。', badges) +
      nameWarn +
      '<div class="panel-card">' +
      '<div class="card-head"><h3>基础数值</h3><span class="card-sub">改完自动进上方「改动明细」</span></div>' +
      '<div class="info-grid">' +
      '<div class="stat-tile"><div class="st-label">游戏时长</div>' +
      '<div class="st-value">' + esc(BotWSave.formatPlaytime(save.getPlaytime())) + '</div></div>' +
      '<div class="tile-field"><div class="tf-k"><label for="inpRupees">卢比</label></div>' +
      '<input id="inpRupees" type="number" min="0" max="999999" value="' + save.getRupees() + '">' +
      '<div class="tf-h">0–999999</div></div>' +
      '<div class="tile-field"><div class="tf-k"><label for="inpHearts">生命上限</label></div>' +
      '<input id="inpHearts" type="number" min="1" max="30" value="' + save.getHearts() + '">' +
      '<div class="tf-h">1–30 心；进游戏按上限回满</div></div>' +
      '<div class="tile-field"><div class="tf-k"><label for="inpStamina">精力上限</label></div>' +
      '<select id="inpStamina">' + staminaOpts + '</select>' +
      '<div class="tf-h">耐力圈，1/5 圈步进</div></div>' +
      korokTile +
      '</div>' +
      '<p class="note"><b>克洛格果实</b>改的是背包持有数量，不是收集进度；不会补全未收集的克洛格。</p>' +
      '</div>' +
      '<div class="panel-card">' +
      '<div class="card-head"><h3>存档文件</h3><span class="card-sub">核对是不是当前存档槽</span></div>' +
      '<div class="info-chips">' +
      '<span class="badge dim">武器 ' + save.items.filter(function (x) { return x.cat === 'weapons'; }).length + '</span>' +
      '<span class="badge dim">弓 ' + save.items.filter(function (x) { return x.cat === 'bows'; }).length + '</span>' +
      '<span class="badge dim">盾 ' + save.items.filter(function (x) { return x.cat === 'shields'; }).length + '</span>' +
      '<span class="badge dim">材料 ' + save.items.filter(function (x) { return x.cat === 'materials'; }).length + '</span>' +
      '<span class="badge dim">料理 ' + save.items.filter(function (x) { return x.cat === 'food'; }).length + '</span>' +
      '<span class="badge dim">箭矢 ' + save.items.filter(function (x) { return x.cat === 'ammo'; }).length + '</span>' +
      '</div>' +
      '<div class="file-card">' +
      '<div class="file-row"><span class="k">存档文件</span><span class="v">' + esc(fileName) + '</span></div>' +
      '<div class="file-row"><span class="k">来源</span><span class="v">' + (saveSource === 'xnavi' ? 'xnavi 最新档' + (saveSlot ? ' · 槽 ' + esc(saveSlot) : '') : '手动选择') + '</span></div>' +
      (savePath ? '<div class="file-row"><span class="k">路径</span><span class="v" title="' + esc(savePath) + '">' + esc(savePath) + '</span></div>' : '') +
      '<div class="file-row"><span class="k">版本 / 平台</span><span class="v">v' + esc(save.version) + ' · ' + esc(save.platform) + '</span></div>' +
      '<div class="file-row"><span class="k">文件大小 / 时间</span><span class="v">' + (fileSize ? formatBytes(fileSize) : '—') + ' · ' + (saveSource === 'xnavi' ? '读取于 ' + formatTime(loadedAt) : formatTime(fileModified)) + '</span></div>' +
      '<div class="file-row"><span class="k">背包物品</span><span class="v">' + save.items.length + ' 件</span></div>' +
      '<div class="file-row"><span class="k">当前地图</span><span class="v">' + esc(save.getMapType() || '—') + (pos && save.getMapType() === 'MainField' ? ' · ' + esc(BotWSave.guessMapGrid(pos.x, pos.z)) : '') + '</span></div>' +
      '<div class="file-row"><span class="k">修改状态</span><span class="v" id="dirtyState">' + (dirty ? dirty + ' 项待导出' : '没有修改') + '</span></div>' +
      '</div>' +
      '<p class="note">Eden / Yuzu 的 <b>0–7</b> 是不同存档槽；载入后看上面的「游戏时长」，和游戏内读档界面一致的那个槽才是当前存档。改错槽位不会生效。</p>' +
      '<p class="note">改完 → 左下角「下载修改档」；下载后：<b>完全关闭模拟器</b> → 覆盖对应槽位的 game_data.sav → 启动读档。运行中替换会被退出保存覆盖。</p>' +
      '</div>';

    function bindNum(id, setter, getter) {
      var el = $(id);
      if (!el) return;
      el.addEventListener('input', function () {
        if (el.value === '') return;
        setter(el.value);
        updateDirty();
      });
      el.addEventListener('blur', function () { el.value = getter(); });
    }
    bindNum('inpRupees', function (v) { save.setRupees(v); }, function () { return save.getRupees(); });
    bindNum('inpHearts', function (v) { save.setHearts(v); }, function () { return save.getHearts(); });
    bindNum('inpKorok', function (v) { save.setKorokHold(v); }, function () { return save.getKorokHold(); });
    $('inpStamina').addEventListener('change', function () {
      save.setStaminaBits(Number(this.value));
      updateDirty();
    });
    var addBtn = $('btnAddKorok');
    if (addBtn) {
      addBtn.addEventListener('click', function () {
        if (!window.confirm('背包里没有克洛格果实。要往空槽放入 1 个吗？\n这是“添加物品”的唯一特例，其他物品仍然不能添加。')) return;
        var item = save.ensureKorokItem();
        if (!item) { toast('没有可用的空槽，无法放入克洛格果实', true); return; }
        renderAll();
        toast('已放入 1 个克洛格果实，可继续调整数量');
      });
    }
  }

  /* ================= 02 材料和道具 ================= */
  function currentItemShown() {
    var pool = itemPool();
    if (state.itemSearch) {
      var q = state.itemSearch.toLowerCase();
      return pool.filter(function (it) {
        return (save.nameOf(it.id) || '').toLowerCase().indexOf(q) >= 0 || it.id.toLowerCase().indexOf(q) >= 0;
      });
    }
    return pool.filter(function (it) { return itemCatOf(it) === state.itemCat; });
  }

  function updateItemResults() {
    var shown = currentItemShown();
    $('itemGrid').innerHTML = itemTilesHtml(shown, 'material');
    $('itemCount').textContent = shown.length + ' 件';
    bindQtyInputs();
  }

  function renderItems() {
    var pool = itemPool();
    var counts = {};
    ITEM_CATS.forEach(function (c) { counts[c.id] = catCount(pool, itemCatOf, c.id); });

    var tabs = '<div class="tabs">' + ITEM_CATS.map(function (c) {
      // 搜索时是跨全部类别搜，不能还亮着某个分类，否则会误导
      return '<button type="button" class="tab ' + (state.itemCat === c.id && !state.itemSearch ? 'active' : '') + '" data-cat="' + c.id + '">' +
        esc(c.name) + '<span class="tab-count">' + counts[c.id] + '</span></button>';
    }).join('') + '</div>';

    var shown = currentItemShown();

    $('moduleView').innerHTML =
      moduleHead('02', '材料和道具', '六类分组；只显示背包里已有的物品，数量 0–999。',
        '<span class="badge dim">' + pool.length + ' 件</span>') +
      '<div class="panel-card">' + tabs +
      '<div class="toolbar-row"><div class="search">' +
      '<input id="itemSearch" type="text" placeholder="搜索食材 / 素材 / 箭矢…" value="' + esc(state.itemSearch) + '">' +
      (state.itemSearch ? '<button type="button" class="search-clear" id="itemClear" aria-label="清空搜索">×</button>' : '') +
      '</div>' +
      (state.itemSearch ? '<span class="badge">全部类别</span>' : '') +
      '<span class="badge dim" id="itemCount">' + shown.length + ' 件</span></div>' +
      '<div id="itemGrid">' + itemTilesHtml(shown, 'material') + '</div>' +
      '<p class="note">数量直接写背包堆叠数；格子上「+10 / +100 / 满」是快捷步进，改错了可在顶部改动明细里单项撤销。</p>' +
      '</div>';

    $('moduleView').querySelectorAll('.tab').forEach(function (b) {
      b.onclick = function () {
        state.itemCat = b.getAttribute('data-cat');
        state.itemSearch = '';
        renderItems();
      };
    });
    $('itemSearch').addEventListener('input', function () {
      var was = !!state.itemSearch;
      state.itemSearch = this.value;
      // 首次输入 / 清空回到分类浏览：tab 高亮和清空按钮要跟着变，重绘一次并还原光标
      if (!!state.itemSearch !== was) {
        var caret = this.selectionStart;
        renderItems();
        var el = $('itemSearch');
        if (el) { el.focus(); try { el.setSelectionRange(caret, caret); } catch (e) { /* ignore */ } }
        return;
      }
      updateItemResults();
    });
    var clearItem = $('itemClear');
    if (clearItem) {
      clearItem.onclick = function () {
        state.itemSearch = '';
        renderItems();
        var el = $('itemSearch');
        if (el) el.focus();
      };
    }
    bindQtyInputs();
  }

  // 与载入时原始档对比，标出改过的格子
  function isItemChanged(it) {
    var o = origIndex[it.gi];
    return o ? o.qty !== it.qty : true;
  }

  function itemTilesHtml(items, kind) {
    if (!items.length) return '<div class="empty-mini">这个分类里没有背包现有的物品。</div>';
    var nameCounts = {};
    items.forEach(function (it) {
      var n = save.nameOf(it.id);
      nameCounts[n] = (nameCounts[n] || 0) + 1;
    });
    return '<div class="item-grid">' + items.map(function (it) {
      var name = save.nameOf(it.id);
      // 副标题只用来区分同名物品（#槽号），类别已由上方 tab 表达，不再重复
      var sub = nameCounts[name] > 1 ? '#' + it.gi : '';
      return '<div class="itile' + (isItemChanged(it) ? ' changed' : '') + '" data-gi="' + it.gi + '" title="' + esc(name + ' · ' + it.id) + '">' +
        '<div class="itile-top">' + iconHtml(it.id, 32, 'item-icon') +
        '<div style="min-width:0"><div class="itile-name">' + esc(name) + '</div>' +
        (sub ? '<div class="itile-sub">' + esc(sub) + '</div>' : '') +
        '</div></div>' +
        '<div class="itile-step">' +
        '<button type="button" class="step-btn js-step" data-step="-1" aria-label="' + esc(name) + ' 数量减一">−</button>' +
        '<input class="js-qty" type="number" inputmode="numeric" min="0" max="999" value="' + it.qty + '" aria-label="' + esc(name) + ' 数量">' +
        '<button type="button" class="step-btn js-step" data-step="1" aria-label="' + esc(name) + ' 数量加一">+</button>' +
        '</div>' +
        '<div class="itile-quick">' +
        '<button type="button" class="q-btn js-step" data-step="10" aria-label="' + esc(name) + ' 加 10">+10</button>' +
        '<button type="button" class="q-btn js-step" data-step="100" aria-label="' + esc(name) + ' 加 100">+100</button>' +
        '<button type="button" class="q-btn js-step" data-step="999" aria-label="' + esc(name) + ' 拉满 999">满</button>' +
        '</div></div>';
    }).join('') + '</div>';
  }

  function bindQtyInputs() {
    $('moduleView').querySelectorAll('.itile').forEach(function (tile) {
      var gi = Number(tile.getAttribute('data-gi'));
      var it = save.items.filter(function (x) { return x.gi === gi; })[0];
      if (!it) return;
      var inp = tile.querySelector('.js-qty');
      function commit(v) {
        v = clamp(v, 0, 999);
        save.setQuantity(gi, v);
        it.qty = v;
        tile.classList.toggle('changed', isItemChanged(it));
        updateDirty();
        return v;
      }
      if (inp) {
        inp.addEventListener('input', function () {
          if (inp.value === '') return;
          commit(inp.value);
        });
        inp.addEventListener('blur', function () { inp.value = it.qty; });
      }
      tile.querySelectorAll('.js-step').forEach(function (btn) {
        btn.addEventListener('click', function () {
          var step = Number(btn.getAttribute('data-step')) || 1;
          var v = commit(it.qty + step);
          if (inp) inp.value = v;
        });
      });
    });
  }

  /* ================= 03 料理和药物 ================= */
  function currentFoodShown() {
    var pool = foodPool();
    if (state.foodSearch) {
      var q = state.foodSearch.toLowerCase();
      return pool.filter(function (it) {
        return (save.nameOf(it.id) || '').toLowerCase().indexOf(q) >= 0 || it.id.toLowerCase().indexOf(q) >= 0;
      });
    }
    return pool.filter(function (it) { return foodCatOf(it) === state.foodCat; });
  }

  function updateFoodResults() {
    var shown = currentFoodShown();
    $('itemGrid').innerHTML = itemTilesHtml(shown, 'food');
    $('itemCount').textContent = shown.length + ' 件';
    bindQtyInputs();
  }

  function renderFood() {
    var pool = foodPool();
    var counts = {};
    FOOD_CATS.forEach(function (c) { counts[c.id] = catCount(pool, foodCatOf, c.id); });
    var tabs = '<div class="tabs">' + FOOD_CATS.map(function (c) {
      return '<button type="button" class="tab ' + (state.foodCat === c.id && !state.foodSearch ? 'active' : '') + '" data-cat="' + c.id + '">' +
        esc(c.name) + '<span class="tab-count">' + counts[c.id] + '</span></button>';
    }).join('') + '</div>';

    var shown = currentFoodShown();

    $('moduleView').innerHTML =
      moduleHead('03', '料理和药物', '只改数量；不改食谱、效果或图标。',
        '<span class="badge dim">' + pool.length + ' 件</span>') +
      '<div class="panel-card">' + tabs +
      '<div class="toolbar-row"><div class="search">' +
      '<input id="foodSearch" type="text" placeholder="搜索料理 / 药物…" value="' + esc(state.foodSearch) + '">' +
      (state.foodSearch ? '<button type="button" class="search-clear" id="foodClear" aria-label="清空搜索">×</button>' : '') +
      '</div>' +
      (state.foodSearch ? '<span class="badge">全部类别</span>' : '') +
      '<span class="badge dim" id="itemCount">' + shown.length + ' 件</span></div>' +
      '<div id="itemGrid">' + itemTilesHtml(shown, 'food') + '</div>' +
      '<p class="note">药物：精灵回力水、药剂（Item_Cook_C_16 / C_17 / _Mod*）；其余烤、冰、煮、料理都归料理。</p>' +
      '</div>';

    $('moduleView').querySelectorAll('.tab').forEach(function (b) {
      b.onclick = function () {
        state.foodCat = b.getAttribute('data-cat');
        state.foodSearch = '';
        renderFood();
      };
    });
    $('foodSearch').addEventListener('input', function () {
      var was = !!state.foodSearch;
      state.foodSearch = this.value;
      if (!!state.foodSearch !== was) {
        var caret = this.selectionStart;
        renderFood();
        var el = $('foodSearch');
        if (el) { el.focus(); try { el.setSelectionRange(caret, caret); } catch (e) { /* ignore */ } }
        return;
      }
      updateFoodResults();
    });
    var clearFood = $('foodClear');
    if (clearFood) {
      clearFood.onclick = function () {
        state.foodSearch = '';
        renderFood();
        var el = $('foodSearch');
        if (el) el.focus();
      };
    }
    bindQtyInputs();
  }

  /* ================= 04 装备强化 ================= */
  function modSelectHtml(it) {
    var opts = BotWSave.modifierOptions(it.cat);
    var normal = [], star = [];
    opts.forEach(function (m) {
      if (m.value === 0) normal.unshift(m);
      else if ((m.value & 0x80000000) !== 0) star.push(m);
      else normal.push(m);
    });
    function option(m) {
      return '<option value="' + m.value + '"' + (m.value === it.flags ? ' selected' : '') + '>' + esc(m.name) + '</option>';
    }
    var html = '<select class="ec-mod-select" aria-label="附加属性">';
    if (normal.length) html += '<optgroup label="普通">' + normal.map(option).join('') + '</optgroup>';
    if (star.length) html += '<optgroup label="★ 强化">' + star.map(option).join('') + '</optgroup>';
    var known = opts.some(function (m) { return m.value === it.flags; });
    if (!known) html += '<optgroup label="当前"><option value="' + it.flags + '" selected>' + esc(BotWSave.modifierInfo(it.flags).name) + '</option></optgroup>';
    html += '</select>';
    return html;
  }

  function isEquipTouched(it) {
    var o = origIndex[it.gi];
    return !o || o.qty !== it.qty || o.flags !== it.flags || o.flagsV !== it.flagsV;
  }

  function renderEquip() {
    var tabs = ['weapons', 'bows', 'shields'].map(function (cat) {
      var label = cat === 'weapons' ? '武器' : cat === 'bows' ? '弓' : '盾';
      var count = save.items.filter(function (it) { return it.cat === cat; }).length;
      return '<button type="button" class="tab ' + (state.equipCat === cat ? 'active' : '') + '" data-cat="' + cat + '">' +
        label + '<span class="tab-count">' + count + '</span></button>';
    }).join('');
    var items = equipPool();

    var cards = items.length ? items.map(function (it) {
      var info = BotWSave.modifierInfo(it.flags);
      var mode = BotWSave.modifierValueMode(it.flags);
      var pct = BotWSave.durabilityToPercent(it.qty);
      var valueDisabled = mode === 'none' ? ' disabled' : '';
      var valueMax = mode === 'bonus' ? 999 : 4294967295;
      var valueHint = mode === 'none' ? '该属性不需要数值' : mode === 'advanced' ? '高级数值（游戏内倍率）' : '攻击 / 耐久 / 防御加成';
      var modBadge = it.flags === 0 ? '<span class="badge dim">无属性</span>' : '<span class="badge ec-mod">' + esc(info.name) + '</span>';
      return '<article class="equip-card' + (isEquipTouched(it) ? ' changed' : '') + '" data-gi="' + it.gi + '">' +
        '<div class="ec-head">' + iconHtml(it.id, 44, 'ec-icon') +
        '<div class="ec-title"><div class="ec-name">' + esc(save.nameOf(it.id)) + '</div>' +
        '<div class="ec-sub">' + esc(typeLabel(it.id)) + ' · 本地 #' + (it.local + 1) + '</div></div>' +
        modBadge + '</div>' +
        '<div class="ec-controls">' +
        '<div class="ec-row"><span class="ec-label">附加属性</span>' + modSelectHtml(it) + '</div>' +
        '<div class="ec-row"><span class="ec-label">数值</span><input class="ec-mod-value" type="number" min="0" max="' + valueMax + '" value="' + it.flagsV + '"' + valueDisabled + '></div>' +
        '<div class="value-hint">' + esc(valueHint) + '</div>' +
        '<div class="ec-row"><span class="ec-label">耐久</span><div class="dur-wrap">' +
        '<input class="ec-dur-range" type="range" min="0" max="100" value="' + pct + '" aria-label="耐久百分比">' +
        '<input class="ec-dur-num" type="number" min="0" max="100" value="' + pct + '" aria-label="耐久百分比数值">' +
        '<span class="unit">%</span><button class="mini-btn ec-full" type="button">满</button>' +
        '</div></div>' +
        '</div></article>';
    }).join('') : '<div class="empty-mini">背包里没有这一类装备。</div>';

    $('moduleView').innerHTML =
      moduleHead('04', '装备强化', '只列出背包现有装备；属性按分类本地序号读写，弓/盾不会再错位。',
        '<span class="badge dim">' + items.length + ' 件</span>') +
      '<div class="panel-card">' +
      '<div class="card-head" style="margin-bottom:8px"><div class="tabs" style="margin:0">' + tabs + '</div>' +
      '<div class="head-right"><button type="button" id="btnAllFullDur" class="btn ghost mini" title="把本类全部装备耐久设为 100%（可在改动明细里逐项撤销）">耐久全满</button>' +
      '<div class="view-toggle"><button type="button" class="seg ' + (state.equipCols === 1 ? 'active' : '') + '" data-cols="1">单列</button>' +
      '<button type="button" class="seg ' + (state.equipCols === 2 ? 'active' : '') + '" data-cols="2">双列</button></div></div></div>' +
      '<div class="equip-grid cols-' + state.equipCols + '">' + cards + '</div>' +
      '<p class="note">耐久 100% = 存档满值 65500；五连射、暴击、冲浪不需要数值；远投/速射的数值是游戏内隐藏倍率，不确定就保持默认。</p>' +
      '</div>';

    $('moduleView').querySelectorAll('.tab').forEach(function (b) {
      b.onclick = function () {
        state.equipCat = b.getAttribute('data-cat');
        renderEquip();
      };
    });
    $('moduleView').querySelectorAll('.seg').forEach(function (b) {
      b.onclick = function () {
        state.equipCols = Number(b.getAttribute('data-cols')) || 1;
        renderEquip();
      };
    });
    var allFull = $('btnAllFullDur');
    if (allFull) {
      allFull.onclick = function () {
        var pool = equipPool();
        var maxQty = BotWSave.percentToDurability(100);
        var n = 0;
        pool.forEach(function (it) {
          if (it.qty === maxQty) return;
          save.setQuantity(it.gi, maxQty);
          it.qty = maxQty;
          n++;
        });
        if (!n) { toast('本类装备耐久已经全是 100%'); return; }
        renderEquip();
        updateDirty();
        toast('已把 ' + n + ' 件装备耐久设为 100%，改动明细里可逐项撤销');
      };
    }
    bindEquipCards();
  }

  function bindEquipCards() {
    $('moduleView').querySelectorAll('.equip-card').forEach(function (card) {
      var gi = Number(card.getAttribute('data-gi'));
      var it = save.items.filter(function (x) { return x.gi === gi; })[0];
      if (!it) return;
      var select = card.querySelector('.ec-mod-select');
      var value = card.querySelector('.ec-mod-value');
      var range = card.querySelector('.ec-dur-range');
      var durNum = card.querySelector('.ec-dur-num');
      var full = card.querySelector('.ec-full');
      function syncCard() { card.classList.toggle('changed', isEquipTouched(it)); }

      select.addEventListener('change', function () {
        var newFlags = Number(select.value) >>> 0;
        var oldFlags = it.flags >>> 0;
        var oldBase = oldFlags & 0x7fffffff;
        var newBase = newFlags & 0x7fffffff;
        var newMode = BotWSave.modifierValueMode(newFlags);
        var oldMode = BotWSave.modifierValueMode(oldFlags);
        var newValue;
        if (newFlags === 0) newValue = 0;
        else if (newBase === oldBase) newValue = it.flagsV;
        else if (newMode === 'bonus') newValue = oldMode === 'bonus' ? Math.min(999, it.flagsV) : 10;
        else if (newMode === 'advanced') newValue = BotWSave.modifierDefaultValue(newFlags);
        else newValue = 0;
        save.setModifier(it.cat, it.local, newFlags, newValue);
        it.flags = newFlags;
        it.flagsV = newValue;

        // 局部更新这张卡：不重建列表，滚动位置与焦点都保住
        value.value = newValue;
        value.disabled = newMode === 'none';
        value.max = newMode === 'bonus' ? 999 : 4294967295;
        value.min = 0;
        var hintEl = card.querySelector('.value-hint');
        if (hintEl) {
          hintEl.textContent = newMode === 'none' ? '该属性不需要数值'
            : newMode === 'advanced' ? '高级数值（游戏内倍率）' : '攻击 / 耐久 / 防御加成';
        }
        var badgeEl = card.querySelector('.ec-head > .badge');
        if (badgeEl) {
          badgeEl.className = 'badge ' + (newFlags === 0 ? 'dim' : 'ec-mod');
          badgeEl.textContent = newFlags === 0 ? '无属性' : BotWSave.modifierInfo(newFlags).name;
        }
        syncCard();
        updateDirty();
      });

      value.addEventListener('input', function () {
        if (value.value === '') return;
        var mode = BotWSave.modifierValueMode(it.flags);
        if (mode === 'none') return;
        var max = mode === 'bonus' ? 999 : 4294967295;
        var v = clamp(value.value, 0, max);
        save.setModifier(it.cat, it.local, it.flags, v);
        it.flagsV = v;
        syncCard();
        updateDirty();
      });
      value.addEventListener('blur', function () { value.value = it.flagsV; });

      function applyDur(pct) {
        pct = clamp(pct, 0, 100);
        var qty = BotWSave.percentToDurability(pct);
        save.setQuantity(it.gi, qty);
        it.qty = qty;
        range.value = pct;
        durNum.value = pct;
        syncCard();
        updateDirty();
      }
      range.addEventListener('input', function () { applyDur(range.value); });
      durNum.addEventListener('input', function () { if (durNum.value !== '') applyDur(durNum.value); });
      durNum.addEventListener('blur', function () { durNum.value = BotWSave.durabilityToPercent(it.qty); });
      full.addEventListener('click', function () { applyDur(100); });
    });
  }

  /* ================= 05 坐标瞬移（实验） ================= */
  function renderTeleport() {
    var pos = save.getPosition();
    var map = save.getMap();
    var mapType = save.getMapType();
    var can = !!pos && mapType === 'MainField';
    var grid = pos ? BotWSave.guessMapGrid(pos.x, pos.z) : '—';
    var disabled = (!can || !state.teleportEnabled) ? ' disabled' : '';
    var warning = can
      ? '<div class="warn-box">改的是存档坐标，导出后读档才会生效，不是游戏运行中的瞬间移动。坐标错误可能掉入虚空/水中，改前先备份。</div>'
      : '<div class="warn-box">当前存档不在大地图（' + esc(mapType || '未知') + '），坐标瞬移暂不支持。请先回到大地图再保存。</div>';

    $('moduleView').innerHTML =
      moduleHead('05', '坐标瞬移', '实验模块：先只改 X/Y/Z；测试通过后再决定是否保留。',
        '<span class="badge warn">实验</span>') +
      warning +
      '<div class="panel-card">' +
      '<div class="readonly-row"><span>当前坐标：<b>' + (pos ? pos.x.toFixed(2) + ' / ' + pos.y.toFixed(2) + ' / ' + pos.z.toFixed(2) : '—') + '</b></span>' +
      '<span>地图格：<b>' + esc(grid) + '</b></span><span>地图类型：<b>' + esc(mapType || '—') + '</b></span><span>地图名：<b>' + esc(map || '—') + '</b></span></div>' +
      '<label class="switch-row"><input type="checkbox" id="tpEnable"' + (state.teleportEnabled ? ' checked' : '') + (can ? '' : ' disabled') + '> 启用坐标修改（实验）</label>' +
      '<div class="teleport-grid">' +
      '<div class="coord-field"><label for="tpX">X</label><input id="tpX" type="number" step="0.1" value="' + (pos ? pos.x.toFixed(2) : '') + '"' + disabled + '></div>' +
      '<div class="coord-field"><label for="tpY">Y（高度）</label><input id="tpY" type="number" step="0.1" value="' + (pos ? pos.y.toFixed(2) : '') + '"' + disabled + '></div>' +
      '<div class="coord-field"><label for="tpZ">Z</label><input id="tpZ" type="number" step="0.1" value="' + (pos ? pos.z.toFixed(2) : '') + '"' + disabled + '></div>' +
      '</div>' +
      '<label class="switch-row"><input type="checkbox" id="tpAutoMap"' + (state.teleportAutoMap ? ' checked' : '') + (can ? '' : ' disabled') + '> 同时更新地图格（MainField）</label>' +
      '<div class="toolbar-row"><button type="button" id="tpApply" class="btn gold"' + (can && state.teleportEnabled ? '' : ' disabled') + '>应用坐标</button>' +
      '<button type="button" id="tpCurrent" class="btn ghost"' + (can ? '' : ' disabled') + '>填当前坐标</button></div>' +
      '<p class="note">先只写 XYZ 并自动计算地图格；如果进游戏落地不正确，再考虑补地图类型。地图格按 x/z 估算：<b>' + esc(grid) + '</b>。</p>' +
      '</div>';

    var enable = $('tpEnable');
    enable.addEventListener('change', function () {
      state.teleportEnabled = enable.checked;
      if (!state.teleportEnabled) {
        // 还原到载入时的坐标/地图
        if (teleportOriginal) {
          save.setPosition(teleportOriginal.x, teleportOriginal.y, teleportOriginal.z);
          save.setMap(teleportOriginal.map, teleportOriginal.mapType);
        }
        save.setTeleportEnabled(false);
        updateDirty();
      }
      renderTeleport();
    });
    $('tpAutoMap').addEventListener('change', function () {
      state.teleportAutoMap = this.checked;
    });
    $('tpCurrent').addEventListener('click', function () {
      var p = pos;
      if (!p) return;
      $('tpX').value = p.x.toFixed(2);
      $('tpY').value = p.y.toFixed(2);
      $('tpZ').value = p.z.toFixed(2);
    });
    $('tpApply').addEventListener('click', function () {
      var x = Number($('tpX').value), y = Number($('tpY').value), z = Number($('tpZ').value);
      if (!isFinite(x) || !isFinite(y) || !isFinite(z)) { toast('坐标必须是数字', true); return; }
      if (Math.abs(x) > 10000 || Math.abs(z) > 10000 || y < -1000 || y > 3000) { toast('坐标超出合理范围，已拒绝', true); return; }
      if (!save.setPosition(x, y, z)) { toast('写入坐标失败', true); return; }
      if (state.teleportAutoMap) {
        if (!save.setMap(BotWSave.guessMapGrid(x, z), 'MainField')) { toast('写入地图格失败', true); return; }
      }
      save.setTeleportEnabled(true);
      updateDirty();
      renderTeleport();
      toast('坐标已写入，导出后读档测试');
    });
  }

  /* ================= 载入 / 导出 ================= */
  function loadBytes(u8, name, meta) {
    if (!(u8 instanceof Uint8Array)) u8 = new Uint8Array(u8);
    var s = BotWSave.fromBuffer(u8);
    if (!s.load()) {
      toast('无法识别的存档（版本 / 端序不匹配，或不是 game_data.sav）', true);
      return false;
    }
    save = s;
    originalBytes = u8.slice();
    // 原始档单独解析一份常驻：改动明细靠它与当前档对比，不重复读 1MB
    origSave = BotWSave.fromBuffer(originalBytes.slice());
    origIndex = {};
    if (origSave.load()) {
      origSave.items.forEach(function (it) { origIndex[it.gi] = it; });
    } else {
      origSave = null;
    }
    fileName = name || 'game_data.sav';
    fileSize = u8.length;
    fileModified = meta && meta.modified ? meta.modified : 0;
    saveSource = (meta && meta.source) || 'file';
    savePath = (meta && meta.path) || '';
    saveSlot = (meta && meta.slot) || slotFromPath(savePath);
    loadedAt = Date.now();
    state.module = 'info';
    state.equipCat = 'weapons';
    state.equipCols = window.innerWidth < 1180 ? 1 : 2;
    state.itemCat = 'ingredient';
    state.foodCat = 'dish';
    state.itemSearch = '';
    state.foodSearch = '';
    state.teleportEnabled = false;
    state.teleportAutoMap = true;
    state.changeOpen = false;
    save.setTeleportEnabled(false);
    var p = save.getPosition();
    teleportOriginal = p ? { x: p.x, y: p.y, z: p.z, map: save.getMap(), mapType: save.getMapType() } : null;
    renderAll();
    toast('已载入 v' + save.version + ' · ' + save.platform + ' · ' + save.items.length + ' 件物品');
    return true;
  }

  function loadFile(file) {
    if (!file) return;
    var reader = new FileReader();
    reader.onload = function () {
      loadBytes(new Uint8Array(reader.result), file.name || 'game_data.sav', { size: file.size, modified: file.lastModified });
    };
    reader.readAsArrayBuffer(file);
  }

  function downloadBytes(bytes, name) {
    var blob = new Blob([bytes], { type: 'application/octet-stream' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a);
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 200);
  }

  // 导出摘要：与改动明细共用同一份结构化清单
  function summarizeChanges() {
    return collectChanges().map(function (c) { return c.n + ' ' + c.from + ' → ' + c.to; });
  }

  function downloadBackup() {
    if (!originalBytes) return;
    downloadBytes(originalBytes, 'game_data_original.sav');
    toast('已下载原档备份 game_data_original.sav（' + formatBytes(originalBytes.length) + '）');
  }

  function downloadModified() {
    if (!save) return;
    var check = save.validateDiff(originalBytes);
    if (!check.ok) {
      toast('导出已中止：检测到白名单外的改动（' + check.bad.length + ' 字节），请反馈', true);
      console.error('[BOTW] blocked diff', check.bad.slice(0, 40));
      return;
    }
    if (check.diff === 0) { toast('没有修改，无需导出'); return; }
    var changes = summarizeChanges();
    downloadBytes(save.getArrayBuffer(), 'game_data.sav');
    var summary = changes.length
      ? changes.slice(0, 3).join('；') + (changes.length > 3 ? '；+ ' + (changes.length - 3) + ' 项' : '')
      : '无修改';
    toast('已下载 game_data.sav（' + formatBytes(fileSize) + '）：' + summary + '。请关闭模拟器后覆盖对应槽位，再重新载入确认。', false, 6500);
  }

  function resetChanges() {
    if (!originalBytes) return;
    var s = BotWSave.fromBuffer(originalBytes.slice());
    if (!s.load()) { toast('还原失败', true); return; }
    save = s;
    save.setTeleportEnabled(false);
    state.teleportEnabled = false;
    var p = save.getPosition();
    teleportOriginal = p ? { x: p.x, y: p.y, z: p.z, map: save.getMap(), mapType: save.getMapType() } : null;
    renderAll();
    toast('已还原到载入时状态');
  }

  /* ================= xnavi 最新存档 ================= */
  function naviSaveUrl() {
    if (location.protocol === 'file:') return null;
    // 由 xnavi 自己托管 /botw/ 时，同源 /save 最稳（端口被顺延也能用）
    if (location.pathname.indexOf('/botw/') === 0 || location.port === '8766') return '/save';
    return 'http://127.0.0.1:8766/save';
  }

  function setNaviBusy(on) {
    ['btnFromNavi', 'btnFromNavi2'].forEach(function (id) {
      var b = $(id);
      if (!b) return;
      b.disabled = !!on;
      b.textContent = on ? '正在读取 xnavi…' : '从 xnavi 读取最新档';
    });
  }

  function loadFromNavi() {
    var url = naviSaveUrl();
    var notice = $('naviNotice');
    if (!url) {
      if (notice) notice.classList.remove('hidden');
      toast('当前是 file://，不能连本机 xnavi；请用 http://127.0.0.1:8766/botw/save-edit/editor.html 打开。', true, 6500);
      return;
    }
    if (notice) notice.classList.add('hidden');
    setNaviBusy(true);
    toast('正在向 xnavi 要最新存档…', false, 4000);
    var ctrl = window.AbortController ? new AbortController() : null;
    var timer = ctrl ? setTimeout(function () { ctrl.abort(); }, 5000) : null;
    fetch(url + '?t=' + Date.now(), { cache: 'no-store', signal: ctrl ? ctrl.signal : undefined })
      .then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        var path = '';
        try { path = r.headers.get('X-Save-Path') || ''; } catch (e) { /* CORS 未暴露 */ }
        return r.arrayBuffer().then(function (ab) { return { ab: ab, path: path }; });
      })
      .then(function (out) {
        if (timer) clearTimeout(timer);
        setNaviBusy(false);
        var ok = loadBytes(new Uint8Array(out.ab), 'game_data.sav', {
          source: 'xnavi',
          path: out.path,
          slot: slotFromPath(out.path),
          modified: Date.now()
        });
        if (!ok) return;
        toast('已从 xnavi 读取最新档' + (saveSlot ? ' · 槽 ' + saveSlot : '') +
          ' · ' + BotWSave.formatPlaytime(save.getPlaytime()) + ' · ' + save.items.length + ' 件', false, 5000);
      })
      .catch(function (err) {
        if (timer) clearTimeout(timer);
        setNaviBusy(false);
        toast('连不上 xnavi。确认 xnavi 已启动；或用「打开存档」手动选文件。' +
          (err && err.name === 'AbortError' ? '（超时）' : ''), true, 6500);
      });
  }

  /* ================= 初始化 ================= */
  function bindShell() {
    $('btnOpen').onclick = function () { $('fileInput').click(); };
    $('btnOpen2').onclick = function (e) { e.stopPropagation(); $('fileInput').click(); };
    if ($('btnFromNavi')) $('btnFromNavi').onclick = loadFromNavi;
    if ($('btnFromNavi2')) $('btnFromNavi2').onclick = function (e) { e.stopPropagation(); loadFromNavi(); };
    $('fileInput').onchange = function (e) {
      var f = e.target.files && e.target.files[0];
      if (f) loadFile(f);
      e.target.value = '';
    };
    $('btnDownload').onclick = downloadModified;
    $('btnMobileDownload').onclick = downloadModified;
    $('btnBackup').onclick = downloadBackup;
    $('btnReset').onclick = resetChanges;
    $('btnRail').onclick = openRail;
    $('btnRailClose').onclick = closeRail;
    $('railScrim').onclick = closeRail;
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeRail(); });

    var dz = $('dropzone');
    dz.addEventListener('click', function () { $('fileInput').click(); });
    ['dragover', 'dragenter'].forEach(function (ev) {
      dz.addEventListener(ev, function (e) { e.preventDefault(); dz.classList.add('drag'); });
    });
    ['dragleave', 'drop'].forEach(function (ev) {
      dz.addEventListener(ev, function (e) { e.preventDefault(); dz.classList.remove('drag'); });
    });
    dz.addEventListener('drop', function (e) {
      var f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
      if (f) loadFile(f);
    });
    document.addEventListener('dragover', function (e) { e.preventDefault(); });
    document.addEventListener('drop', function (e) { e.preventDefault(); });
  }

  function init() {
    bindShell();
    renderNav();
    updateDirty();
  }

  window.BOTW_EDITOR = {
    loadBytes: loadBytes,
    getSave: function () { return save; },
    getOriginal: function () { return originalBytes; },
    getChanges: collectChanges,
    downloadModified: downloadModified,
    downloadBackup: downloadBackup,
    reset: resetChanges,
    setModule: function (id) { if (save) { state.module = id; renderNav(); renderModule(); } }
  };

  init();
})();
