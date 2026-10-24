export default {
  title: {
    main: 'Xnavi Game Position Tracker',
    nav: 'Xnavi'
  },
  header: {
    settings: 'Settings',
    about: 'About',
    minimize: 'Minimize',
    close: 'Close'
  },
  sync: {
    waiting: 'Waiting for stream',
    syncing: 'Stream connected',
    attached: 'Attached pid={pid}, locating...',
    waitingProcess: 'Waiting for emulator...',
    ready: 'Ready'
  },
  steps: {
    ready: 'Ready',
    scanning: 'Scanning',
    verifying: 'Verify Movement',
    locked: 'Locked'
  },
  state: {
    ready: 'Ready',
    waitingProcess: 'Waiting for emulator...',
    scanning: 'Scanning...',
    lockedUnconfirmed: 'Candidate locked · Move to verify',
    verified: 'Verified · Tracking'
  },
  emu: {
    auto: 'Auto (Detect)',
    cemu: 'Cemu (Wii U)',
    ryujinx: 'Ryujinx (Switch)',
    eden: 'Eden (Switch)',
    pickTitle: 'Select emulator (restart locating to apply)'
  },
  buttons: {
    openMap: 'Open Map',
    start: 'Start Locating',
    locating: 'Stop Locating',
    browse: 'Browse',
    openDir: 'Open Dir',
    save: 'Save',
    cancel: 'Cancel',
    confirm: 'Confirm',
    later: 'Later',
    download: 'Download',
    clear: 'Clear',
    checkUpdate: 'Check Update',
    exportDiag: 'Export Diag',
    chooseFile: 'Select Save',
    chooseDir: 'Save Dir',
    locateFile: 'Locate Save',
    detectSave: 'Detect Save',
    resetAuto: 'Reset Auto',
    submitCalib: 'Search Link'
  },
  locate: {
    liveCoords: 'Live Coordinates',
    offset: 'Dynamic Root Addr',
    waitingAddr: 'Waiting for lock',
    axisZ: 'Altitude (Z)',
    currentLayer: 'Region / Mode',
    calibration: 'Quick Calibration',
    calWhenFail: 'Search Target',
    submit: 'Search Target',
    calHint: 'Enter nearby landmark coordinates to fast locate:',
    localProgress: 'Save Progress',
    waitingProgress: 'Waiting for save... (Start locating first)',
    saveInfo: 'Active Save File',
    autoDetectSave: 'Auto detect save',
    latestSaveTime: 'Latest Save',
    justNow: 'Just now (0m ago)',
    minsAgo: '{m}m ago',
    hoursAgo: '{h}h ago',
    playtime: 'Playtime',
    shrine: 'Shrine',
    tower: 'Tower',
    korok: 'Korok',
    memory: 'Memory',
    beast: 'Beast',
    grpExplore: 'Exploration',
    grpQuest: 'Quests',
    grpCollect: 'Collection',
    grpSubjugate: 'Subjugation',
    memoryPhoto: 'Captured Memories',
    memoryFinal: 'Final Memory',
    questMain: 'Main Quests',
    questSide: 'Side Quests',
    questShrine: 'Shrine Quests',
    questDlc: 'DLC Quests',
    chest: 'Chests',
    weapon: 'Weapons Obtained',
    pbEquip: 'Compendium · Equipment',
    pbMat: 'Compendium · Materials',
    pbEnemy: 'Compendium · Monsters',
    bossHinox: 'Hinox',
    bossTalus: 'Stone Talus',
    bossMolduga: 'Molduga',
    saveOnlyHint: 'Save data only (manual marks excluded)',
    saveSync: 'Progress Sync',
    saveSyncOk: 'Full Live Sync'
  },
  settings: {
    title: 'Settings & Save Management',
    autoOpenMap: 'Auto open web map upon lock',
    cemuPath: 'Cemu BOTW save path (leave empty for auto):',
    ryuPath: 'Ryujinx BOTW save path (leave empty for auto):',
    edenPath: 'Eden BOTW save path (leave empty for auto):',
    pathAuto: 'Auto detection',
    pathOk: '✓ Valid save path',
    pathBad: '✕ Save not found',
    tls: 'Phone Screen Wake Lock (HTTPS)',
    tlsWarn: '(Requires root certificate)',
    saved: 'Settings saved'
  },
  about: {
    title: 'About Xnavi',
    desc: 'Unified 3-in-1 Tracker (BOTW × Cemu + Ryujinx + Eden)',
    version: 'Version',
    map: 'Map',
    github: 'GitHub',
    note: 'Read-only memory tracking. No injection. Safe to use.'
  },
  update: {
    available: 'Update Available',
    current: 'Current',
    latest: 'Latest'
  },
  log: {
    title: 'Live Logs',
    none: 'No logs yet',
    filter: 'Filter logs...'
  }
}

