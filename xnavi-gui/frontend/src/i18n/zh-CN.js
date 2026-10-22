export default {
  title: {
    main: 'Xnavi 游戏定位导航',
    nav: 'Xnavi'
  },
  header: {
    settings: '设置',
    about: '关于',
    minimize: '最小化',
    close: '关闭'
  },
  sync: {
    waiting: '等待坐标流',
    syncing: '坐标流同步中',
    attached: '已附加 pid={pid}，定位中...',
    waitingProcess: '等待模拟器进程...',
    ready: '就绪'
  },
  steps: {
    ready: '就绪',
    scanning: '扫描中',
    verifying: '验证移动',
    locked: '已锁定'
  },
  state: {
    ready: '就绪',
    waitingProcess: '等待模拟器进程...',
    scanning: '扫描中...',
    lockedUnconfirmed: '已初锁 · 待移动确认',
    verified: '已验证 · 跟随中'
  },
  emu: {
    auto: 'Auto (自动探测)',
    cemu: 'Cemu (Wii U)',
    ryujinx: 'Ryujinx (Switch)',
    eden: 'Eden (Switch)',
    pickTitle: '选择模拟器（改后需重启定位）'
  },
  buttons: {
    openMap: '打开网页地图',
    start: '开始定位',
    locating: '正在定位中',
    browse: '浏览',
    openDir: '打开目录',
    save: '保存',
    cancel: '取消',
    confirm: '确认继续',
    later: '稍后',
    download: '下载',
    clear: '清屏',
    checkUpdate: '检查更新',
    exportDiag: '导出诊断包',
    chooseFile: '选择存档',
    chooseDir: '存档目录',
    locateFile: '定位存档',
    detectSave: '检测存档',
    resetAuto: '恢复自动探测',
    submitCalib: '定向找人'
  },
  locate: {
    liveCoords: '实时同步坐标',
    offset: '动力学根地址',
    waitingAddr: '等待锁定',
    axisZ: 'Z 高度',
    currentLayer: '所在区域',
    calibration: '坐标校准',
    calWhenFail: '快速找人',
    submit: '定向找人',
    calHint: '若未跟随，可输入神庙/高塔坐标定向秒搜：',
    localProgress: '本地解析进度',
    waitingProgress: '等待读取存档...（需先开始定位）',
    saveInfo: '当前生效存档',
    autoDetectSave: '自动检测存档',
    latestSaveTime: '最新存档',
    justNow: '刚刚 (0分钟前)',
    minsAgo: '{m}分钟前',
    hoursAgo: '{h}小时前',
    playtime: '时长',
    shrine: '神庙',
    tower: '高塔',
    korok: '呀哈哈',
    memory: '回忆',
    beast: '神兽',
    saveSync: '探索度直通',
    saveSyncOk: '实时全量同步'
  },
  settings: {
    title: '设置与存档管理',
    autoOpenMap: '锁定后自动打开网页地图',
    customMapUrl: '自定义地图网页地址',
    customMapUrlPlaceholder: '默认官方地图 (支持 file:/// 或 本地网页)',
    cemuPath: 'Cemu 的 BOTW 存档路径（留空自动探测）：',
    ryuPath: 'Ryujinx 的 BOTW 存档路径（留空自动探测）：',
    edenPath: 'Eden 的 BOTW 存档路径（留空自动探测）：',
    pathAuto: '自动探测',
    pathOk: '✓ 路径有效',
    pathBad: '✕ 路径未找到',
    tls: '手机防息屏（HTTPS）',
    tlsWarn: '（首次需装证书）',
    saved: '已保存配置'
  },
  about: {
    title: '关于 Xnavi',
    desc: '三合一定位导航（BOTW × Cemu + Ryujinx + Eden）',
    version: '版本',
    map: '地图',
    github: 'GitHub',
    note: '只读内存，不注入，不修改游戏。'
  },
  update: {
    available: '有新版本可用',
    current: '当前',
    latest: '最新'
  },
  log: {
    title: '实时日志',
    none: '暂无日志',
    filter: '过滤日志...'
  }
}

