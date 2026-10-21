<template>
  <div class="flex flex-col h-screen w-full bg-[#0d1117] text-slate-200 select-none overflow-hidden font-sans">
    <header class="flex items-center justify-between px-4 py-2 bg-[#161b22] border-b border-slate-800 shrink-0">
      <div class="flex items-center space-x-2 text-sm font-semibold tracking-wide">
        <span class="w-2.5 h-2.5 rounded-full bg-emerald-400 shadow-sm shadow-emerald-500/50"></span>
        <span class="text-white">Enavi · Eden 定位伴侣</span>
        <span class="text-xs text-slate-400 font-mono">{{ ver }}</span>
      </div>
      <div class="flex items-center space-x-4 text-xs">
        <div class="flex items-center space-x-1.5 font-mono" :class="syncClass">
          <span class="w-2 h-2 rounded-full inline-block" :class="syncDot"></span>
          <span>{{ syncText }}</span>
        </div>
        <button class="text-slate-400 hover:text-white transition" @click="showSettings = true">⚙ 设置</button>
        <button class="text-slate-400 hover:text-white transition" @click="openMap">🌐 地图</button>
        <button class="text-slate-400 hover:text-white transition" @click="showAbout = true">ℹ 关于</button>
      </div>
    </header>

    <main class="flex-1 flex flex-col p-3 gap-3 overflow-hidden">
      <!-- 顶部三卡：运行环境 / 开始停止 / 定位状态 -->
      <div class="grid grid-cols-12 gap-3 shrink-0">
        <!-- 左：运行环境 -->
        <section class="col-span-12 md:col-span-5 bg-[#161b22] border border-slate-800 rounded-md p-3">
          <div class="text-[11px] font-semibold text-slate-400 mb-2 uppercase tracking-wider">运行环境</div>
          <div>
            <label class="block text-xs text-slate-400 mb-1">Eden BOTW 存档目录：</label>
            <div class="flex gap-1">
              <input v-model="cfg.edenSaveDir" placeholder="自动探测" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none" />
              <button @click="pickDir" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">浏览</button>
            </div>
          </div>
          <div class="text-[11px] font-mono mt-2" :class="detectClass">{{ detectText }}</div>
          <div class="text-[11px] font-mono mt-1 text-slate-500">
            PID：<span class="text-slate-300">{{ st.pid > 0 ? st.pid : '—' }}</span>
            <span class="mx-1 text-slate-700">|</span>
            Web：<span :class="st.webOK ? 'text-emerald-400' : 'text-slate-400'">{{ st.webOK ? '可用' : '不可用' }}</span>
            <span v-if="st.webNote" class="text-slate-600 ml-1">{{ st.webNote }}</span>
          </div>
        </section>

        <!-- 中：开始 / 停止 -->
        <section class="col-span-12 md:col-span-2 flex">
          <button @click="toggleLocating"
            :class="st.running ? 'bg-amber-600 hover:bg-amber-500' : 'bg-blue-600 hover:bg-blue-500'"
            class="w-full rounded flex items-center justify-center font-bold text-sm text-white shadow-lg transition active:scale-95">
            {{ st.running ? '■ 停止' : '▶ 开始定位' }}
          </button>
        </section>

        <!-- 右：定位状态 -->
        <section class="col-span-12 md:col-span-5 bg-gradient-to-r from-emerald-950/30 to-[#161b22] border border-emerald-500/30 rounded-md p-3">
          <div class="flex items-center gap-1.5 mb-1.5">
            <div class="text-[11px] text-emerald-400 font-medium">定位状态</div>
            <span class="px-1.5 py-0.5 rounded text-[10px] font-semibold"
              :class="st.verified ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/40' : 'bg-amber-500/20 text-amber-400 border border-amber-500/40'">
              {{ st.verified ? '已验证' : '未验证' }}
            </span>
            <span v-if="st.shrine" class="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-amber-500/20 text-amber-300 border border-amber-500/50 animate-pulse">
              神庙挂起
            </span>
            <span v-if="st.source" class="ml-auto text-[10px] font-mono text-slate-500">src={{ st.source }}</span>
          </div>
          <div class="grid grid-cols-2 gap-x-4 gap-y-1 font-mono text-[12px]">
            <div class="text-slate-400">X <span class="text-white font-bold">{{ fmt(st.gx) }}</span></div>
            <div class="text-slate-400">alt <span class="text-white font-bold">{{ fmt(st.gy) }}</span></div>
            <div class="text-slate-400">Z <span class="text-white font-bold">{{ fmt(st.gz) }}</span></div>
            <div class="text-slate-400">地图像素 <span class="text-white font-bold">{{ fmtInt(st.mx) }}, {{ fmtInt(st.my) }}</span></div>
            <div class="text-slate-400">地址 <span class="text-white font-bold">{{ hexAddr }}</span></div>
            <div class="text-slate-400">扫描次数 <span class="text-white font-bold">{{ st.scans }}</span></div>
          </div>
          <div class="text-[11px] font-mono mt-1.5 text-slate-400">
            锚点 <span class="text-slate-300">{{ anchorText }}</span>
          </div>
          <div class="text-[11px] mt-1 text-slate-400 truncate" :title="st.note">{{ st.note || '—' }}</div>
        </section>
      </div>

      <!-- 步骤条 -->
      <section class="bg-[#161b22] border border-slate-800 rounded-md p-3 shrink-0">
        <div class="flex items-center justify-center gap-4">
          <template v-for="(s, i) in steps" :key="s.name">
            <div class="flex items-center space-x-1.5" :class="stepClass(s.state)">
              <span class="w-2 h-2 rounded-full" :class="stepDot(s.state)"></span>
              <span class="text-xs font-mono">{{ s.name }}</span>
            </div>
            <div v-if="i < steps.length - 1" class="w-6 h-px bg-slate-700"></div>
          </template>
        </div>
        <div class="text-xs mt-2 text-center">
          <span :class="stateTextClass">{{ stateText }}</span>
          <span v-if="st.shrine" class="text-amber-400 ml-2">· 神庙挂起中</span>
        </div>
      </section>

      <!-- 日志面板 -->
      <section class="bg-[#0d1117] border border-slate-800 rounded-md p-2.5 flex flex-col font-mono text-[11px] flex-1 min-h-0">
        <div class="flex items-center justify-between pb-1.5 mb-1.5 border-b border-slate-800/80 text-slate-400 text-[10px] shrink-0">
          <span>实时日志</span>
          <div class="space-x-3">
            <button class="hover:text-slate-200" @click="clearLogs">清屏</button>
            <button class="hover:text-slate-200" @click="exportDiag">导出诊断包</button>
            <button class="hover:text-slate-200" @click="openLogDir">打开目录</button>
            <button class="hover:text-blue-300 text-blue-400" @click="showManualCalib = true">手工校准</button>
            <button class="hover:text-slate-200" @click="rescan">重新扫描</button>
          </div>
        </div>
        <div ref="logBox" class="flex-1 overflow-y-auto space-y-0.5 leading-relaxed text-slate-400 min-h-0 select-text">
          <div v-for="(log, idx) in logs" :key="idx" :class="log.color">
            <span class="text-slate-500">[{{ log.time }}]</span> {{ log.text }}
          </div>
        </div>
      </section>
    </main>

    <!-- 操作结果提示 -->
    <div v-if="toast" class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2 rounded-md text-xs shadow-xl border"
      :class="toastClass" :title="toast">
      {{ toast }}
    </div>

    <!-- 设置弹窗 -->
    <div v-if="showSettings" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="showSettings = false">
      <div class="bg-[#161b22] border border-slate-700 rounded-lg p-5 w-96">
        <div class="text-sm font-bold text-white mb-4">设置</div>
        <div class="space-y-3 text-xs">
          <div>
            <label class="block text-slate-400 mb-1">Eden 的 BOTW 存档目录（留空自动探测）：</label>
            <div class="flex gap-1">
              <input v-model="cfg.edenSaveDir" placeholder="自动探测" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white focus:border-blue-500 focus:outline-none" />
              <button @click="pickDir" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">浏览</button>
            </div>
          </div>
          <div class="text-slate-500">只读该存档目录做定位锚点，填错了换个正确的即可。</div>
        </div>
        <div class="flex justify-end gap-2 mt-4">
          <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs" @click="showSettings = false">取消</button>
          <button class="bg-blue-600 hover:bg-blue-500 px-4 py-1.5 rounded text-xs text-white" @click="saveSettings">保存</button>
        </div>
      </div>
    </div>

    <!-- 手工校准弹窗 -->
    <div v-if="showManualCalib" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="showManualCalib = false">
      <div class="bg-[#161b22] border border-blue-500/40 rounded-lg p-5 w-96 shadow-2xl">
        <div class="text-sm font-bold text-white mb-2 flex items-center justify-between">
          <span>手工坐标校准（快速找人）</span>
          <span class="text-[11px] text-blue-400 font-normal">BOTW 仅需 XZ</span>
        </div>
        <div class="text-xs text-slate-400 mb-3 leading-relaxed">
          若长时间未存档或自动定位偏差，建议在游戏里<span class="text-white">传送到神庙或高塔</span>，输入该地标大概坐标（或点下方快捷预设）定向秒搜：
        </div>
        <div class="grid grid-cols-2 gap-2 mb-3">
          <div>
            <label class="block text-slate-400 text-[11px] mb-1">X 坐标：</label>
            <input v-model="calibX" type="number" step="0.1" placeholder="例如 -215.0" class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white font-mono text-xs focus:border-blue-500 focus:outline-none" />
          </div>
          <div>
            <label class="block text-slate-400 text-[11px] mb-1">Z 坐标：</label>
            <input v-model="calibZ" type="number" step="0.1" placeholder="例如 250.0" class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white font-mono text-xs focus:border-blue-500 focus:outline-none" />
          </div>
        </div>
        <div class="flex items-center gap-1.5 flex-wrap mb-4 text-[11px]">
          <span class="text-slate-500">快捷预设:</span>
          <button @click="fillPreset(-215, 250)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300">平原之塔</button>
          <button @click="fillPreset(-1071, 1913)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300">复苏神庙</button>
          <button @click="fillPreset(3435, 1968)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300">哈特诺塔</button>
          <button @click="fillPreset(3780, 2111)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300">哈特诺所</button>
        </div>
        <div class="flex justify-end gap-2">
          <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs text-slate-300" @click="showManualCalib = false">取消</button>
          <button class="bg-blue-600 hover:bg-blue-500 px-4 py-1.5 rounded text-xs text-white font-medium" @click="submitManualCalib">以此坐标定向找人</button>
        </div>
      </div>
    </div>

    <!-- 关于弹窗 -->
    <div v-if="showAbout" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="showAbout = false">
      <div class="bg-[#161b22] border border-slate-700 rounded-lg p-5 w-96 text-xs space-y-2">
        <div class="text-sm font-bold text-white mb-2">关于 Enavi</div>
        <div class="text-slate-300">Eden 定位伴侣（Eden × 旷野之息）</div>
        <div class="text-slate-400">版本：{{ ver }}</div>
        <div class="text-slate-400">地图：<a :href="mapUrl" class="text-blue-400 hover:underline" @click.prevent="openMap">botw.yalin.site</a></div>
        <div class="text-slate-400">GitHub：<a href="https://github.com/yalincc/BOTWmap" class="text-blue-400 hover:underline">yalincc/BOTWmap</a></div>
        <div class="text-slate-400 mt-2">只读内存，不注入，不修改游戏。</div>
        <div class="flex justify-end mt-3">
          <button class="bg-slate-700 hover:bg-slate-600 px-4 py-1.5 rounded text-xs text-white" @click="showAbout = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { api, rt } from './bridge.js'

const ver = ref('v0.1.0')
const showSettings = ref(false)
const showAbout = ref(false)
const showManualCalib = ref(false)
const calibX = ref('')
const calibZ = ref('')
const toast = ref('')
let toastTimer = null

function fillPreset(x, z) {
  calibX.value = String(x)
  calibZ.value = String(z)
}

async function submitManualCalib() {
  const x = Number(calibX.value)
  const z = Number(calibZ.value)
  if (isNaN(x) || isNaN(z)) {
    notify('请输入合法的 X 与 Z 数值')
    return
  }
  showManualCalib.value = false
  const msg = await api.RescanManual(x, z)
  notify(msg || '已提交手工校准定向找人')
}

const mapUrl = 'https://botw.yalin.site'

// ------------------------------------------------------------- status state

const st = reactive({
  running: false, ok: false, verified: false, source: '', note: '',
  gx: 0, gy: 0, gz: 0, mx: 0, my: 0, addr: 0, scans: 0, saves: 0, shrine: false,
  anchor: { X: 0, Y: 0, Z: 0 }, pid: 0, webOK: false, webNote: ''
})

const cfg = reactive({ edenSaveDir: '' })

function applyStatus(s) {
  if (!s || typeof s !== 'object') return
  st.running = !!s.running
  st.ok = !!s.ok
  st.verified = !!s.verified
  st.source = s.source || ''
  st.note = s.note || ''
  st.gx = num(s.gx)
  st.gy = num(s.gy)
  st.gz = num(s.gz)
  st.mx = num(s.mx)
  st.my = num(s.my)
  st.addr = num(s.addr)
  st.scans = num(s.scans)
  st.saves = num(s.saves)
  st.shrine = !!s.shrine
  st.pid = num(s.pid)
  st.webOK = !!s.webOK
  st.webNote = s.webNote || ''
  if (s.anchor && typeof s.anchor === 'object') {
    st.anchor = {
      X: num(s.anchor.X !== undefined ? s.anchor.X : s.anchor.x),
      Y: num(s.anchor.Y !== undefined ? s.anchor.Y : s.anchor.y),
      Z: num(s.anchor.Z !== undefined ? s.anchor.Z : s.anchor.z)
    }
  }
}

function num(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function fmt(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(1) : '—'
}

function fmtInt(v) {
  const n = Number(v)
  return Number.isFinite(n) ? String(Math.round(n)) : '—'
}

const hexAddr = computed(() => (st.addr > 0 ? '0x' + st.addr.toString(16).toUpperCase() : '—'))

const anchorText = computed(() => {
  const a = st.anchor
  if (!a) return '—'
  const any = a.X || a.Y || a.Z
  if (!any) return '—'
  return `(${a.X.toFixed(1)}, ${a.Y.toFixed(1)}, ${a.Z.toFixed(1)})`
})

// -------------------------------------------------------------- header sync

const syncText = computed(() => {
  if (!st.running) return '等待'
  return st.ok ? '同步中' : '定位中'
})
const syncDot = computed(() => {
  if (!st.running) return 'bg-slate-600'
  return st.ok ? 'bg-emerald-400' : 'bg-amber-400 animate-pulse'
})
const syncClass = computed(() => {
  if (!st.running) return 'text-slate-500'
  return st.ok ? 'text-emerald-400' : 'text-amber-400'
})

// ------------------------------------------------------------------ 运行环境

const detectText = computed(() => {
  if (cfg.edenSaveDir) return '✓ 已手动配置 Eden 存档目录'
  return '未手动配置存档目录（点开始后自动探测存档）'
})
const detectClass = computed(() => (cfg.edenSaveDir ? 'text-emerald-400' : 'text-amber-400'))

// ---------------------------------------------------------------------- 步骤

const STEP_NAMES = ['就绪', '扫描中', '验证移动', '已锁定']
const steps = ref([
  { name: '就绪', state: 'done' },
  { name: '扫描中', state: 'idle' },
  { name: '验证移动', state: 'idle' },
  { name: '已锁定', state: 'idle' }
])

function stepClass(s) {
  return { done: 'text-emerald-400', active: 'text-amber-400 font-semibold', idle: 'text-slate-600' }[s] || 'text-slate-600'
}
function stepDot(s) {
  return { done: 'bg-emerald-400', active: 'bg-amber-400 animate-pulse', idle: 'bg-slate-700' }[s] || 'bg-slate-700'
}
function setStep(doneCount, activeName) {
  steps.value.forEach(s => { s.state = 'idle' })
  STEP_NAMES.slice(0, doneCount).forEach(n => {
    const t = steps.value.find(x => x.name === n)
    if (t) t.state = 'done'
  })
  const act = steps.value.find(x => x.name === activeName)
  if (act) act.state = 'active'
}

const stateText = computed(() => {
  if (!st.running) return '就绪'
  if (st.shrine) return '已锁定 · 神庙挂起'
  if (st.ok && st.verified) return '已锁定 · 跟随中'
  if (st.ok) return '验证移动中'
  return '扫描中'
})
const stateTextClass = computed(() => {
  if (!st.running) return 'text-slate-300'
  if (st.shrine) return 'text-amber-400 font-semibold'
  if (st.ok && st.verified) return 'text-emerald-400 font-semibold'
  if (st.ok) return 'text-amber-400 font-semibold'
  return 'text-amber-400 font-semibold'
})

function updateSteps() {
  if (!st.running) { setStep(1, '就绪'); return }
  if (st.shrine) { setStep(3, '已锁定'); return }
  if (st.ok && st.verified) { setStep(3, '已锁定'); return }
  if (st.ok) { setStep(2, '验证移动'); return }
  setStep(1, '扫描中')
}

// ------------------------------------------------------------------- 日志

const logs = ref([])
const logBox = ref(null)

function pushLogs(lines) {
  if (!lines || !lines.length) return
  const items = lines.map(parseLogLine)
  logs.value = logs.value.concat(items)
  if (logs.value.length > 3000) logs.value = logs.value.slice(logs.value.length - 3000)
  nextTick(() => { if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight })
}

const TS_RE = /^(\d{4})\/(\d{2})\/(\d{2})\s+(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,3}))?/

function parseLogLine(raw) {
  const line = String(raw == null ? '' : raw)
  const m = line.match(TS_RE)
  const time = m ? `${m[4]}:${m[5]}:${m[6]}` : nowTime()
  const text = m ? line.slice(m[0].length).trim() : line
  let color = 'text-slate-400'
  if (/ERROR|FAILED|panic/i.test(text)) color = 'text-red-400'
  else if (/失败|占用|重扫|!!/.test(text)) color = 'text-amber-400'
  else if (/已锁定|✓/.test(text)) color = 'text-emerald-400'
  return { time, text, color }
}

function nowTime() {
  const d = new Date()
  const p = n => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function clearLogs() { logs.value = [] }

// ------------------------------------------------------------------- 动作

function notify(msg) {
  if (!msg) return
  toast.value = String(msg)
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 4000)
}
const toastClass = computed(() => {
  const t = toast.value
  if (/失败|error|错误/i.test(t)) return 'bg-[#2d1214] border-red-500/50 text-red-300'
  if (/已提交|已保存|已停止|已启动|可用/.test(t)) return 'bg-[#0f2a1c] border-emerald-500/50 text-emerald-300'
  return 'bg-[#1c2433] border-slate-600 text-slate-200'
})

async function toggleLocating() {
  if (st.running) {
    const msg = await api.StopCore()
    notify(msg || '已停止')
  } else {
    const msg = await api.StartCore()
    notify(msg || '已提交启动')
  }
  // 立刻拉一次真实状态（不等 800ms 的事件轮询）→ 按钮和状态条立刻跟着变。
  // ★ 状态必须以服务端返回的 running 为准，不能靠本地猜测，
  //   否则「停止后界面还显示停止」这类 stale 状态会一直误导用户。
  try {
    applyStatus(await api.CoreStatus())
  } catch (e) { /* 忽略：下一轮事件会补上 */ }
}

async function pickDir() {
  const d = await api.PickDir('选择 Eden 的 BOTW 存档目录')
  if (d) cfg.edenSaveDir = d
}

async function saveSettings() {
  const msg = await api.SaveConfig({ edenSaveDir: cfg.edenSaveDir })
  showSettings.value = false
  notify(msg || '已保存')
}

async function openMap() {
  rt.BrowserOpenURL('https://botw.yalin.site/?follow=1&game=botw')
}

async function openLogDir() {
  const msg = await api.OpenLogDir()
  if (msg) notify(msg)
}

async function exportDiag() {
  const msg = await api.ExportDiagnostics()
  notify(msg || '诊断包已导出')
}

async function rescan() {
  const msg = await api.Rescan()
  notify(msg || '已提交重新扫描')
}

// ------------------------------------------------------------------- 生命周期

let offStatus = null
let offLogs = null

onMounted(async () => {
  try { ver.value = await api.Version() } catch (e) { /* 保持默认版本号 */ }

  try {
    const c = await api.LoadConfig()
    if (c && c.edenSaveDir) cfg.edenSaveDir = c.edenSaveDir
  } catch (e) { /* 配置读取失败时保持空值（自动探测） */ }

  try {
    const s = await api.CoreStatus()
    applyStatus(s)
    updateSteps()
  } catch (e) { /* 核心未启动时无状态 */ }

  try { pushLogs(await api.TailLog()) } catch (e) { /* 无历史日志 */ }

  offStatus = rt.EventsOn('status:update', (s) => {
    applyStatus(s)
    updateSteps()
  })
  offLogs = rt.EventsOn('log:append', (lines) => {
    pushLogs(Array.isArray(lines) ? lines : [lines])
  })
})

onUnmounted(() => {
  if (offStatus) offStatus()
  if (offLogs) offLogs()
  if (toastTimer) clearTimeout(toastTimer)
})
</script>
