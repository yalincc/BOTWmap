import { ref, reactive, computed, nextTick } from 'vue'
import { i18n } from '../i18n'

const t = (...args) => i18n.global.t(...args)
const api = window.go.main.App
const rt = window.runtime

export { api, rt }

export const ver = ref('')
export const coreRunning = ref(false)
export const isLocating = ref(false)
export const updateInfo = ref(null)

export const pos = reactive({
  ok: false, gx: 0, gy: 0, gz: 0, mx: 0, my: 0,
  layer: 0, verified: false, source: '-', copies: 0,
  lockAddr: '', ageSec: 0, pid: 0
})

export const progressData = ref({})

export const cfg = reactive({
  cemuSaveDir: '', ryujinxSaveDir: '', edenSaveDir: '',
  emulator: 'auto', game: 'auto', tls: false,
  autoOpenMap: true, customMapURL: '', autoDetectSave: true
})

export const envDetect = reactive({ cemu: false, ryujinx: false, eden: false })
export const statusMapUrl = ref('')
export const detectedGame = ref('')

export const syncText = computed(() => {
  if (coreRunning.value && pos.ok) return t('sync.syncing')
  if (coreRunning.value) {
    return pos.pid > 0 ? t('sync.attached', { pid: pos.pid }) : t('sync.waitingProcess')
  }
  return t('sync.waiting')
})

export const syncDot = computed(() => {
  if (coreRunning.value && pos.ok) return 'bg-emerald-400'
  if (coreRunning.value) return 'bg-amber-400 animate-pulse'
  return 'bg-slate-600'
})

export const syncClass = computed(() => coreRunning.value ? 'text-amber-400' : 'text-slate-500')

const STEP_KEYS = ['ready', 'scanning', 'verifying', 'locked']
export const steps = ref(STEP_KEYS.map(key => ({ key, state: key === 'ready' ? 'done' : 'idle' })))
export function stepClass(s) { return { done: 'text-emerald-400', active: 'text-amber-400 font-semibold', idle: 'text-slate-600' }[s] }
export function stepDot(s) { return { done: 'bg-emerald-400', active: 'bg-amber-400 animate-pulse', idle: 'bg-slate-700' }[s] }

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

export const defaultMapUrl = computed(() => {
  if (statusMapUrl.value) return statusMapUrl.value
  const g = detectedGame.value || 'botw'
  return g === 'totk' ? 'https://totk.yalin.site/' : 'https://botw.yalin.site/'
})

export function buildMapUrl() {
  const base = cfg.customMapURL && cfg.customMapURL.trim() ? cfg.customMapURL.trim() : defaultMapUrl.value
  let g = detectedGame.value
  if (!g || g === 'auto') g = (cfg.game && cfg.game !== 'auto') ? cfg.game : 'botw'
  const sep = base.includes('?') ? '&' : '?'
  return `${base}${sep}follow=1&game=${g}`
}

export async function openMap() {
  const url = buildMapUrl()
  if (api.OpenMapURL) {
    await api.OpenMapURL(url)
  } else {
    rt.BrowserOpenURL(url)
  }
}

export const logs = ref([])
export const logFilter = ref('')

export const filteredLogs = computed(() => {
  const kw = logFilter.value.trim().toLowerCase()
  if (!kw) return logs.value
  return logs.value.filter(l => (l.text + ' ' + l.level).toLowerCase().includes(kw))
})

export function pushLogs(lines) {
  if (!lines || !lines.length) return
  const items = lines.map(l => {
    const m = l.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) ?(.*)$/)
    const time = m ? m[1].slice(11) : new Date().toTimeString().slice(0, 8)
    let text = m ? m[2] : l
    let level = 'info', color = 'text-slate-400', levelColor = 'text-sky-500'
    if (/ERROR|FAILED|panic/i.test(text)) { level = 'error'; color = 'text-red-400'; levelColor = 'text-red-500' }
    else if (/!!|占用/i.test(text)) { level = 'warn'; color = 'text-amber-400'; levelColor = 'text-amber-500' }
    else if (/confirmed live|confirmed\]|完全锁定|locked!|confirmed!/i.test(text)) { level = 'ok'; color = 'text-emerald-400'; levelColor = 'text-emerald-500' }
    else if (/\[verify\]|候选/i.test(text)) { level = 'warn'; color = 'text-amber-400'; levelColor = 'text-amber-500' }
    else if (/\[scan\]/i.test(text)) { level = 'info'; color = 'text-sky-400'; levelColor = 'text-sky-500' }
    else if (/\[shrine\]|\[warp\]/i.test(text)) { level = 'info'; color = 'text-teal-400'; levelColor = 'text-teal-500' }
    text = text.replace(/^\[sm\]\s*/, '')
    return { time, text, level, color, levelColor }
  })
  logs.value = logs.value.concat(items)
  if (logs.value.length > 2000) logs.value = logs.value.slice(logs.value.length - 2000)
}

export function clearLogs() { logs.value = [] }

export async function toggleLocating() {
  if (isLocating.value) {
    await api.StopCore()
    isLocating.value = false
    mapAutoOpened.value = false
  } else {
    mapAutoOpened.value = false
    await api.StartCore(cfg.emulator)
    isLocating.value = true
  }
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
    tls: cfg.tls,
    autoOpenMap: cfg.autoOpenMap,
    customMapURL: cfg.customMapURL,
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
  cfg.tls = !!c.tls
  cfg.autoOpenMap = c.autoOpenMap !== undefined ? !!c.autoOpenMap : true
  cfg.customMapURL = c.customMapURL || ''
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
  await loadConfig()
  const initial = await api.TailLog()
  pushLogs(initial)
  rt.EventsOn('log:append', pushLogs)
  rt.EventsOn('update:available', (info) => { updateInfo.value = info })
  rt.EventsOn('status:update', (st) => {
    coreRunning.value = !!st.running
    pos.ok = !!st.ok
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
    if (st.map_url) statusMapUrl.value = st.map_url

    if (st.saveMtime) {
      saveMtime.value = st.saveMtime
    } else if (st.progress && st.progress.saveMtime) {
      saveMtime.value = st.progress.saveMtime
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
