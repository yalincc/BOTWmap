// Bridge to the Wails backend.
//
// Inside the desktop app the real bindings live on `window.go.main.App` and
// `window.runtime`. When the page is opened in a plain browser (`npm run dev`)
// a tiny mock keeps the layout previewable without a Go backend.
//
// The mock is a preview aid only: it is used exclusively when the Wails
// runtime is absent, so the shipped desktop build always talks to the real
// `window.go.main.App` bindings.

const realApi = (typeof window !== 'undefined' && window.go && window.go.main && window.go.main.App) ? window.go.main.App : null
const realRuntime = (typeof window !== 'undefined') ? (window.runtime || null) : null

// ---------------------------------------------------------------- event bus

const listeners = Object.create(null)

function on(name, cb) {
  if (realRuntime && realRuntime.EventsOn) return realRuntime.EventsOn(name, cb)
  const arr = listeners[name] || (listeners[name] = [])
  arr.push(cb)
  return () => {
    const i = arr.indexOf(cb)
    if (i >= 0) arr.splice(i, 1)
  }
}

function emit(name, payload) {
  if (realRuntime && realRuntime.EventsEmit) { realRuntime.EventsEmit(name, payload); return }
  const arr = (listeners[name] || []).slice()
  for (const cb of arr) {
    try { cb(payload) } catch (e) { console.error(e) }
  }
}

function browserOpen(url) {
  if (realRuntime && realRuntime.BrowserOpenURL) { realRuntime.BrowserOpenURL(url); return }
  console.log('[mock] BrowserOpenURL', url)
}

// ------------------------------------------------------------------- mock

function pad(n) { return String(n).padStart(2, '0') }

function stamp() {
  const d = new Date()
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${String(d.getMilliseconds()).padStart(3, '0')}`
}

function createMockApi() {
  const st = {
    running: false, ok: false, verified: false, source: 'lost', note: '未启动',
    gx: 0, gy: 0, gz: 0, mx: 0, my: 0, addr: 0, scans: 0, saves: 0, shrine: false,
    anchor: { X: 0, Y: 0, Z: 0 }, pid: 0, webOK: false, webNote: ''
  }
  let timer = null
  let tick = 0
  let backlog = []

  function push(text) {
    const line = `${stamp()} [mock] ${text}`
    backlog.push(line)
    emit('log:append', [line])
  }

  function snapshot() {
    return Object.assign({}, st, { anchor: Object.assign({}, st.anchor) })
  }

  function advance() {
    tick++
    if (!st.running) return snapshot()
    st.scans++
    st.gx += Math.sin(tick / 3) * 2
    st.gz += Math.cos(tick / 3) * 2
    st.gy = 114.3 + Math.sin(tick / 5)
    st.mx = Math.round(st.gx * 1.7)
    st.my = Math.round(st.gz * 1.7)
    st.ok = tick > 4
    st.verified = tick > 8
    st.shrine = tick > 24 && tick < 34
    st.source = st.shrine ? 'shrine' : (st.verified ? 'player' : 'scan')
    st.note = st.shrine ? '神庙挂起，等待玩家继续移动' : (st.verified ? '已锁定，跟随中' : '扫描中，请在游戏里走动')
    st.addr = 0x1B1A2CA279C
    st.saves = Math.floor(tick / 6)
    st.pid = 4321
    st.webOK = true
    st.webNote = 'http://127.0.0.1:8766'
    if (tick === 5) push('[pos] 首次命中候选地址，等待走动确认')
    if (tick === 9) push('[pos] 坐标已锁定 ✓ verified=true src=player')
    if (tick === 25) push('[shrine] 检测到神庙挂起 !! 暂停跟随')
    if (tick === 35) push('[pos] 恢复跟随')
    return snapshot()
  }

  function tickTimer() {
    if (timer) return
    timer = setInterval(() => { emit('status:update', advance()) }, 800)
  }

  return {
    Version: async () => 'v0.1.0',
    CoreStatus: async () => snapshot(),
    CoreRunning: async () => st.running,
    StartCore: async () => {
      st.running = true; tick = 0; st.anchor = { X: 996.7, Y: 114.3, Z: 1726.8 }
      push('核心已启动（Eden / BOTW）')
      tickTimer()
      emit('status:update', advance())
      return '核心已启动（Eden / BOTW）'
    },
    StopCore: async () => {
      st.running = false; st.ok = false; st.verified = false; st.source = 'lost'; st.note = '已停止'
      if (timer) { clearInterval(timer); timer = null }
      push('核心已停止')
      emit('status:update', snapshot())
      return '已停止'
    },
    Rescan: async () => { push('已提交重新扫描'); emit('status:update', advance()); return '已提交重新扫描' },
    PickDir: async (title) => { console.log('[mock] PickDir', title); return 'D:\\Eden\\games\\botw\\save' },
    OpenMap: async () => { browserOpen('https://botw.yalin.site/?follow=1&game=botw'); return 'ok' },
    OpenLogDir: async () => { push('打开日志目录'); return 'ok' },
    ExportDiagnostics: async () => { push('导出诊断包'); return 'C:\\Users\\tester\\enavi-diagnostics.zip' },
    SaveConfig: async (cfg) => { console.log('[mock] SaveConfig', cfg); return '已保存' },
    LoadConfig: async () => ({ edenSaveDir: '' }),
    PollLogs: async () => { const l = backlog; backlog = []; return l },
    TailLog: async () => backlog.slice()
  }
}

// ------------------------------------------------------------------- export

const mockApi = createMockApi()

// The Go side is the source of truth. A method that the backend does not
// expose yet must not break the UI, so every call goes through a guard that
// falls back to the mock value and reports the gap to the console.
const MISSING = (name) => `后端未实现 ${name}()`

export const api = realApi
  ? new Proxy({}, {
    get(_t, name) {
      if (typeof name !== 'string') return undefined
      const fn = realApi[name]
      if (typeof fn === 'function') {
        return (...args) => Promise.resolve()
          .then(() => fn.apply(realApi, args))
          .catch((e) => { console.error(`[enavi] ${name} failed:`, e); return MISSING(name) })
      }
      console.warn(`[enavi] backend has no method ${name}()`)
      return mockApi[name]
    }
  })
  : mockApi

export const rt = {
  EventsOn: on,
  EventsEmit: emit,
  BrowserOpenURL: browserOpen
}

export const isMock = !realApi
