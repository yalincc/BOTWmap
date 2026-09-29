<template>
  <div class="flex flex-col h-screen w-full bg-[#0d1117] text-slate-200 select-none overflow-hidden font-sans">
    <header class="flex items-center justify-between px-4 py-2 bg-[#161b22] border-b border-slate-800 shrink-0">
      <div class="flex items-center space-x-2 text-sm font-semibold tracking-wide">
        <span class="w-2.5 h-2.5 rounded-full bg-blue-500 shadow-sm shadow-blue-500/50"></span>
        <span class="text-white">xnavi - BOTW 定位导航</span>
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
      <div class="grid grid-cols-12 gap-3 shrink-0">
        <section class="col-span-12 md:col-span-5 bg-[#161b22] border border-slate-800 rounded-md p-3">
          <div class="text-[11px] font-semibold text-slate-400 mb-2 uppercase tracking-wider">运行环境配置</div>
          <div class="grid grid-cols-2 gap-2">
            <div>
              <label class="block text-xs text-slate-400 mb-1">模拟器：</label>
              <select v-model="env.emulator" class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none">
                <option value="auto">Auto (Cemu/Ryujinx)</option>
                <option value="cemu">Cemu</option>
                <option value="ryujinx">Ryujinx</option>
              </select>
            </div>
            <div>
              <label class="block text-xs text-slate-400 mb-1">游戏版本：</label>
              <select v-model="env.version" class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none">
                <option value="auto">自动检测</option>
                <option value="1.6.0_us">1.6.0 (美版)</option>
                <option value="1.6.0_jp">1.6.0 (日版)</option>
              </select>
            </div>
          </div>
          <div class="text-[11px] font-mono mt-2" :class="hitClass">{{ hitText }}</div>
        </section>
        <section class="col-span-12 md:col-span-2 flex">
          <button @click="toggleLocating"
            :disabled="!coreRunning && !emulatorFound"
            :class="isLocating ? 'bg-amber-600 hover:bg-amber-500' : 'bg-blue-600 hover:bg-blue-500 disabled:bg-slate-800 disabled:text-slate-600'"
            class="w-full rounded flex items-center justify-center font-bold text-sm text-white shadow-lg transition active:scale-95">
            {{ isLocating ? '■ 停止' : '▶ 开始定位' }}
          </button>
        </section>
        <section class="col-span-12 md:col-span-5 bg-gradient-to-r from-emerald-950/30 to-[#161b22] border border-emerald-500/30 rounded-md p-3 flex flex-col justify-center">
          <div class="text-[11px] text-emerald-400 font-medium">快路径档案</div>
          <div class="text-sm font-bold text-white mt-0.5">{{ profileText }}</div>
          <div class="text-[11px] text-slate-400 mt-1 font-mono">{{ profileNote }}</div>
        </section>
      </div>
      <section class="bg-[#161b22] border border-slate-800 rounded-md p-3 shrink-0">
        <div class="flex items-center justify-between gap-4">
          <div class="flex items-center space-x-2 flex-1">
            <template v-for="(s, i) in steps" :key="s.name">
              <div class="flex items-center space-x-1.5" :class="stepClass(s.state)">
                <span class="w-2 h-2 rounded-full" :class="stepDot(s.state)"></span>
                <span class="text-xs font-mono">{{ s.name }}</span>
              </div>
              <div v-if="i < steps.length - 1" class="w-6 h-px bg-slate-700"></div>
            </template>
          </div>
          <div v-if="scanPercent > 0" class="text-xl font-bold font-mono" :class="percentClass">{{ scanPercent }}%</div>
        </div>
        <div class="text-xs mt-2">
          <span :class="stateTextClass">{{ stateText }}</span>
          <span v-if="scanInfo" class="text-slate-400 ml-2">{{ scanInfo }}</span>
        </div>
      </section>
      <section class="bg-[#0d1117] border border-slate-800 rounded-md p-2.5 flex flex-col font-mono text-[11px] flex-1 min-h-0">
        <div class="flex items-center justify-between pb-1.5 mb-1.5 border-b border-slate-800/80 text-slate-400 text-[10px] shrink-0">
          <span>实时日志</span>
          <div class="space-x-3">
            <button class="hover:text-slate-200" @click="clearLogs">清屏</button>
            <button class="hover:text-slate-200" @click="openLogDir">打开目录</button>
          </div>
        </div>
        <div ref="logBox" class="flex-1 overflow-y-auto space-y-0.5 leading-relaxed text-slate-400 min-h-0">
          <div v-for="(log, idx) in logs" :key="idx" :class="log.color">
            <span class="text-slate-500">[{{ log.time }}]</span>
            <span class="mr-1" :class="log.levelColor">{{ log.level }}</span>{{ log.text }}
          </div>
        </div>
      </section>
    </main>
    <div v-if="showSettings" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="showSettings = false">
      <div class="bg-[#161b22] border border-slate-700 rounded-lg p-5 w-96">
        <div class="text-sm font-bold text-white mb-4">设置</div>
        <div class="space-y-3 text-xs">
          <div>
            <label class="block text-slate-400 mb-1">Cemu 路径：</label>
            <div class="flex gap-1">
              <input v-model="paths.cemu" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white focus:border-blue-500 focus:outline-none" />
              <button @click="pickCemu" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">浏览</button>
            </div>
          </div>
          <div>
            <label class="block text-slate-400 mb-1">Ryujinx 路径：</label>
            <div class="flex gap-1">
              <input v-model="paths.ryujinx" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white focus:border-blue-500 focus:outline-none" />
              <button @click="pickRyujinx" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">浏览</button>
            </div>
          </div>
          <div>
            <label class="block text-slate-400 mb-1">存档路径（留空自动探测）：</label>
            <div class="flex gap-1">
              <input v-model="paths.saveDir" placeholder="自动探测" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white focus:border-blue-500 focus:outline-none" />
              <button @click="pickSave" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">浏览</button>
            </div>
          </div>
        </div>
        <div class="flex justify-end mt-4">
          <button class="bg-blue-600 hover:bg-blue-500 px-4 py-1.5 rounded text-xs text-white" @click="saveSettings">保存</button>
        </div>
      </div>
    </div>
    <div v-if="updateInfo" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="updateInfo = null">
      <div class="bg-[#161b22] border border-emerald-500/50 rounded-lg p-5 w-96">
        <div class="text-sm font-bold text-emerald-400 mb-2">有新版本可用</div>
        <div class="text-xs text-slate-300">当前：{{ ver }} → 最新：{{ updateInfo.latest }}</div>
        <div class="flex justify-end gap-2 mt-4">
          <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs" @click="updateInfo = null">稍后</button>
          <button class="bg-emerald-600 hover:bg-emerald-500 px-3 py-1.5 rounded text-xs text-white" @click="rt.BrowserOpenURL(updateInfo.url); updateInfo = null">下载</button>
        </div>
      </div>
    </div>
    <div v-if="showAbout" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="showAbout = false">
      <div class="bg-[#161b22] border border-slate-700 rounded-lg p-5 w-96 text-xs space-y-2">
        <div class="text-sm font-bold text-white mb-2">关于 xnavi</div>
        <div class="text-slate-300">BOTW 通用定位导航（Cemu + Ryujinx）</div>
        <div class="text-slate-400">版本：{{ ver }}</div>
        <div class="text-slate-400">地图：<a href="https://botw.yalin.site/" class="text-blue-400 hover:underline">botw.yalin.site</a></div>
        <div class="text-slate-400">GitHub：<a href="https://github.com/yalincc/BOTWmap" class="text-blue-400 hover:underline">yalincc/BOTWmap</a></div>
        <div class="text-slate-400 mt-2">只读内存，不注入，不修改游戏。</div>
        <div class="flex justify-end gap-2 mt-3">
          <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs" @click="checkUpdate">检查更新</button>
          <button class="bg-slate-700 hover:bg-slate-600 px-4 py-1.5 rounded text-xs text-white" @click="showAbout = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>
<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
const api = window.go.main.App
const rt = window.runtime
const ver = ref('v2.0.0')
const coreRunning = ref(false)
const syncText = ref('等待坐标流')
const syncDot = ref('bg-slate-600')
const syncClass = computed(() => coreRunning.value ? 'text-emerald-400' : 'text-slate-500')
const showSettings = ref(false)
const showAbout = ref(false)
const updateInfo = ref(null)
let isInitLog = true
const paths = reactive({ cemu: 'H:\\Cemu', ryujinx: 'G:\\YUZU\\ryujinx-canary-1.3.351-win_x64', saveDir: '' })
const env = reactive({ emulator: 'auto', version: 'auto' })
const envDetect = reactive({ cemu: false, ryujinx: false })
const hitText = computed(() => {
  if (envDetect.cemu && envDetect.ryujinx) return '✓ H:/Cemu + Ryujinx'
  if (envDetect.cemu) return '✓ H:/Cemu'
  if (envDetect.ryujinx) return '✓ Ryujinx'
  return '未检测到模拟器'
})
const hitClass = computed(() => (envDetect.cemu || envDetect.ryujinx) ? 'text-emerald-400' : 'text-amber-400')
const emulatorFound = computed(() => envDetect.cemu || envDetect.ryujinx)
const isLocating = ref(false)
async function toggleLocating() {
  if (isLocating.value) { await api.StopCore(); isLocating.value = false }
  else { await api.StartCore(env.emulator); isLocating.value = true }
}
const stateText = ref('就绪')
const scanInfo = ref('')
const scanPercent = ref(0)
const percentClass = computed(() => stateText.value.includes('已验证') ? 'text-emerald-400' : 'text-amber-400')
const stateTextClass = computed(() => {
  const t = stateText.value
  if (t.includes('已验证')) return 'text-emerald-400 font-semibold'
  if (t.includes('扫描') || t.includes('重扫')) return 'text-amber-400 font-semibold'
  return 'text-slate-300'
})
const steps = ref([
  { name: '就绪', state: 'done' },
  { name: '扫描中', state: 'idle' },
  { name: '验证移动', state: 'idle' },
  { name: '已锁定', state: 'idle' }
])
function stepClass(s) { return { done: 'text-emerald-400', active: 'text-amber-400 font-semibold', idle: 'text-slate-600' }[s] }
function stepDot(s) { return { done: 'bg-emerald-400', active: 'bg-amber-400 animate-pulse', idle: 'bg-slate-700' }[s] }
function setStep(doneCount, activeName) {
  const names = ['就绪', '扫描中', '验证移动', '已锁定']
  steps.value.forEach(s => { s.state = 'idle' })
  names.slice(0, doneCount).forEach(n => { const st = steps.value.find(x => x.name === n); if (st) st.state = 'done' })
  const act = steps.value.find(x => x.name === activeName)
  if (act) act.state = 'active'
}
const profileText = ref('本地自动档案')
const profileNote = ref('等待定位...')
const logs = ref([])
const logBox = ref(null)
function pushLogs(lines) {
  if (!lines || !lines.length) return
  const items = lines.map(l => {
    const m = l.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) ?(.*)$/)
    const time = m ? m[1].slice(11) : '--:--:--'
    let text = m ? m[2] : l
    let level = 'info', color = 'text-slate-400', levelColor = 'text-sky-500'
    if (/ERROR|FAILED|panic/i.test(text)) { level = 'error'; color = 'text-red-400'; levelColor = 'text-red-500' }
    else if (/!!|占用/i.test(text)) { level = 'warn'; color = 'text-amber-400'; levelColor = 'text-amber-500' }
    else if (/confirmed live|confirmed\]/.test(text)) { level = 'ok'; color = 'text-emerald-400'; levelColor = 'text-emerald-500' }
    else if (/\[sm\]/i.test(text)) { level = 'info'; color = 'text-sky-400' }
    text = text.replace(/^\[sm\]\s*/, '')
    return { time, text, level, color, levelColor }
  })
  logs.value = logs.value.concat(items)
  if (logs.value.length > 2000) logs.value = logs.value.slice(logs.value.length - 2000)
  nextTick(() => { if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight })
  // 遍历所有新行找状态变化（不只是最后一行）
  if (!isInitLog) {
    for (const it of items) { parseState(it.text) }
  }
}
async function pickCemu() { const d = await api.PickDir("选择 Cemu 目录"); if (d) paths.cemu = d }
async function pickRyujinx() { const d = await api.PickDir("选择 Ryujinx 目录"); if (d) paths.ryujinx = d }
async function pickSave() { const d = await api.PickDir("选择存档目录"); if (d) paths.saveDir = d }
async function saveSettings() { await api.SaveConfig({ cemuDir: paths.cemu, ryujinxDir: paths.ryujinx, saveDir: paths.saveDir, emulator: env.emulator }); showSettings.value = false }
function clearLogs() { logs.value = [] }
function checkUpdate() {
  rt.EventsOn("update:latest", () => { alert("已经是最新版") });
  rt.EventsEmit("update:check");
}
async function openMap() { rt.BrowserOpenURL("https://botw.yalin.site/") }
async function openLogDir() { await api.OpenLogDir() }
function parseState(line) {
  const s = line
  if (/fixed offset.*confirmed/.test(s)) {
    stateText.value = '已验证 · 跟随中'
    setStep(3, '已锁定'); scanPercent.value = 100
    profileText.value = '本地固定偏移档案'
    profileNote.value = '快路径秒锁'
  }
  else if (/confirmed live/.test(s)) {
    stateText.value = '已验证 · 跟随中'; setStep(3, '已锁定'); scanPercent.value = 100
  }
  else if (/probe -> switched/.test(s)) { stateText.value = '已锁定 · 探针换活组'; setStep(2, '验证移动') }
  else if (/scan -> lock/.test(s)) { stateText.value = '已锁定 · 待移动确认'; setStep(2, '验证移动') }
  else if (/structural scan|trying fixed/.test(s)) { stateText.value = '定位中...'; setStep(1, '扫描中'); scanPercent.value = 30 }
  else if (/UNLOCKED/.test(s)) { stateText.value = '未锁定'; setStep(0, '就绪'); scanPercent.value = 0 }
}
let offLogs
onMounted(async () => {
  logs.value = []
  stateText.value = '就绪'
  scanPercent.value = 0
  profileText.value = '本地自动档案'
  profileNote.value = '等待定位...'
  steps.value.forEach(s => s.state = (s.name === '就绪' ? 'done' : 'idle'))
  ver.value = await api.Version()
  const cfg = await api.LoadConfig()
  if (cfg.cemuDir) paths.cemu = cfg.cemuDir
  if (cfg.ryujinxDir) paths.ryujinx = cfg.ryujinxDir
  if (cfg.saveDir) paths.saveDir = cfg.saveDir
  if (cfg.emulator) env.emulator = cfg.emulator
  const det = await api.EnvDetect()
  envDetect.cemu = det.cemu
  envDetect.ryujinx = det.ryujinx
  const initial = await api.TailLog()
  pushLogs(initial)
  isInitLog = false
  offLogs = rt.EventsOn('log:append', pushLogs)
  rt.EventsOn('update:available', (info) => { updateInfo.value = info })
  rt.EventsOn('status:update', (st) => {
    coreRunning.value = st.running
    if (st.running && st.ok) {
      syncText.value = '坐标流同步中'
      syncDot.value = 'bg-emerald-400'
    } else if (st.running) {
      syncText.value = '定位中...'
      syncDot.value = 'bg-amber-400 animate-pulse'
    } else {
      syncText.value = '等待坐标流'
      syncDot.value = 'bg-slate-600'
    }
  })
})
onUnmounted(() => { if (offLogs) offLogs() })
</script>
