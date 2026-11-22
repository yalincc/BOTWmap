/* ============================================================================
 * BOTWmap · 独立网页端实时语音导航模块 (app/voice/voice-nav.js)
 * ----------------------------------------------------------------------------
 * 极简架构：
 *   1. 单一全局 Audio 实例顺序播放本地高清 48kbps MP3，物理上杜绝双声/重音；
 *      无线上回退、无系统 TTS 兜底，播不了静默跳过。
 *   2. 罗盘 8 方位 + 真 3D 距离 + 海拔高低差（alt_up / alt_down）+ 偏航纠偏。
 *   3. 2 档离线角色音色：塞尔达风 (zelda) / 御姐音 (yujie)；英文界面自动切 en/。
 *   4. 独立弹窗 #voiceBox（点击顶栏「语音」打开），与 #settingsBox 互斥。
 * ============================================================================ */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.VoiceNav = factory();
  }
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var STORAGE_KEY = 'botwmap.voice.cfg.v1';
  var BASE_PATH = 'voice/';

  var DEFAULT_CFG = {
    enabled: true,         // 语音导航总开关
    pack: 'zelda',         // 'zelda' | 'yujie'
    volume: 85,            // 音量 10 ~ 100
    dirDist: true,         // 播报罗盘 8 方位与距离
    altAlert: true,        // 播报高低差（目标在上方/下方）
    offCourse: true,       // 偏航提醒
    arriveM: 35,           // 到达距离阈值（默认 35m）
    altDiffM: 25,          // 高低差阈值（默认 ±25m）
    freqMode: 'standard'   // 'compact'(100/25m) | 'standard'(300/100/35m) | 'dense'(500/300/100/50m)
  };

  var FREQ_PRESETS = {
    compact:  { arriveM: 25, altDiffM: 30 },
    standard: { arriveM: 35, altDiffM: 25 },
    dense:    { arriveM: 50, altDiffM: 20 }
  };

  var cfg = loadCfg();

  /* 内部状态机 */
  var state = {
    targetKey: '',
    passed500: false,
    passed300: false,
    passed100: false,
    passedArrive: false,
    lastDist: null,
    lastPx: null,
    lastPy: null,
    offCourseSince: 0,
    lastOffCourseSpeak: 0,
    lastSpokenClips: [],
    lastPlaybackPack: ''
  };

  var OFF_COURSE_WAIT = 6000;
  var OFF_COURSE_COOLDOWN = 22000;
  var MILESTONE_RESET_HYST = 80;
  var externalPaused = false;
  var autoplayWarned = false;

  /* 唯一全局音频播放器（单实例，物理上不可能同时发出两重声音） */
  var audioEl = (typeof Audio !== 'undefined') ? new Audio() : null;
  var playQueue = [];
  var queueTimer = null;

  function getArriveM() {
    var v = parseInt(cfg.arriveM, 10);
    return (isNaN(v) || v < 10) ? 35 : Math.min(80, Math.max(15, v));
  }

  function getAltDiffM() {
    var v = parseInt(cfg.altDiffM, 10);
    return (isNaN(v) || v < 5) ? 25 : Math.min(60, Math.max(10, v));
  }

  /* ---------------- 配置存取与宿主桥接 ---------------- */
  function loadCfg() {
    var res = {};
    for (var k in DEFAULT_CFG) res[k] = DEFAULT_CFG[k];
    try {
      var raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        var parsed = JSON.parse(raw);
        for (var pk in DEFAULT_CFG) {
          if (parsed[pk] !== undefined) res[pk] = parsed[pk];
        }
        if (res.pack !== 'zelda' && res.pack !== 'yujie') res.pack = 'zelda';
      }
    } catch (e) {}
    return res;
  }

  function saveCfg() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg)); } catch (e) {}
    syncUI();
  }

  function isEn() {
    return !!(typeof window !== 'undefined' && window.I18N && window.I18N.getLang && window.I18N.getLang() === 'en');
  }

  function tr(key, zh, en) {
    if (typeof window !== 'undefined' && window.I18N && window.I18N.t) {
      var v = window.I18N.t(key, null);
      if (v && v !== key) return v;
    }
    return isEn() ? en : zh;
  }

  function showHostToast(msg) {
    if (typeof window !== 'undefined' && window.UI && typeof window.UI.toast === 'function') {
      window.UI.toast(msg);
    }
  }

  /* ---------------- 罗盘 8 方位、真 3D 距离与海拔高度差 ---------------- */
  function calcBearingDeg(px, py, tx, ty) {
    var dE = tx - px;
    var dN = py - ty;
    return (Math.atan2(dE, dN) * 180 / Math.PI + 360) % 360;
  }

  function bearingToDirClip(deg) {
    deg = ((deg % 360) + 360) % 360;
    if (deg >= 337.5 || deg < 22.5)  return 'dir_n';
    if (deg >= 22.5  && deg < 67.5)  return 'dir_ne';
    if (deg >= 67.5  && deg < 112.5) return 'dir_e';
    if (deg >= 112.5 && deg < 157.5) return 'dir_se';
    if (deg >= 157.5 && deg < 202.5) return 'dir_s';
    if (deg >= 202.5 && deg < 247.5) return 'dir_sw';
    if (deg >= 247.5 && deg < 292.5) return 'dir_w';
    return 'dir_nw';
  }

  function calcCompass8(px, py, tx, ty) {
    return bearingToDirClip(calcBearingDeg(px, py, tx, ty));
  }

  function calcDistM(player, target) {
    if (!player || !target || player.x == null || target.x == null || player.y == null || target.y == null) return null;
    var dxM = (target.x - player.x) * 0.5;
    var dyM = (target.y - player.y) * 0.5;
    var dzM = (player.z != null && target.z != null && isFinite(Number(player.z)) && isFinite(Number(target.z)))
      ? (Number(target.z) - Number(player.z))
      : 0;
    return Math.sqrt(dxM * dxM + dyM * dyM + dzM * dzM);
  }

  function pickDistClip(d) {
    if (d == null || !isFinite(d)) return null;
    if (d <= getArriveM()) return 'arrived';
    if (d <= 75)   return 'dist_50';
    if (d <= 125)  return 'dist_100';
    if (d <= 175)  return 'dist_150';
    if (d <= 250)  return 'dist_200';
    if (d <= 350)  return 'dist_300';
    if (d <= 450)  return 'dist_400';
    if (d <= 650)  return 'dist_500';
    if (d <= 1000) return 'dist_800';
    return 'dist_far';
  }

  function pickAltClip(player, target) {
    if (!cfg.altAlert) return null;
    if (!player || !target || player.z == null || target.z == null) return null;
    var dz = Number(target.z) - Number(player.z);
    if (!isFinite(dz)) return null;
    var th = getAltDiffM();
    if (dz >= th)  return 'alt_up';
    if (dz <= -th) return 'alt_down';
    return null;
  }

  /* ---------------- 极简单实例音频队列播放器 ---------------- */
  function stopPlayback() {
    playQueue = [];
    if (queueTimer) { clearTimeout(queueTimer); queueTimer = null; }
    if (audioEl) {
      audioEl.onended = null;
      audioEl.onerror = null;
      try { audioEl.pause(); audioEl.currentTime = 0; } catch (e) {}
    }
  }

  function pumpQueue() {
    if (!cfg.enabled || externalPaused || !audioEl || !playQueue.length) return;
    var item = playQueue.shift();
    var src = BASE_PATH + item.pack + '/' + item.clipId + '.mp3';

    audioEl.onended = function () {
      queueTimer = setTimeout(pumpQueue, 25);
    };
    audioEl.onerror = function () {
      queueTimer = setTimeout(pumpQueue, 25);
    };
    audioEl.volume = Math.max(0.05, Math.min(1, (cfg.volume || 85) / 100));
    audioEl.src = src;

    try {
      var pr = audioEl.play();
      if (pr && typeof pr.catch === 'function') {
        pr.catch(function (err) {
          if (err && err.name === 'NotAllowedError') {
            playQueue = [];
            if (!autoplayWarned) {
              autoplayWarned = true;
              showHostToast(tr('voice.autoplayHint', '浏览器限制自动语音，请点击地图任意处解锁', 'Browser blocked auto audio — click anywhere on the map to unlock'));
            }
          }
        });
      }
    } catch (e) {}
  }

  function speakClips(clips, opts) {
    opts = opts || {};
    if (!cfg.enabled || externalPaused || !clips || !clips.length) return;

    state.lastSpokenClips = clips.slice();
    var pack = isEn() ? 'en' : ((cfg.pack === 'yujie') ? 'yujie' : 'zelda');
    state.lastPlaybackPack = pack;

    if (opts.interrupt !== false) {
      stopPlayback();
    }

    var wasIdle = !playQueue.length && (!audioEl || audioEl.paused || audioEl.ended);
    for (var i = 0; i < clips.length; i++) {
      playQueue.push({ pack: pack, clipId: clips[i] });
    }
    if (opts.interrupt !== false || wasIdle) {
      pumpQueue();
    }
  }

  /* ---------------- 核心导航语音组装逻辑 ---------------- */
  function buildTargetAnnounceClips(prefixClip, player, target, d) {
    var clips = [];
    if (prefixClip) clips.push(prefixClip);

    var arrM = getArriveM();
    if (cfg.dirDist && player && target && player.x != null && target.x != null) {
      if (d != null && d <= arrM) {
        clips.push('arrived');
      } else {
        clips.push(calcCompass8(player.x, player.y, target.x, target.y));
        var distClip = pickDistClip(d);
        if (distClip) clips.push(distClip);
      }
    }
    var altClip = pickAltClip(player, target);
    if (altClip) clips.push(altClip);
    return clips;
  }

  function pickArriveExtraClip(target) {
    var tType = String((target && target.type) || '').toLowerCase();
    var tName = String((target && target.name) || '');
    if (tType === 'korok_flower') return 'korok_flower';
    if (tType === 'korok_race') return 'korok_race';
    if (tType === 'seed' || tType.indexOf('korok') >= 0 || tName.indexOf('克洛格') >= 0 || tName.toLowerCase().indexOf('korok') >= 0) {
      return 'korok_near';
    }
    if (tType === 'shrine' || tType.indexOf('shrine') >= 0 || tName.indexOf('神庙') >= 0 || tName.toLowerCase().indexOf('shrine') >= 0) {
      return 'shrine_near';
    }
    if (tType === 'chest' || tType === 'treasure' || tName.indexOf('宝箱') >= 0 || tName.toLowerCase().indexOf('chest') >= 0) {
      return 'chest_near';
    }
    if (tType === 'equip' || tType === 'weapon' || tType === 'armor' || tType === 'camp') {
      return 'equip_near';
    }
    return null;
  }

  function resetTargetState(tKey, d) {
    var arrM = getArriveM();
    state.targetKey = tKey || '';
    state.passed500 = (d != null && d <= 500);
    state.passed300 = (d != null && d <= 300);
    state.passed100 = (d != null && d <= 100);
    state.passedArrive = (d != null && d <= arrM);
    state.lastDist = d;
    state.offCourseSince = 0;
  }

  function feed(payload) {
    if (!payload) return;
    var ev = payload.event || null;
    var player = payload.player || null;
    var target = payload.target || null;

    if (ev === 'clear') {
      state.targetKey = '';
      stopPlayback();
      return;
    }

    if (ev === 'all_done') {
      state.targetKey = '';
      if (cfg.enabled) speakClips(['all_done'], { interrupt: true });
      return;
    }

    if (!target || target.x == null || target.y == null) {
      state.targetKey = '';
      return;
    }

    var tKey = Math.round(target.x) + ',' + Math.round(target.y) + ',' + (target.id || target.name || '');
    var hasPlayer = !!(player && player.located !== false && player.x != null && player.y != null);
    var d = hasPlayer ? calcDistM(player, target) : null;

    if (ev === 'start_nav' || ev === 'start_auto' || ev === 'next_target' || ev === 'switch_near') {
      resetTargetState(tKey, d);
      if (hasPlayer) {
        speakClips(buildTargetAnnounceClips(ev, player, target, d), { interrupt: true });
        state.lastPx = player.x;
        state.lastPy = player.y;
      } else {
        speakClips([ev], { interrupt: true });
      }
      return;
    }

    if (!hasPlayer) return;

    if (tKey !== state.targetKey) {
      resetTargetState(tKey, d);
      speakClips(buildTargetAnnounceClips('start_nav', player, target, d), { interrupt: true });
      state.lastPx = player.x;
      state.lastPy = player.y;
      return;
    }

    if (d == null) return;
    var arrM = getArriveM();

    if (state.passedArrive && d > arrM + MILESTONE_RESET_HYST) state.passedArrive = false;
    if (state.passed100 && d > 100 + MILESTONE_RESET_HYST) state.passed100 = false;
    if (state.passed300 && d > 300 + MILESTONE_RESET_HYST) state.passed300 = false;
    if (state.passed500 && d > 500 + MILESTONE_RESET_HYST) state.passed500 = false;

    if (d <= arrM && !state.passedArrive) {
      state.passedArrive = true;
      state.passed100 = true;
      state.passed300 = true;
      state.passed500 = true;
      var arrClips = ['arrived'];
      var extraClip = pickArriveExtraClip(target);
      if (extraClip) arrClips.push(extraClip);
      var arrAlt = pickAltClip(player, target);
      if (arrAlt) arrClips.push(arrAlt);
      speakClips(arrClips, { interrupt: true });
    } else if (d <= 100 && !state.passed100) {
      state.passed100 = true;
      state.passed300 = true;
      state.passed500 = true;
      var c100 = [];
      if (cfg.dirDist) {
        c100.push(calcCompass8(player.x, player.y, target.x, target.y));
        c100.push('dist_100');
      }
      var alt100 = pickAltClip(player, target);
      if (alt100) c100.push(alt100);
      if (c100.length) speakClips(c100, { interrupt: false });
    } else if (d <= 300 && !state.passed300 && cfg.freqMode !== 'compact') {
      state.passed300 = true;
      state.passed500 = true;
      if (cfg.dirDist) {
        speakClips([calcCompass8(player.x, player.y, target.x, target.y), 'dist_300'], { interrupt: false });
      }
    } else if (d <= 500 && !state.passed500 && cfg.freqMode === 'dense') {
      state.passed500 = true;
      if (cfg.dirDist) {
        speakClips([calcCompass8(player.x, player.y, target.x, target.y), 'dist_500'], { interrupt: false });
      }
    }

    var now = Date.now();
    if (cfg.offCourse && d > 60 && state.lastPx != null && state.lastPy != null) {
      var moveE = player.x - state.lastPx;
      var moveN = state.lastPy - player.y;
      var moveStepM = Math.sqrt(moveE * moveE + moveN * moveN) * 0.5;
      if (moveStepM >= 1.2 && moveStepM <= 150) {
        var moveHeading = (Math.atan2(moveE, moveN) * 180 / Math.PI + 360) % 360;
        var targetBearing = calcBearingDeg(player.x, player.y, target.x, target.y);
        var angleDiff = Math.abs(((moveHeading - targetBearing) % 360 + 540) % 360 - 180);
        if (angleDiff > 120) {
          if (!state.offCourseSince) {
            state.offCourseSince = now;
          } else if (now - state.offCourseSince >= OFF_COURSE_WAIT && now - state.lastOffCourseSpeak >= OFF_COURSE_COOLDOWN) {
            state.lastOffCourseSpeak = now;
            state.offCourseSince = now;
            speakClips(['off_course', bearingToDirClip(targetBearing)], { interrupt: false });
          }
        } else if (angleDiff < 85) {
          state.offCourseSince = 0;
        }
      }
    }

    state.lastPx = player.x;
    state.lastPy = player.y;
    state.lastDist = d;
  }

  function testPlay() {
    if (!cfg.enabled) {
      cfg.enabled = true;
      saveCfg();
    }
    var demoClips = cfg.altAlert ? ['welcome', 'dir_ne', 'dist_100', 'alt_up'] : ['welcome', 'dir_ne', 'dist_100'];
    speakClips(demoClips, { interrupt: true });
  }

  /* ---------------- 独立语音设置弹窗 #voiceBox ---------------- */
  function injectStyles() {
    if (typeof document === 'undefined' || document.getElementById('botw-voice-nav-css')) return;
    var st = document.createElement('style');
    st.id = 'botw-voice-nav-css';
    st.textContent =
      '#voiceBox{position:absolute;z-index:26;width:340px;max-width:calc(100vw - 24px);right:12px;top:66px;' +
      'background:var(--panel-solid,#121c14);border:1px solid var(--gold-dim,#9a7b4f);border-radius:var(--radius,10px);' +
      'box-shadow:var(--shadow,0 12px 32px rgba(0,0,0,.55));padding:14px 16px;max-height:calc(100vh - 90px);overflow-y:auto;}' +
      '#voiceBox.hidden{display:none;}' +
      '#voiceBox .vn-desc{font-size:12px;color:var(--text-dim,#8fa390);margin:4px 0 12px;line-height:1.45;}' +
      '#voiceBox .vn-row{display:flex;align-items:center;justify-content:space-between;gap:8px;margin:10px 0;}' +
      '#voiceBox .vn-row .st-label{margin-bottom:0;font-size:13.5px;}' +
      '#voiceBox .vn-detail{display:flex;flex-direction:column;gap:10px;transition:opacity .15s;margin-top:8px;padding-top:10px;border-top:1px dashed rgba(255,255,255,.1);}' +
      '#voiceBox .vn-detail.off{opacity:.42;pointer-events:none;}' +
      '#voiceBox .vn-sub-row{display:flex;align-items:center;gap:8px;flex-wrap:nowrap;}' +
      '#voiceBox .vn-slabel{font-size:12.5px;color:var(--text-dim,#8fa390);white-space:nowrap;min-width:56px;}' +
      '#voiceBox .vn-vol-slider{flex:1;min-width:80px;accent-color:var(--gold,#f2c94c);cursor:pointer;height:4px;}' +
      '#voiceBox .vn-vol-val{width:36px;text-align:right;font-variant-numeric:tabular-nums;color:var(--gold-lite,#ffd978);font-size:12px;}' +
      '#voiceBox .vn-test-btn{background:rgba(242,201,76,.15);border:1px solid var(--gold-dim,#9a7b4f);color:var(--gold-lite,#ffd978);border-radius:6px;padding:4px 10px;font-size:12px;cursor:pointer;white-space:nowrap;transition:.15s;}' +
      '#voiceBox .vn-test-btn:hover{background:rgba(242,201,76,.3);color:#fff;}' +
      '#voiceBox .vn-freq-sel{flex:1;min-width:0;background:#132016;border:1px solid var(--border,#2d4731);border-radius:6px;color:var(--text,#e8eee8);font-size:12px;padding:5px 8px;outline:none;cursor:pointer;}' +
      '#voiceBox .vn-freq-sel option{background:#132016;color:#e8eee8;}' +
      '#voiceBox .vn-packs{display:flex;gap:8px;flex:1;}' +
      '#voiceBox .vn-packs .st-opt{flex:1;text-align:center;padding:5px 8px;font-size:12.5px;white-space:nowrap;}' +
      '#voiceBox .vn-foot{margin-top:12px;padding-top:10px;border-top:1px solid rgba(255,255,255,.08);display:flex;justify-content:flex-end;}' +
      '@media (max-width:760px){#voiceBox{right:10px;left:10px;width:auto;max-width:none;top:8px;}}';
    document.head.appendChild(st);
  }

  function openVoiceBox() {
    var box = document.getElementById('voiceBox');
    if (!box) return;
    var stBox = document.getElementById('settingsBox');
    if (stBox) stBox.classList.add('hidden');
    var stBtn = document.getElementById('btnSettings');
    if (stBtn) stBtn.classList.remove('active');
    box.classList.remove('hidden');
    syncUI();
  }

  function closeVoiceBox() {
    var box = document.getElementById('voiceBox');
    if (box) box.classList.add('hidden');
  }

  function toggleVoiceBox() {
    var box = document.getElementById('voiceBox');
    if (!box) return;
    if (box.classList.contains('hidden')) openVoiceBox();
    else closeVoiceBox();
  }

  function mountVoiceBoxUI() {
    if (typeof document === 'undefined') return;
    var mapArea = document.getElementById('mapArea') || document.body;
    if (document.getElementById('voiceBox')) return;

    var box = document.createElement('div');
    box.id = 'voiceBox';
    box.className = 'hidden';
    box.innerHTML =
      '<div class="sb-head"><span id="vnSetTitle">语音导航</span>' +
        '<button type="button" id="vnBoxClose" class="card-close" title="关闭">×</button>' +
      '</div>' +
      '<div class="vn-desc" id="vnSetHint">罗盘 8 方位 + 海拔高度差实时语音指路（塞尔达 / 御姐离线语音）</div>' +
      '<div class="vn-row">' +
        '<span class="st-label" id="vnEnableLbl">语音播报</span>' +
        '<div class="st-options" id="vnSetToggleOpts">' +
          '<button type="button" class="st-opt" data-v="on" id="vnToggleOn">开启</button>' +
          '<button type="button" class="st-opt" data-v="off" id="vnToggleOff">关闭</button>' +
        '</div>' +
      '</div>' +
      '<div class="vn-detail" id="vnSetDetail">' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnPackLbl">音色选择</span>' +
          '<div class="st-options vn-packs" id="vnSetPackBtns">' +
            '<button type="button" class="st-opt" data-pack="zelda" id="vnBtnZelda">塞尔达</button>' +
            '<button type="button" class="st-opt" data-pack="yujie" id="vnBtnYujie">御姐音</button>' +
          '</div>' +
        '</div>' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnVolLabel">音量</span>' +
          '<input type="range" id="vnVolRange" class="vn-vol-slider" min="10" max="100" step="5" value="85">' +
          '<span id="vnVolText" class="vn-vol-val">85%</span>' +
          '<button type="button" id="vnSetTestBtn" class="vn-test-btn">▶ 试听</button>' +
        '</div>' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnAltLbl">高低差</span>' +
          '<div class="st-options" id="vnAltOpts">' +
            '<button type="button" class="st-opt" data-v="on" id="vnAltOn">开启</button>' +
            '<button type="button" class="st-opt" data-v="off" id="vnAltOff">关闭</button>' +
          '</div>' +
        '</div>' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnFreqLbl">播报频次</span>' +
          '<select id="vnFreqMode" class="vn-freq-sel">' +
            '<option value="compact">精简（100 / 25m）</option>' +
            '<option value="standard">标准（300 / 100 / 35m）</option>' +
            '<option value="dense">详细（500 / 300 / 100 / 50m）</option>' +
          '</select>' +
        '</div>' +
      '</div>' +
      '<div class="vn-foot">' +
        '<button type="button" id="vnResetBtn" class="mini">恢复默认</button>' +
      '</div>';

    mapArea.appendChild(box);

    var topBtn = document.getElementById('btnVoice');
    if (topBtn && !topBtn.dataset.vnBound) {
      topBtn.dataset.vnBound = '1';
      topBtn.addEventListener('click', function (e) {
        e.stopPropagation();
        toggleVoiceBox();
      });
    }

    var stBtn = document.getElementById('btnSettings');
    if (stBtn) stBtn.addEventListener('click', function () { closeVoiceBox(); });

    var closeBtn = document.getElementById('vnBoxClose');
    if (closeBtn) closeBtn.addEventListener('click', function () { closeVoiceBox(); });

    box.addEventListener('click', function (e) { e.stopPropagation(); });
    document.addEventListener('click', function (e) {
      if (box.classList.contains('hidden')) return;
      if (topBtn && topBtn.contains(e.target)) return;
      if (!box.contains(e.target)) closeVoiceBox();
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !box.classList.contains('hidden')) closeVoiceBox();
    });

    var onBtn = document.getElementById('vnToggleOn');
    var offBtn = document.getElementById('vnToggleOff');
    if (onBtn) onBtn.addEventListener('click', function () { setEnabled(true, true); });
    if (offBtn) offBtn.addEventListener('click', function () { setEnabled(false, true); });

    var altOn = document.getElementById('vnAltOn');
    var altOff = document.getElementById('vnAltOff');
    if (altOn) altOn.addEventListener('click', function () { cfg.altAlert = true; saveCfg(); });
    if (altOff) altOff.addEventListener('click', function () { cfg.altAlert = false; saveCfg(); });

    var packBtns = box.querySelectorAll('#vnSetPackBtns .st-opt');
    for (var i = 0; i < packBtns.length; i++) {
      packBtns[i].addEventListener('click', function () {
        cfg.enabled = true;
        cfg.pack = this.getAttribute('data-pack');
        saveCfg();
        testPlay();
      });
    }

    var volRange = document.getElementById('vnVolRange');
    if (volRange) {
      volRange.addEventListener('input', function () {
        cfg.volume = parseInt(this.value, 10) || 85;
        var vt = document.getElementById('vnVolText');
        if (vt) vt.textContent = cfg.volume + '%';
        if (audioEl) audioEl.volume = Math.max(0.05, Math.min(1, cfg.volume / 100));
        saveCfg();
      });
    }

    var testBtn = document.getElementById('vnSetTestBtn');
    if (testBtn) testBtn.addEventListener('click', function () { testPlay(); });

    var selFreq = document.getElementById('vnFreqMode');
    if (selFreq) {
      selFreq.addEventListener('change', function () {
        var fm = this.value || 'standard';
        var preset = FREQ_PRESETS[fm] || FREQ_PRESETS.standard;
        cfg.freqMode = fm;
        cfg.arriveM = preset.arriveM;
        cfg.altDiffM = preset.altDiffM;
        saveCfg();
      });
    }

    var resetBtn = document.getElementById('vnResetBtn');
    if (resetBtn) {
      resetBtn.addEventListener('click', function () {
        resetConfig();
        showHostToast(tr('voice.resetToast', '已恢复语音默认设置', 'Voice settings reset to default'));
      });
    }
  }

  function syncUI() {
    if (typeof document === 'undefined') return;
    var en = isEn();

    var topBtn = document.getElementById('btnVoice');
    if (topBtn) {
      topBtn.classList.toggle('active', !!cfg.enabled);
      var ico = topBtn.querySelector('.ico');
      var lbl = topBtn.querySelector('.label');
      if (ico) ico.textContent = cfg.enabled ? '🔊' : '🔇';
      if (lbl) lbl.textContent = tr('tool.voice', '语音', 'Voice');
      topBtn.setAttribute('title', tr('tool.voice.title', '打开语音导航设置', 'Open voice navigation settings'));
    }

    var setTitle = document.getElementById('vnSetTitle');
    var setHint = document.getElementById('vnSetHint');
    var enLbl = document.getElementById('vnEnableLbl');
    var onBtn = document.getElementById('vnToggleOn');
    var offBtn = document.getElementById('vnToggleOff');
    if (setTitle) setTitle.textContent = tr('voice.setTitle', '语音导航', 'Voice Navigation');
    if (setHint) setHint.textContent = tr('voice.setHint', '罗盘 8 方位 + 海拔高度差实时语音指路（塞尔达 / 御姐离线语音）', '8-compass + altitude voice guidance (Zelda / Mature offline packs)');
    if (enLbl) enLbl.textContent = tr('voice.enableLbl', '语音播报', 'Voice Guidance');
    if (onBtn) {
      onBtn.textContent = tr('voice.on', '开启', 'On');
      onBtn.classList.toggle('sel', !!cfg.enabled);
    }
    if (offBtn) {
      offBtn.textContent = tr('voice.off', '关闭', 'Off');
      offBtn.classList.toggle('sel', !cfg.enabled);
    }

    var detail = document.getElementById('vnSetDetail');
    if (detail) detail.classList.toggle('off', !cfg.enabled);

    var packLbl = document.getElementById('vnPackLbl');
    if (packLbl) packLbl.textContent = tr('voice.packLbl', '音色选择', 'Voice');

    var btnZelda = document.getElementById('vnBtnZelda');
    var btnYujie = document.getElementById('vnBtnYujie');
    if (btnZelda) btnZelda.textContent = tr('voice.pack.zeldaShort', '塞尔达', 'Zelda');
    if (btnYujie) btnYujie.textContent = tr('voice.pack.yujieShort', '御姐音', 'Mature');

    var packBtns = document.querySelectorAll('#vnSetPackBtns .st-opt');
    var activePack = (cfg.pack === 'yujie') ? 'yujie' : 'zelda';
    for (var i = 0; i < packBtns.length; i++) {
      var b = packBtns[i];
      b.classList.toggle('sel', b.getAttribute('data-pack') === activePack);
    }

    var volLbl = document.getElementById('vnVolLabel');
    var volRange = document.getElementById('vnVolRange');
    var volText = document.getElementById('vnVolText');
    var setTestBtn = document.getElementById('vnSetTestBtn');
    if (volLbl) volLbl.textContent = tr('voice.volume', '音量', 'Volume');
    if (volRange) volRange.value = String(cfg.volume || 85);
    if (volText) volText.textContent = (cfg.volume || 85) + '%';
    if (setTestBtn) setTestBtn.textContent = tr('voice.testBtn', '▶ 试听', '▶ Test');

    var altLbl = document.getElementById('vnAltLbl');
    var altOn = document.getElementById('vnAltOn');
    var altOff = document.getElementById('vnAltOff');
    if (altLbl) altLbl.textContent = tr('voice.altLbl', '高低差', 'Altitude');
    if (altOn) {
      altOn.textContent = tr('voice.on', '开启', 'On');
      altOn.classList.toggle('sel', !!cfg.altAlert);
    }
    if (altOff) {
      altOff.textContent = tr('voice.off', '关闭', 'Off');
      altOff.classList.toggle('sel', !cfg.altAlert);
    }

    var freqLbl = document.getElementById('vnFreqLbl');
    var selFreq = document.getElementById('vnFreqMode');
    if (freqLbl) freqLbl.textContent = tr('voice.adv.freq', '播报频次', 'Frequency');
    if (selFreq) {
      selFreq.value = cfg.freqMode || 'standard';
      selFreq.title = en
        ? 'Compact: 100m alert, 25m arrive, ±30m alt\nStandard: 300/100m alert, 35m arrive, ±25m alt\nDetailed: 500/300/100m alert, 50m arrive, ±20m alt'
        : '精简：100m 提醒，25m 到达，±30m 高低差\n标准：300 / 100m 提醒，35m 到达，±25m 高低差\n详细：500 / 300 / 100m 提醒，50m 到达，±20m 高低差';
      var fOpts = selFreq.options;
      if (fOpts && fOpts.length >= 3) {
        fOpts[0].text = tr('voice.freq.compact', '精简（100 / 25m）', 'Compact (100 / 25m)');
        fOpts[1].text = tr('voice.freq.standard', '标准（300 / 100 / 35m）', 'Standard (300 / 100 / 35m)');
        fOpts[2].text = tr('voice.freq.dense', '详细（500 / 300 / 100 / 50m）', 'Detailed (500 / 300 / 100 / 50m)');
      }
    }

    var resetBtn = document.getElementById('vnResetBtn');
    if (resetBtn) resetBtn.textContent = tr('voice.resetBtn', '恢复默认', 'Reset');
  }

  function packDisplayName(pack) {
    if (pack === 'yujie') return tr('voice.pack.yujie', '御姐领航员', 'Navigator (Mature)');
    return tr('voice.pack.zelda', '塞尔达风', 'Princess (Zelda)');
  }

  function setEnabled(flag, showToast) {
    cfg.enabled = !!flag;
    if (!cfg.enabled) stopPlayback();
    saveCfg();
    if (showToast) {
      showHostToast(cfg.enabled
        ? (isEn() ? ('Voice navigation ON (' + packDisplayName(cfg.pack) + ')') : ('已开启语音导航（' + packDisplayName(cfg.pack) + '）'))
        : tr('voice.toastOff', '已关闭语音导航', 'Voice navigation OFF'));
    }
  }

  function setPaused(flag) {
    externalPaused = !!flag;
    if (externalPaused) stopPlayback();
  }

  function isPaused() { return externalPaused; }

  function isSpeaking() {
    return !!(audioEl && !audioEl.paused && !audioEl.ended);
  }

  function setConfig(patch) {
    if (!patch || typeof patch !== 'object') return cfg;
    for (var k in DEFAULT_CFG) {
      if (patch[k] !== undefined) cfg[k] = patch[k];
    }
    if (cfg.pack !== 'zelda' && cfg.pack !== 'yujie') cfg.pack = 'zelda';
    if (!cfg.enabled) stopPlayback();
    saveCfg();
    return getConfig();
  }

  function getConfig() {
    var copy = {};
    for (var k in cfg) copy[k] = cfg[k];
    return copy;
  }

  function resetConfig() {
    for (var k in DEFAULT_CFG) cfg[k] = DEFAULT_CFG[k];
    saveCfg();
    return getConfig();
  }

  function init(opts) {
    opts = opts || {};
    if (opts.basePath) BASE_PATH = opts.basePath;
    if (opts.storageKey) {
      STORAGE_KEY = opts.storageKey;
      cfg = loadCfg();
    }
    if (typeof document !== 'undefined') {
      injectStyles();
      mountVoiceBoxUI();
      syncUI();

      if (typeof window !== 'undefined' && window.I18N && typeof window.I18N.onChange === 'function') {
        window.I18N.onChange(function () { syncUI(); });
      }
      var langBtns = document.getElementById('stLang');
      if (langBtns) langBtns.addEventListener('click', function () { setTimeout(syncUI, 50); });
      var stReset = document.getElementById('stReset');
      if (stReset) stReset.addEventListener('click', function () { resetConfig(); });
    }
  }

  if (typeof document !== 'undefined') {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', function () { init(); });
    } else {
      setTimeout(function () { init(); }, 0);
    }
  }

  return {
    init: init,
    feed: feed,
    openBox: openVoiceBox,
    closeBox: closeVoiceBox,
    toggleBox: toggleVoiceBox,
    setEnabled: setEnabled,
    setPaused: setPaused,
    isPaused: isPaused,
    isSpeaking: isSpeaking,
    setConfig: setConfig,
    getConfig: getConfig,
    resetConfig: resetConfig,
    testPlay: testPlay,
    stop: stopPlayback,
    calcCompass8: calcCompass8,
    calcBearingDeg: calcBearingDeg,
    calcDistM: calcDistM,
    pickDistClip: pickDistClip,
    refreshUI: syncUI,
    _state: function () { return state; }
  };
});
