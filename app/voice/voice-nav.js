/* ============================================================================
 * BOTWmap · 独立网页端实时语音导航模块 (app/voice/voice-nav.js)
 * ----------------------------------------------------------------------------
 * 设计目标：
 *   1. 100% 独立可插拔模块：自带状态机、音频池、Web Audio 希卡感应音合成、独立配置与 UI 挂载；
 *      宿主（网页地图 BOTWmap）即使不加载或彻底关闭本模块也 0 报错、0 开销。
 *   2. 罗盘 8 方位绝对方向：严格对齐 BOTWmap 坐标系（mx 东正西负、my 北负南正，1px = 0.5m）。
 *   3. 2D 平面距离阶梯：支持 500m / 300m / 100m / 自定义到达距离、传送远离 +80m 重置、偏航提醒。
 *   4. 中英文离线角色语音包 + 三级兜底：
 *      - zelda : 塞尔达公主风（温柔空灵少女音，离线 Opus .webm；英文界面自动切 en/ 英式女声）
 *      - yujie : 御姐领航员（成熟干练御姐音，离线 Opus .webm；英文界面自动切 en/ 英式女声）
 *      - chime : 仅希卡之石雷达感应音（纯 Web Audio 合成，0 音频文件请求）
 *      - tts   : 浏览器原生语音合成（支持直接播报具体神庙/地标名称，自适应中英文）
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
  var ONLINE_BASE = 'https://botw.yalin.site/voice/';

  /* 默认独立配置 */
  var DEFAULT_CFG = {
    enabled: true,         // 语音导航总开关
    pack: 'zelda',         // 'zelda' | 'yujie' | 'chime' | 'tts'
    volume: 85,            // 音量 0 ~ 100
    dirDist: true,         // 播报罗盘 8 方位与距离
    offCourse: true,       // 偏航提醒（背离目标持续跑动）
    chime: true,           // 希卡之石雷达感应音
    arriveM: 35,           // 到达播报距离阈值（15 ~ 80m，默认 35m）
    freqMode: 'standard'   // 中途播报频次：'compact'(仅100m) | 'standard'(300m+100m) | 'dense'(500m+300m+100m)
  };

  var FREQ_PRESETS = {
    compact:  { arriveM: 25 },
    standard: { arriveM: 35 },
    dense:    { arriveM: 50 }
  };

  var cfg = loadCfg();

  /* 标准词条文本（供系统 TTS 模式、中英双语回退及调试日志使用） */
  var CLIP_TEXT = {
    welcome:     { zh: '语音导航已就绪。', en: 'Voice navigation ready.' },
    start_nav:   { zh: '开始导航。', en: 'Starting navigation.' },
    start_auto:  { zh: '开启自动收集。', en: 'Auto collection started.' },
    next_target: { zh: '完成！下一个目标，', en: 'Completed! Next target,' },
    switch_near: { zh: '发现更近目标，', en: 'Closer target found,' },
    dir_n:       { zh: '正北方向，', en: 'North,' },
    dir_ne:      { zh: '东北方向，', en: 'Northeast,' },
    dir_e:       { zh: '正东方向，', en: 'East,' },
    dir_se:      { zh: '东南方向，', en: 'Southeast,' },
    dir_s:       { zh: '正南方向，', en: 'South,' },
    dir_sw:      { zh: '西南方向，', en: 'Southwest,' },
    dir_w:       { zh: '正西方向，', en: 'West,' },
    dir_nw:      { zh: '西北方向，', en: 'Northwest,' },
    dist_far:    { zh: '距离较远，建议先传送。', en: 'Far away, consider fast travel.' },
    dist_800:    { zh: '约八百米。', en: 'about 800 meters.' },
    dist_500:    { zh: '约五百米。', en: 'about 500 meters.' },
    dist_400:    { zh: '约四百米。', en: 'about 400 meters.' },
    dist_300:    { zh: '三百米。', en: '300 meters.' },
    dist_200:    { zh: '两百米。', en: '200 meters.' },
    dist_150:    { zh: '一百五十米。', en: '150 meters.' },
    dist_100:    { zh: '一百米。', en: '100 meters.' },
    dist_50:     { zh: '五十米。', en: '50 meters.' },
    arrived:     { zh: '已到达目标附近。', en: 'Arrived near target.' },
    all_done:    { zh: '太棒了，目标已全部完成！', en: 'Awesome! All targets are completed!' },
    off_course:  { zh: '方向偏离，目标在', en: 'Off course, target is' },
    korok_near:  { zh: '附近有克洛格的气息哦。', en: 'A Korok is hiding nearby.' },
    shrine_near: { zh: '感应到神庙反应。', en: 'Shrine sensor triggered nearby.' }
  };

  /* 内部状态机（针对当前导航目标） */
  var state = {
    targetKey: '',          // 当前目标唯一标识 'x,y,name'
    announcedStart: false,  // 是否已播报起步
    passed500: false,       // 是否已触发 500m 节点（详细模式）
    passed300: false,       // 是否已触发 300m 节点
    passed100: false,       // 是否已触发 100m 节点
    passedArrive: false,    // 是否已触发到达节点
    lastDist: null,         // 上一拍平面距离（米）
    lastPx: null,           // 上一拍玩家 mx（像素）
    lastPy: null,           // 上一拍玩家 my（像素）
    offCourseSince: 0,      // 连续偏航起始时间戳
    lastOffCourseSpeak: 0,  // 上次播报偏航时间戳
    lastSpokenClips: [],    // 最近一次播报的词条列表（便于测试与状态检查）
    lastPlaybackMode: '',   // 最近一次播报模式: 'webm' | 'tts' | 'chime'
    lastPlaybackPack: ''    // 最近一次实际使用的语音包目录: 'zelda' | 'yujie' | 'en' | ''
  };

  var OFF_COURSE_WAIT = 6000;      // 持续反向移动 6 秒触发偏航
  var OFF_COURSE_COOLDOWN = 22000; // 偏航提醒冷却 22 秒
  var MILESTONE_RESET_HYST = 80;   // 传送/远离超过里程碑 +80m 时重置对应里程碑
  var externalPaused = false;      // 外部模块临时静音避让标志（不改动用户 cfg.enabled）
  var audioUnlocked = false;       // 浏览器首次手势解锁标志
  var autoplayWarned = false;      // 自动播放被拦截提示防重复标志

  function getArriveM() {
    var v = parseInt(cfg.arriveM, 10);
    return (isNaN(v) || v < 10) ? 35 : Math.min(80, Math.max(15, v));
  }

  /* 音频播放队列与缓存池 */
  var audioCache = {};
  var playQueue = [];
  var currentAudio = null;
  var queueTimer = null;
  var audioCtx = null;

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
      }
    } catch (e) {}
    return res;
  }

  function saveCfg() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg));
    } catch (e) {}
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

  /* ---------------- BOTWmap 罗盘 8 方位与平面距离计算 ----------------
   * 坐标口径（严格对齐 BOTWmap app/js/live.js）：
   *   player.x (mx) = 像素 X（东正西负），gameX = (mx - 12000) * 0.5
   *   player.y (my) = 像素 Y（南正北负），gameZ = (my - 10000) * 0.5
   *   比例尺：1 像素 = 0.5 游戏米
   *   方位角：0° = 正北，90° = 正东，180° = 正南，270° = 正西
   */
  function calcBearingDeg(px, py, tx, ty) {
    var dE = tx - px; // 东向增量
    var dN = py - ty; // 北向增量（my 越小越偏北）
    var rad = Math.atan2(dE, dN);
    return (rad * 180 / Math.PI + 360) % 360;
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
    var dx = target.x - player.x;
    var dy = target.y - player.y;
    return Math.sqrt(dx * dx + dy * dy) * 0.5;
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

  /* ---------------- Web Audio 希卡之石雷达感应音（0 字节纯合成） ---------------- */
  function ensureAudioCtx() {
    if (!audioCtx && typeof window !== 'undefined') {
      var AC = window.AudioContext || window.webkitAudioContext;
      if (AC) {
        try { audioCtx = new AC(); } catch (e) {}
      }
    }
    if (audioCtx && audioCtx.state === 'suspended') {
      try { audioCtx.resume(); } catch (e) {}
    }
    return audioCtx;
  }

  function playChime(kind) {
    if (!cfg.enabled || externalPaused || !cfg.chime) return;
    var ctx = ensureAudioCtx();
    if (!ctx) return;
    var now = ctx.currentTime;
    var vol = Math.max(0.05, Math.min(1, (cfg.volume || 80) / 100)) * 0.28;

    function tone(freq, startOffset, dur, gainMul) {
      try {
        var osc = ctx.createOscillator();
        var gain = ctx.createGain();
        osc.type = 'sine';
        osc.frequency.setValueAtTime(freq, now + startOffset);
        var g = vol * (gainMul || 1);
        gain.gain.setValueAtTime(0.0001, now + startOffset);
        gain.gain.linearRampToValueAtTime(g, now + startOffset + 0.015);
        gain.gain.exponentialRampToValueAtTime(0.0001, now + startOffset + dur);
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.start(now + startOffset);
        osc.stop(now + startOffset + dur + 0.02);
      } catch (e) {}
    }

    if (kind === 'ping') {
      // 希卡感应器双连音 (E6 -> A6)
      tone(1318.5, 0.00, 0.14, 0.7);
      tone(1760.0, 0.11, 0.24, 1.0);
    } else if (kind === 'arrive') {
      // 到达目标三连清脆泛音 (E6 -> G#6 -> B6)
      tone(1318.5, 0.00, 0.14, 0.8);
      tone(1661.2, 0.11, 0.16, 0.9);
      tone(1975.5, 0.22, 0.35, 1.1);
    } else if (kind === 'done') {
      // 完成目标上扬双音 (A5 -> E6)
      tone(880.0,  0.00, 0.12, 0.8);
      tone(1318.5, 0.10, 0.28, 1.0);
    }
  }

  /* ---------------- 音频切片队列播放器 ---------------- */
  function stopPlayback() {
    playQueue = [];
    if (queueTimer) {
      clearTimeout(queueTimer);
      queueTimer = null;
    }
    if (currentAudio) {
      try {
        currentAudio.pause();
        currentAudio.currentTime = 0;
      } catch (e) {}
      currentAudio = null;
    }
    if (typeof window !== 'undefined' && window.speechSynthesis) {
      try { window.speechSynthesis.cancel(); } catch (e) {}
    }
  }

  function getAudioEl(pack, clipId, useOnline) {
    var key = (useOnline ? 'online:' : 'local:') + pack + '/' + clipId;
    if (!audioCache[key]) {
      var base = useOnline
        ? ((typeof window !== 'undefined' && window.BOTW_VOICE_ONLINE_BASE) || ONLINE_BASE)
        : BASE_PATH;
      var a = new Audio(base + pack + '/' + clipId + '.webm');
      a.preload = 'auto';
      audioCache[key] = a;
    }
    return audioCache[key];
  }

  function speakViaSystemTTS(clips, customTargetName) {
    if (typeof window === 'undefined' || !window.speechSynthesis) return;
    var lang = isEn() ? 'en' : 'zh';
    var parts = [];
    for (var i = 0; i < clips.length; i++) {
      var c = clips[i];
      if (CLIP_TEXT[c]) {
        parts.push(CLIP_TEXT[c][lang] || CLIP_TEXT[c].zh);
      }
    }
    if (customTargetName && (clips[0] === 'start_nav' || clips[0] === 'start_auto' || clips[0] === 'next_target' || clips[0] === 'switch_near')) {
      parts.splice(1, 0, customTargetName + (lang === 'en' ? ', ' : '，'));
    }
    var text = parts.join('');
    if (!text) return;
    try {
      window.speechSynthesis.cancel();
      var u = new SpeechSynthesisUtterance(text);
      u.lang = isEn() ? 'en-US' : 'zh-CN';
      u.volume = Math.max(0.1, Math.min(1, (cfg.volume || 80) / 100));
      u.rate = 1.05;
      window.speechSynthesis.speak(u);
    } catch (e) {}
  }

  function speakClips(clips, opts) {
    opts = opts || {};
    if (!cfg.enabled || externalPaused) return;
    if (!clips || !clips.length) return;

    state.lastSpokenClips = clips.slice();

    // 仅感应音模式：只响雷达音，不播人声
    if (cfg.pack === 'chime') {
      state.lastPlaybackMode = 'chime';
      state.lastPlaybackPack = '';
      if (opts.chime) playChime(opts.chime);
      return;
    }

    if (opts.interrupt !== false) {
      stopPlayback();
    }

    if (opts.chime) {
      playChime(opts.chime);
    }

    // 系统原生 TTS 模式
    if (cfg.pack === 'tts') {
      state.lastPlaybackMode = 'tts';
      state.lastPlaybackPack = '';
      speakViaSystemTTS(clips, opts.targetName);
      return;
    }

    // 离线 Opus .webm 模式：英文界面自动切内置 en/ 英式离线女声包，中文界面按 zelda / yujie 播放
    state.lastPlaybackMode = 'webm';
    var pack = isEn() ? 'en' : ((cfg.pack === 'yujie') ? 'yujie' : 'zelda');
    state.lastPlaybackPack = pack;
    for (var i = 0; i < clips.length; i++) {
      playQueue.push({ pack: pack, clipId: clips[i] });
    }
    for (var j = 0; j < playQueue.length; j++) {
      getAudioEl(playQueue[j].pack, playQueue[j].clipId, false);
    }
    pumpQueue();
  }

  function pumpQueue() {
    if (!cfg.enabled || externalPaused || currentAudio || !playQueue.length) return;
    var item = playQueue.shift();

    function playOne(useOnline) {
      var audio = getAudioEl(item.pack, item.clipId, useOnline);
      currentAudio = audio;
      audio.volume = Math.max(0.05, Math.min(1, (cfg.volume || 80) / 100));

      var advanced = false;
      function next() {
        if (advanced) return;
        advanced = true;
        audio.removeEventListener('ended', next);
        audio.removeEventListener('error', onErr);
        if (currentAudio === audio) currentAudio = null;
        queueTimer = setTimeout(pumpQueue, 25);
      }
      function onErr() {
        if (advanced) return;
        advanced = true;
        audio.removeEventListener('ended', next);
        audio.removeEventListener('error', onErr);
        if (currentAudio === audio) currentAudio = null;
        // 三级回退：本地 .webm -> 线上 .webm -> 系统 TTS
        if (!useOnline) {
          playOne(true);
          return;
        }
        speakViaSystemTTS([item.clipId]);
        queueTimer = setTimeout(pumpQueue, 100);
      }

      audio.addEventListener('ended', next);
      audio.addEventListener('error', onErr);
      try {
        audio.currentTime = 0;
        var pr = audio.play();
        if (pr && typeof pr.catch === 'function') {
          pr.catch(function (err) {
            if (err && err.name === 'NotAllowedError') {
              advanced = true;
              audio.removeEventListener('ended', next);
              audio.removeEventListener('error', onErr);
              if (currentAudio === audio) currentAudio = null;
              playQueue = [];
              if (!autoplayWarned) {
                autoplayWarned = true;
                showHostToast(tr('voice.autoplayHint', '浏览器限制自动语音，请点击地图任意处解锁', 'Browser blocked auto audio — click anywhere on the map to unlock'));
              }
              return;
            }
            onErr();
          });
        }
      } catch (e) {
        onErr();
      }
    }
    playOne(false);
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
    return clips;
  }

  function resetTargetState(tKey, d) {
    var arrM = getArriveM();
    state.targetKey = tKey || '';
    state.announcedStart = true;
    state.passed500 = (d != null && d <= 500);
    state.passed300 = (d != null && d <= 300);
    state.passed100 = (d != null && d <= 100);
    state.passedArrive = (d != null && d <= arrM);
    state.lastDist = d;
    state.offCourseSince = 0;
  }

  /* 宿主每拍或事件触发时调用：
   * VoiceNav.feed({
   *   player: { x, y, located },
   *   target: { id, name, x, y, type },
   *   event: 'start_nav' | 'start_auto' | 'next_target' | 'switch_near' | 'all_done' | 'clear' | null
   * })
   */
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
      if (cfg.enabled) {
        speakClips(['all_done'], { chime: 'done', interrupt: true });
      }
      return;
    }

    if (!target || target.x == null || target.y == null) {
      state.targetKey = '';
      return;
    }

    var tKey = Math.round(target.x) + ',' + Math.round(target.y) + ',' + (target.id || target.name || '');
    var hasPlayer = !!(player && player.located !== false && player.x != null && player.y != null);
    var d = hasPlayer ? calcDistM(player, target) : null;

    // 1. 显式导航事件
    if (ev === 'start_nav' || ev === 'start_auto' || ev === 'next_target' || ev === 'switch_near') {
      resetTargetState(tKey, d);
      if (hasPlayer) {
        var clips = buildTargetAnnounceClips(ev, player, target, d);
        var chimeType = (ev === 'next_target') ? 'done' : 'ping';
        speakClips(clips, { chime: chimeType, interrupt: true, targetName: target.name });
        state.lastPx = player.x;
        state.lastPy = player.y;
      } else {
        speakClips([ev], { chime: 'ping', interrupt: true, targetName: target.name });
      }
      return;
    }

    if (!hasPlayer) return;

    // 2. 目标发生隐式切换（例如服务端 /pos 同步过来的新目标）
    if (tKey !== state.targetKey) {
      resetTargetState(tKey, d);
      var autoClips = buildTargetAnnounceClips('start_nav', player, target, d);
      speakClips(autoClips, { chime: 'ping', interrupt: true, targetName: target.name });
      state.lastPx = player.x;
      state.lastPy = player.y;
      return;
    }

    if (d == null) return;
    var arrM = getArriveM();

    // 2.5 传送或大幅远离超过里程碑 +80m 迟滞时，自动重置对应里程碑
    if (state.passedArrive && d > arrM + MILESTONE_RESET_HYST) state.passedArrive = false;
    if (state.passed100 && d > 100 + MILESTONE_RESET_HYST) state.passed100 = false;
    if (state.passed300 && d > 300 + MILESTONE_RESET_HYST) state.passed300 = false;
    if (state.passed500 && d > 500 + MILESTONE_RESET_HYST) state.passed500 = false;

    // 3. 距离阶梯里程碑检测
    if (d <= arrM && !state.passedArrive) {
      state.passedArrive = true;
      state.passed100 = true;
      state.passed300 = true;
      state.passed500 = true;
      var arrClips = ['arrived'];
      var tType = String(target.type || '').toLowerCase();
      var tName = String(target.name || '');
      if (tType === 'seed' || tType.indexOf('korok') >= 0 || tName.indexOf('克洛格') >= 0 || tName.toLowerCase().indexOf('korok') >= 0) {
        arrClips.push('korok_near');
      } else if (tType === 'shrine' || tType.indexOf('shrine') >= 0 || tName.indexOf('神庙') >= 0 || tName.toLowerCase().indexOf('shrine') >= 0) {
        arrClips.push('shrine_near');
      }
      speakClips(arrClips, { chime: 'arrive', interrupt: true });
    } else if (d <= 100 && !state.passed100) {
      state.passed100 = true;
      state.passed300 = true;
      state.passed500 = true;
      if (cfg.dirDist) {
        speakClips([calcCompass8(player.x, player.y, target.x, target.y), 'dist_100'], { chime: 'ping', interrupt: false });
      } else {
        playChime('ping');
      }
    } else if (d <= 300 && !state.passed300 && cfg.freqMode !== 'compact') {
      state.passed300 = true;
      state.passed500 = true;
      if (cfg.dirDist) {
        speakClips([calcCompass8(player.x, player.y, target.x, target.y), 'dist_300'], { chime: null, interrupt: false });
      }
    } else if (d <= 500 && !state.passed500 && cfg.freqMode === 'dense') {
      state.passed500 = true;
      if (cfg.dirDist) {
        speakClips([calcCompass8(player.x, player.y, target.x, target.y), 'dist_500'], { chime: null, interrupt: false });
      }
    }

    // 4. 偏航检测（距离 > 60m，且连续 6 秒朝背离目标 > 120° 方向移动）
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
            speakClips(['off_course', bearingToDirClip(targetBearing)], { chime: 'ping', interrupt: false });
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

  /* 试听功能 */
  function testPlay() {
    ensureAudioCtx();
    if (!cfg.enabled) {
      cfg.enabled = true;
      saveCfg();
    }
    var demoClips = ['welcome', 'dir_ne', 'dist_100'];
    speakClips(demoClips, { chime: 'ping', interrupt: true });
  }

  /* ---------------- 独立 UI 挂载与双向同步（融入 BOTWmap 原生样式） ---------------- */
  function injectStyles() {
    if (typeof document === 'undefined' || document.getElementById('botw-voice-nav-css')) return;
    var st = document.createElement('style');
    st.id = 'botw-voice-nav-css';
    st.textContent =
      '#vnSettingsGroup .vn-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:6px;}' +
      '#vnSettingsGroup .vn-head .st-label{margin-bottom:0;}' +
      '#vnSettingsGroup .vn-desc{font-size:11.5px;color:var(--text-dim,#8fa390);margin-bottom:8px;line-height:1.4;}' +
      '#vnSettingsGroup .vn-detail{display:flex;flex-direction:column;gap:8px;transition:opacity .15s;}' +
      '#vnSettingsGroup .vn-detail.off{opacity:.42;pointer-events:none;}' +
      '#vnSettingsGroup .vn-sub-row{display:flex;align-items:center;gap:8px;flex-wrap:wrap;}' +
      '#vnSettingsGroup .vn-slabel{font-size:12px;color:var(--text-dim,#8fa390);white-space:nowrap;min-width:48px;}' +
      '#vnSettingsGroup .vn-vol-slider{flex:1;min-width:90px;accent-color:var(--gold,#f2c94c);cursor:pointer;height:4px;}' +
      '#vnSettingsGroup .vn-vol-val{width:34px;text-align:right;font-variant-numeric:tabular-nums;color:var(--gold-lite,#ffd978);font-size:12px;}' +
      '#vnSettingsGroup .vn-test-btn{background:rgba(242,201,76,.14);border:1px solid var(--gold-dim,#9a7b4f);color:var(--gold-lite,#ffd978);border-radius:6px;padding:3px 9px;font-size:12px;cursor:pointer;white-space:nowrap;transition:.15s;}' +
      '#vnSettingsGroup .vn-test-btn:hover{background:rgba(242,201,76,.28);color:#fff;}' +
      '#vnSettingsGroup .vn-freq-sel{flex:1;min-width:120px;max-width:190px;background:#132016;border:1px solid var(--border,#2d4731);border-radius:6px;color:var(--text,#e8eee8);font-size:12px;padding:4px 8px;outline:none;cursor:pointer;}' +
      '#vnSettingsGroup .vn-freq-sel option{background:#132016;color:#e8eee8;}' +
      '#vnSettingsGroup .vn-packs .st-opt{padding:4px 9px;font-size:12px;}';
    document.head.appendChild(st);
  }

  function bindTopbarBtn() {
    if (typeof document === 'undefined') return;
    var btn = document.getElementById('btnVoice');
    if (!btn || btn.dataset.vnBound) return;
    btn.dataset.vnBound = '1';
    btn.addEventListener('click', function () {
      ensureAudioCtx();
      setEnabled(!cfg.enabled, true);
    });
  }

  function mountSettingsBoxUI() {
    if (typeof document === 'undefined') return;
    var box = document.getElementById('settingsBox');
    if (!box || document.getElementById('vnSettingsGroup')) return;

    var liveTrailEl = document.getElementById('stLiveTrail');
    var anchorGroup = liveTrailEl ? liveTrailEl.parentNode : null;

    var grp = document.createElement('div');
    grp.id = 'vnSettingsGroup';
    grp.className = 'st-group';
    grp.innerHTML =
      '<div class="vn-head">' +
        '<div class="st-label" id="vnSetTitle">语音导航</div>' +
        '<div class="st-options" id="vnSetToggleOpts">' +
          '<button type="button" class="st-opt" data-v="on" id="vnToggleOn">开启</button>' +
          '<button type="button" class="st-opt" data-v="off" id="vnToggleOff">关闭</button>' +
        '</div>' +
      '</div>' +
      '<div class="vn-desc" id="vnSetHint">罗盘 8 方位实时语音指路（塞尔达 / 御姐离线语音）</div>' +
      '<div class="vn-detail" id="vnSetDetail">' +
        '<div class="st-options vn-packs" id="vnSetPackBtns">' +
          '<button type="button" class="st-opt" data-pack="zelda" id="vnBtnZelda">塞尔达</button>' +
          '<button type="button" class="st-opt" data-pack="yujie" id="vnBtnYujie">御姐音</button>' +
          '<button type="button" class="st-opt" data-pack="chime" id="vnBtnChime">感应音</button>' +
          '<button type="button" class="st-opt" data-pack="tts" id="vnBtnTts">系统语音</button>' +
        '</div>' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnVolLabel">音量</span>' +
          '<input type="range" id="vnVolRange" class="vn-vol-slider" min="10" max="100" step="5" value="85">' +
          '<span id="vnVolText" class="vn-vol-val">85%</span>' +
          '<button type="button" id="vnSetTestBtn" class="vn-test-btn">▶ 试听</button>' +
        '</div>' +
        '<div class="vn-sub-row">' +
          '<span class="vn-slabel" id="vnFreqLbl">播报频次</span>' +
          '<select id="vnFreqMode" class="vn-freq-sel">' +
            '<option value="compact">精简（100m）</option>' +
            '<option value="standard">标准（300/100m）</option>' +
            '<option value="dense">详细（500/300/100m）</option>' +
          '</select>' +
        '</div>' +
      '</div>';

    if (anchorGroup && anchorGroup.parentNode === box) {
      box.insertBefore(grp, anchorGroup.nextSibling);
    } else {
      var foot = box.querySelector('.st-foot');
      if (foot) box.insertBefore(grp, foot);
      else box.appendChild(grp);
    }

    var onBtn = document.getElementById('vnToggleOn');
    var offBtn = document.getElementById('vnToggleOff');
    if (onBtn) onBtn.addEventListener('click', function () { ensureAudioCtx(); setEnabled(true, true); });
    if (offBtn) offBtn.addEventListener('click', function () { setEnabled(false, true); });

    var packBtns = grp.querySelectorAll('#vnSetPackBtns .st-opt');
    for (var i = 0; i < packBtns.length; i++) {
      packBtns[i].addEventListener('click', function () {
        ensureAudioCtx();
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
        if (currentAudio) currentAudio.volume = Math.max(0.05, Math.min(1, cfg.volume / 100));
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
        saveCfg();
      });
    }
  }

  function syncUI() {
    if (typeof document === 'undefined') return;
    var en = isEn();

    // 1. 同步顶栏快捷按钮 #btnVoice
    var topBtn = document.getElementById('btnVoice');
    if (topBtn) {
      topBtn.classList.toggle('active', !!cfg.enabled);
      var ico = topBtn.querySelector('.ico');
      var lbl = topBtn.querySelector('.label');
      if (ico) ico.textContent = cfg.enabled ? '🔊' : '🔇';
      if (lbl) lbl.textContent = tr('tool.voice', '语音', 'Voice');
      topBtn.setAttribute('title', cfg.enabled
        ? tr('voice.topTitleOn', '语音导航已开启（点击静音，详细音色请在设置中调整）', 'Voice navigation ON (click to mute; configure in Settings)')
        : tr('voice.topTitleOff', '语音导航已关闭（点击开启）', 'Voice navigation OFF (click to enable)'));
    }

    // 2. 同步设置弹层 #settingsBox 内控件
    var setTitle = document.getElementById('vnSetTitle');
    var setHint = document.getElementById('vnSetHint');
    var onBtn = document.getElementById('vnToggleOn');
    var offBtn = document.getElementById('vnToggleOff');
    if (setTitle) setTitle.textContent = tr('voice.setTitle', '语音导航', 'Voice Navigation');
    if (setHint) setHint.textContent = tr('voice.setHint', '罗盘 8 方位实时语音指路（塞尔达 / 御姐离线语音）', '8-compass live voice guidance (Zelda / Mature offline packs)');
    if (onBtn) {
      onBtn.textContent = tr('voice.on', '开启', 'On');
      onBtn.classList.toggle('active', !!cfg.enabled);
    }
    if (offBtn) {
      offBtn.textContent = tr('voice.off', '关闭', 'Off');
      offBtn.classList.toggle('active', !cfg.enabled);
    }

    var detail = document.getElementById('vnSetDetail');
    if (detail) detail.classList.toggle('off', !cfg.enabled);

    var btnZelda = document.getElementById('vnBtnZelda');
    var btnYujie = document.getElementById('vnBtnYujie');
    var btnChime = document.getElementById('vnBtnChime');
    var btnTts = document.getElementById('vnBtnTts');
    if (btnZelda) btnZelda.textContent = tr('voice.pack.zeldaShort', '塞尔达', 'Zelda');
    if (btnYujie) btnYujie.textContent = tr('voice.pack.yujieShort', '御姐音', 'Mature');
    if (btnChime) btnChime.textContent = tr('voice.pack.chimeShort', '感应音', 'Chime');
    if (btnTts) btnTts.textContent = tr('voice.pack.ttsShort', '系统语音', 'TTS');

    var packBtns = document.querySelectorAll('#vnSetPackBtns .st-opt');
    var activePack = (cfg.pack === 'yujie' || cfg.pack === 'chime' || cfg.pack === 'tts') ? cfg.pack : 'zelda';
    for (var i = 0; i < packBtns.length; i++) {
      var b = packBtns[i];
      b.classList.toggle('active', b.getAttribute('data-pack') === activePack);
    }

    var volLbl = document.getElementById('vnVolLabel');
    var volRange = document.getElementById('vnVolRange');
    var volText = document.getElementById('vnVolText');
    var setTestBtn = document.getElementById('vnSetTestBtn');
    if (volLbl) volLbl.textContent = tr('voice.volume', '音量', 'Vol');
    if (volRange) volRange.value = String(cfg.volume || 85);
    if (volText) volText.textContent = (cfg.volume || 85) + '%';
    if (setTestBtn) setTestBtn.textContent = tr('voice.testBtn', '▶ 试听', '▶ Test');

    var freqLbl = document.getElementById('vnFreqLbl');
    var selFreq = document.getElementById('vnFreqMode');
    if (freqLbl) freqLbl.textContent = tr('voice.adv.freq', '播报频次', 'Frequency');
    if (selFreq) {
      selFreq.value = cfg.freqMode || 'standard';
      selFreq.title = en
        ? 'Compact: 100m alert, 25m arrive\nStandard: 300/100m alert, 35m arrive\nDetailed: 500/300/100m alert, 50m arrive'
        : '精简：100m 提醒，25m 到达\n标准：300m / 100m 提醒，35m 到达\n详细：500m / 300m / 100m 提醒，50m 到达';
      var fOpts = selFreq.options;
      if (fOpts && fOpts.length >= 3) {
        fOpts[0].text = tr('voice.freq.compact', '精简（100m）', 'Compact (100m)');
        fOpts[1].text = tr('voice.freq.standard', '标准（300/100m）', 'Standard (300/100m)');
        fOpts[2].text = tr('voice.freq.dense', '详细（500/300/100m）', 'Detailed (500/300/100m)');
      }
    }
  }

  function packDisplayName(pack) {
    if (pack === 'yujie') return tr('voice.pack.yujie', '御姐领航员', 'Navigator (Mature)');
    if (pack === 'chime') return tr('voice.pack.chime', '希卡感应音', 'Sheikah Chime');
    if (pack === 'tts') return tr('voice.pack.tts', '系统语音', 'System TTS');
    return tr('voice.pack.zelda', '塞尔达风', 'Princess (Zelda)');
  }

  function setEnabled(flag, showToast) {
    cfg.enabled = !!flag;
    if (!cfg.enabled) {
      stopPlayback();
    }
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

  function isPaused() {
    return externalPaused;
  }

  function isSpeaking() {
    if (currentAudio && !currentAudio.paused) return true;
    if (typeof window !== 'undefined' && window.speechSynthesis && window.speechSynthesis.speaking) return true;
    return false;
  }

  function bindAutoplayUnlock() {
    if (audioUnlocked || typeof document === 'undefined') return;
    function unlock() {
      audioUnlocked = true;
      autoplayWarned = false;
      ensureAudioCtx();
      document.removeEventListener('pointerdown', unlock, true);
      document.removeEventListener('keydown', unlock, true);
    }
    document.addEventListener('pointerdown', unlock, true);
    document.addEventListener('keydown', unlock, true);
  }

  function setConfig(patch) {
    if (!patch || typeof patch !== 'object') return cfg;
    for (var k in DEFAULT_CFG) {
      if (patch[k] !== undefined) cfg[k] = patch[k];
    }
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
      bindTopbarBtn();
      mountSettingsBoxUI();
      syncUI();
      bindAutoplayUnlock();

      if (typeof window !== 'undefined' && window.I18N && typeof window.I18N.onChange === 'function') {
        window.I18N.onChange(function () { syncUI(); });
      }
      var langBtns = document.getElementById('stLang');
      if (langBtns) {
        langBtns.addEventListener('click', function () { setTimeout(syncUI, 50); });
      }
      var stReset = document.getElementById('stReset');
      if (stReset) {
        stReset.addEventListener('click', function () { resetConfig(); });
      }
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
    setEnabled: setEnabled,
    setPaused: setPaused,
    isPaused: isPaused,
    isSpeaking: isSpeaking,
    setConfig: setConfig,
    getConfig: getConfig,
    resetConfig: resetConfig,
    testPlay: testPlay,
    stop: stopPlayback,
    playChime: playChime,
    calcCompass8: calcCompass8,
    calcBearingDeg: calcBearingDeg,
    calcDistM: calcDistM,
    pickDistClip: pickDistClip,
    refreshUI: syncUI,
    _state: function () { return state; }
  };
});
