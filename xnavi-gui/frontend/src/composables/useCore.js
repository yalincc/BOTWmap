import { ref, reactive, computed } from 'vue'
import { i18n } from '../i18n'

const t = (...args) => i18n.global.t(...args)
const api = window.go.main.App
const rt = window.runtime

export { api, rt }

export const ver = ref('')
export const edition = ref('emu')
export const coreRunning = ref(false)
export const coreBusy = ref(false)
export const webConnected = ref(false)
export const updateInfo = ref(null)

export const pos = reactive({
  ok: false, gx: 0, gy: 0, gz: 0, mx: 0, my: 0,
  layer: 0, verified: false, source: '-', copies: 0,
  lockAddr: '', ageSec: 0, pid: 0
})

export const progressData = ref({})

export const cfg = reactive({
  cemuSaveDir: '', ryujinxSaveDir: '', edenSaveDir: '',
  emulator: 'auto', game: 'auto', locateMode: 'auto', tls: false,
  autoOpenMap: true, autoDetectSave: true
})

export const envDetect = reactive({ cemu: false, ryujinx: false, eden: false })
export const detectedGame = ref('')

export const syncText = computed(() => {
  if (!coreRunning.value) return t('sync.waiting')
  if (!pos.ok) return pos.pid > 0 ? t('sync.attached', { pid: pos.pid }) : t('sync.waitingProcess')
  return webConnected.value ? t('sync.syncing') : t('sync.waitWeb')
})

export const syncDot = computed(() => {
  if (!coreRunning.value) return 'bg-slate-600'
  if (!pos.ok) return 'bg-sky-400'
  return webConnected.value ? 'bg-emerald-400' : 'bg-amber-400'
})

export const syncClass = computed(() => {
  if (!coreRunning.value) return 'text-slate-500'
  if (!pos.ok) return 'text-sky-400'
  return webConnected.value ? 'text-emerald-400' : 'text-amber-400'
})

const STEP_KEYS = ['ready', 'scanning', 'verifying', 'locked']
export const steps = ref(STEP_KEYS.map(key => ({ key, state: key === 'ready' ? 'done' : 'idle' })))

export const mapAutoOpened = ref(false)

function setStepState() {
  const idx = !coreRunning.value ? 0 : (!pos.ok ? 1 : (!pos.verified ? 2 : 3))
  steps.value.forEach((s, i) => { s.state = i < idx ? 'done' : (i === idx ? 'active' : 'idle') })
  // 必须严格在完全锁定（verified=true）后才打开地图网页，彻底杜绝 Eden 抢跑触发 30s 冷却
  if (pos.verified && !mapAutoOpened.value && cfg.autoOpenMap !== false) {
    mapAutoOpened.value = true
    openMap()
  }
}

// v3.2.1：在线地图地址写死（xnavi 只服务 BOTW）。
// follow=1 由网页端 app/js/live.js 消费 → 打开即激活右下角「跟随玩家位置」并持久化。
export const MAP_URL = 'https://botw.yalin.site/?follow=1&game=botw'

// 用 Wails 自带的浏览器打开能力：走系统 ShellExecute，URL 里的 & 不会被 shell 截断。
export function openMap() {
  rt.BrowserOpenURL(MAP_URL)
  api.LogMapOpened(MAP_URL).catch(() => {})
}

export const logs = ref([])
export const logFilter = ref('')

export const filteredLogs = computed(() => {
  const kw = logFilter.value.trim().toLowerCase()
  if (!kw) return logs.value
  return logs.value.filter(l => (l.text + ' ' + l.level).toLowerCase().includes(kw))
})

function legacyLogLevel(text) {
  // Bracketed module names and quoted level tags are metadata, not legacy keywords.
  text = text.replace(/\[(?:ok|info|warn|error)\]/gi, '')
  if (/\b(error|failed|panic|fatal)\b|失败/i.test(text)) return 'error'
  if (/\b(warn(?:ing)?|disconnected)\b|!!|占用|回退|失效|断开|falling back/i.test(text)) return 'warn'
  if (/confirmed live|confirmed\]|完全锁定|\blocked!|confirmed!|定位已生效|\[web\] connected/i.test(text)) return 'ok'
  return 'info'
}

function parseLogLine(line) {
  const stamp = line.match(/^\d{4}-\d{2}-\d{2} (\d{2}:\d{2}:\d{2}\.\d{3})(?:\s+|$)/)
  const time = stamp ? stamp[1] : new Date().toTimeString().slice(0, 8)
  let text = line.slice(stamp ? stamp[0].length : 0).trimStart().replace(/^\[sm\]\s*/i, '')
  const tag = text.match(/^\[(ok|info|warn|error)\]\s*/i)
  const level = tag ? tag[1].toLowerCase() : legacyLogLevel(text)
  if (tag) text = text.slice(tag[0].length)
  return { time, text, level }
}

export function pushLogs(lines) {
  if (!lines || !lines.length) return
  const items = lines.map(parseLogLine)
  logs.value = logs.value.concat(items)
  if (logs.value.length > 2000) logs.value = logs.value.slice(logs.value.length - 2000)
}

export function clearLogs() { logs.value = [] }

let lockedAtMs = 0
let envDiagDone = false

async function startCore() {
  lockedAtMs = 0
  envDiagDone = false
  mapAutoOpened.value = false
  const message = await api.StartCore(edition.value === 'pc' ? 'pc' : cfg.emulator)
  coreRunning.value = await api.CoreRunning()
  if (!coreRunning.value) throw new Error(message)
}

export async function toggleLocating() {
  if (coreBusy.value) return
  coreBusy.value = true
  try {
    if (coreRunning.value) {
      await api.StopCore()
      coreRunning.value = false
      mapAutoOpened.value = false
    } else {
      await startCore()
    }
  } finally {
    coreBusy.value = false
  }
}

export async function changeLocateConfig(emulator, locateMode) {
  if (coreBusy.value) return
  if (!['eden', 'auto'].includes(emulator)) locateMode = 'auto'
  if (emulator === cfg.emulator && locateMode === cfg.locateMode) return
  coreBusy.value = true
  try {
    await api.SaveConfig({ ...cfg, emulator, locateMode })
    Object.assign(cfg, { emulator, locateMode })
    // StartCore replaces the current process under the backend lock.
    if (coreRunning.value) await startCore()
  } finally {
    coreBusy.value = false
  }
}

export async function calibrateCoords(x, y, z) {
  return await api.Calibrate(Number(x), Number(y), Number(z))
}

export async function manualCalibrate(x, z) {
  return await api.ManualCalibrate(Number(x), Number(z))
}

export async function saveSettings() {
  const res = await api.SaveConfig({
    cemuSaveDir: cfg.cemuSaveDir,
    ryujinxSaveDir: cfg.ryujinxSaveDir,
    edenSaveDir: cfg.edenSaveDir,
    emulator: cfg.emulator,
    game: cfg.game,
    locateMode: cfg.locateMode,
    tls: cfg.tls,
    autoOpenMap: cfg.autoOpenMap,
    autoDetectSave: cfg.autoDetectSave
  })
  const det = await api.EnvDetect()
  envDetect.cemu = det.cemu
  envDetect.ryujinx = det.ryujinx
  envDetect.eden = det.eden
  return res
}

export async function loadConfig() {
  const c = await api.LoadConfig()
  cfg.cemuSaveDir = c.cemuSaveDir || ''
  cfg.ryujinxSaveDir = c.ryujinxSaveDir || ''
  cfg.edenSaveDir = c.edenSaveDir || ''
  cfg.emulator = c.emulator || 'auto'
  cfg.game = c.game || 'auto'
  cfg.locateMode = c.locateMode || 'auto'
  cfg.tls = !!c.tls
  cfg.autoOpenMap = c.autoOpenMap !== undefined ? !!c.autoOpenMap : true
  cfg.autoDetectSave = c.autoDetectSave !== undefined ? !!c.autoDetectSave : true
  const det = await api.EnvDetect()
  envDetect.cemu = det.cemu
  envDetect.ryujinx = det.ryujinx
  envDetect.eden = det.eden
}

export async function applySave(p) {
  const msg = await api.ApplySavePath(p)
  await loadConfig()
  return msg
}

export async function detectSave() {
  const msg = await api.ResetSaveDetection()
  await loadConfig()
  return msg
}

export const saveMtime = ref(0)
export const nowSec = ref(Math.floor(Date.now() / 1000))

setInterval(() => {
  nowSec.value = Math.floor(Date.now() / 1000)
}, 1000)

export const saveTimeAgoText = computed(() => {
  if (!saveMtime.value || saveMtime.value <= 0) return ''
  const diff = Math.max(0, nowSec.value - saveMtime.value)
  if (diff < 60) return t('locate.justNow')
  const mins = Math.floor(diff / 60)
  if (mins < 60) return t('locate.minsAgo', { m: mins })
  const hours = Math.floor(mins / 60)
  return t('locate.hoursAgo', { h: hours })
})

export const saveTimeAgoClass = computed(() => {
  if (!saveMtime.value || saveMtime.value <= 0) return 'text-slate-400'
  const diff = Math.max(0, nowSec.value - saveMtime.value)
  if (diff < 120) return 'text-emerald-400 font-semibold'
  if (diff < 3600) return 'text-slate-300'
  return 'text-amber-400'
})

export async function exportDiag() { await api.ExportDiagnostics() }
export async function uploadDiag(note) { return await api.UploadDiagnostics(note || '') }
export async function openLogDir() { await api.OpenLogDir() }
export async function openSave() { return await api.OpenSaveDir() }
export async function pickSaveFile() {
  const f = await api.PickSaveFile()
  if (f) return await applySave(f)
}
export async function pickSaveDir() {
  const d = await api.PickDir("选择 BOTW 存档目录")
  if (d) return await applySave(d)
}

export async function initCore() {
  ver.value = await api.Version()
  try { edition.value = await api.Edition() } catch (e) { edition.value = 'emu' }
  await loadConfig()
  const initial = await api.TailLog()
  pushLogs(initial)
  rt.EventsOn('log:append', pushLogs)
  rt.EventsOn('update:available', (info) => { updateInfo.value = info })
  rt.EventsOn('status:update', (st) => {
    coreRunning.value = !!st.running
    webConnected.value = !!st.webConnected
    pos.ok = !!st.ok
    if (st.running && st.verified && !st.webConnected) {
      if (!lockedAtMs) lockedAtMs = Date.now()
      else if (!envDiagDone && Date.now() - lockedAtMs >= 8000) {
        envDiagDone = true
        api.RunEnvDiag().catch(() => {})
      }
    } else if (!st.running || st.webConnected) {
      lockedAtMs = 0
    }
    pos.pid = st.pid || 0
    if (st.pos) {
      pos.gx = st.pos.gx || 0
      pos.gy = st.pos.gy || 0
      pos.gz = st.pos.gz || 0
      pos.mx = st.pos.mx || 0
      pos.my = st.pos.my || 0
    }
    pos.verified = !!st.verified
    pos.source = st.source || '-'
    pos.copies = st.copies || 0
    pos.lockAddr = st.lockAddr || ''
    pos.ageSec = st.ageSec || 0
    if (st.game) detectedGame.value = st.game

    if (st.saveMtime) {
      saveMtime.value = st.saveMtime
    } else if (st.progress && st.progress.saveMtime) {
      saveMtime.value = st.progress.saveMtime
    } else if (!st.ok) {
      // v3.2.1：未取得本次会话的签名快照时清空，避免继续显示上次会话的相对时间
      saveMtime.value = 0
    }

    if (st.progress && st.progress.counts) {
      progressData.value = st.progress
    }
    if (st.save) {
      if (!progressData.value) progressData.value = {}
      progressData.value.save = st.save
    }
    if (st.saveSource) {
      if (!progressData.value) progressData.value = {}
      progressData.value.saveSource = st.saveSource
    }
    setStepState()
  })
}
