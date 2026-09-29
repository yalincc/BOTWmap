<template>
  <div class="flex flex-col h-screen w-full bg-[#0d1117] text-slate-200 select-none overflow-hidden font-sans">
    <!-- ===== 标题栏（拖拽区） ===== -->
    <header
      class="flex items-center justify-between px-4 py-2 bg-[#161b22] border-b border-slate-800 shrink-0"
      style="--wails-draggable:drag"
    >
      <div class="flex items-center space-x-2 text-sm font-semibold tracking-wide">
        <span class="w-2.5 h-2.5 rounded-full bg-blue-500 shadow-sm shadow-blue-500/50"></span>
        <span class="text-white">xnavi - BOTW 定位导航</span>
        <span class="text-xs text-slate-400 font-mono">{{ ver }}</span>
      </div>
      <div class="flex items-center space-x-4 text-xs" style="--wails-draggable:no-drag">
        <!-- 坐标流同步状态 -->
        <div class="flex items-center space-x-1.5 font-mono" :class="syncClass">
          <span class="w-2 h-2 rounded-full inline-block" :class="syncDot"></span>
          <span>{{ syncText }}</span>
        </div>
        <button class="text-slate-400 hover:text-white transition" @click="openSettings">⚙</button>
        <div class="flex items-center space-x-2 pl-2 border-l border-slate-700">
          <button class="text-slate-400 hover:text-white px-1" @click="minimizeWindow">一</button>
          <button class="text-slate-400 hover:text-red-400 px-1" @click="closeWindow">✕</button>
        </div>
      </div>
    </header>

    <!-- ===== 主体 ===== -->
    <main class="flex-1 p-4 space-y-3 overflow-y-auto">
      <!-- 行A：环境配置 | 主控 | 档案 -->
      <div class="grid grid-cols-12 gap-3">
        <!-- 运行环境配置 -->
        <section class="col-span-12 md:col-span-5 bg-[#161b22] border border-slate-800 rounded-md p-3">
          <div class="text-[11px] font-semibold text-slate-400 mb-2 uppercase tracking-wider">运行环境配置</div>
          <div class="space-y-2">
            <div class="grid grid-cols-2 gap-2">
              <div>
                <label class="block text-xs text-slate-400 mb-1">模拟器：</label>
                <select
                  v-model="env.emulator"
                  class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                >
                  <option value="auto">Auto (Cemu/Ryujinx)</option>
                  <option value="cemu">Cemu</option>
                  <option value="ryujinx">Ryujinx</option>
                </select>
              </div>
              <div>
                <label class="block text-xs text-slate-400 mb-1">游戏版本：</label>
                <select
                  v-model="env.version"
                  class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                >
                  <option value="auto">自动检测（1.x）</option>
                  <option value="1.6.0_us">1.6.0 (美版)</option>
                  <option value="1.6.0_jp">1.6.0 (日版)</option>
                  <option value="1.5.0">1.5.0 (全区)</option>
                </select>
              </div>
            </div>
            <div>
              <label class="block text-xs text-slate-400 mb-1">区域 / MOD：</label>
              <select
                v-model="env.mod"
                class="w-full bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
              >
                <option value="none">原版 (无MOD)</option>
                <option value="rebalance">大师模式扩展 MOD</option>
              </select>
            </div>
            <div class="text-[11px] font-mono" :class="hitClass">{{ hitText }}</div>
          </div>
        </section>

        <!-- 主控：开始定位 / 停止 -->
        <section class="col-span-12 md:col-span-2 flex flex-col justify-between gap-2">
          <button
            @click="toggleLocating"
            :disabled="!coreRunning && !emulatorFound"
            :class="isLocating ? 'bg-amber-600 hover:bg-amber-500' : 'bg-blue-600 hover:bg-blue-500 disabled:bg-slate-800 disabled:text-slate-600'"
            class="flex-1 rounded flex items-center justify-center font-bold text-sm text-white shadow-lg transition active:scale-95 min-h-[48px]"
            :title="!emulatorFound && !coreRunning ? '未检测到模拟器，请先启动 Cemu 或 Ryujinx' : ''"
          >{{ isLocating ? '■ 停止' : '开始定位' }}</button>
          <button
            @click="stopCore"
            class="flex-1 rounded flex items-center justify-center font-bold text-sm text-slate-300 bg-slate-700 hover:bg-slate-600 active:scale-95 min-h-[48px]"
          >停止</button>
        </section>

        <!-- 快路径推荐档案 -->
        <section class="col-span-12 md:col-span-5 bg-gradient-to-r from-emerald-950/30 to-[#161b22] border border-emerald-500/30 rounded-md p-3 flex flex-col justify-between">
          <div>
            <div class="text-[11px] text-emerald-400 font-medium">快路径推荐档案</div>
            <div class="text-sm font-bold text-white mt-1">{{ profileText }}</div>
            <div class="text-[11px] text-slate-400 mt-1 font-mono">{{ profileNote }}</div>
          </div>
          <div class="flex items-center justify-between text-[11px] font-mono">
            <span class="text-slate-400">{{ profileMeta }}</span>
            <button
              v-if="verified"
              class="text-emerald-400 hover:underline"
              @click="saveProfile"
            >保存当前档案</button>
          </div>
        </section>
      </div>

      <!-- 实时状态看板 -->
      <section class="bg-[#161b22] border border-slate-800 rounded-md p-3">
        <div class="text-[11px] font-semibold text-slate-400 mb-3 uppercase tracking-wider">实时状态看板</div>
        <div class="flex items-center justify-between gap-4">
          <!-- 流程节点 -->
          <div class="flex items-center space-x-2 flex-1">
            <template v-for="(s, i) in steps" :key="s.name">
              <div class="flex items-center space-x-2">
                <div class="flex items-center space-x-1.5" :class="stepClass(s.state)">
                  <span class="w-2 h-2 rounded-full" :class="stepDot(s.state)"></span>
                  <span class="text-xs font-mono">{{ s.name }}</span>
                </div>
              </div>
              <div v-if="i < steps.length - 1" class="w-6 h-px bg-slate-700"></div>
            </template>
          </div>
          <!-- 进度大数字 -->
          <div v-if="scanPercent > 0" class="text-2xl font-bold font-mono" :class="percentClass">{{ scanPercent }}%</div>
        </div>
        <div class="flex items-center justify-between text-xs mt-3">
          <div class="font-mono text-slate-300">
            <span :class="stateTextClass">{{ stateText }}</span>
            <span v-if="scanInfo" class="text-slate-400 ml-2">{{ scanInfo }}</span>
          </div>
          <div class="text-[11px] text-slate-400 font-mono">{{ stateDetail }}</div>
        </div>
        <div class="w-full bg-[#0d1117] h-1.5 rounded-full overflow-hidden mt-2">
          <div
            :class="progressClass"
            class="h-full transition-all duration-300 rounded-full"
            :style="{ width: scanPercent + '%' }"
          ></div>
        </div>
      </section>

      <!-- 日志面板 -->
      <section class="bg-[#0d1117] border border-slate-800 rounded-md p-2.5 flex flex-col font-mono text-[11px]">
        <div class="flex items-center justify-between pb-1.5 mb-1.5 border-b border-slate-800/80 text-slate-400 text-[10px]">
          <span>实时输出 (落盘：run.log)</span>
          <div class="space-x-3">
            <button class="hover:text-slate-200" @click="clearLogs">清屏</button>
            <button class="hover:text-slate-200" @click="openLogDir">打开日志目录</button>
          </div>
        </div>
        <div ref="logBox" class="h-32 overflow-y-auto space-y-0.5 leading-relaxed text-slate-400">
          <div v-for="(log, idx) in logs" :key="idx" :class="log.color">
            <span class="text-slate-500">[{{ log.time }}]</span>
            <span class="mr-1" :class="log.levelColor">{{ log.level }}</span>{{ log.text }}
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'

const api = window.go.main.App
const rt = window.runtime

// ---- 标题栏 ----
const ver = ref('v2.0.0')
const coreRunning = ref(false)
const verified = ref(false)
const syncText = ref('等待坐标流')
const syncDot = ref('bg-slate-600')
const syncClass = computed(() => coreRunning.value ? 'text-emerald-400' : 'text-slate-500')

function minimizeWindow() { rt.WindowMinimise() }
function closeWindow() { rt.WindowClose() }
function openSettings() { } // 阶段 C

// ---- 环境 ----
const env = reactive({ emulator: 'auto', version: 'auto', mod: 'none' })
const envDetect = reactive({ cemu: false, ryujinx: false })
const hitText = computed(() => {
  if (envDetect.cemu && envDetect.ryujinx) return '✓ H:/Cemu + G:/YUZU/...'
  if (envDetect.cemu) return '✓ H:/Cemu'
  if (envDetect.ryujinx) return '✓ G:/YUZU/...'
  return '未检测到模拟器，请手动选择'
})
const hitClass = computed(() => (envDetect.cemu || envDetect.ryujinx) ? 'text-emerald-400' : 'text-amber-400')
const emulatorFound = computed(() => envDetect.cemu || envDetect.ryujinx)

// ---- 定位控制 ----
const isLocating = ref(false)
async function toggleLocating() {
  if (isLocating.value) { await api.StopCore(); isLocating.value = false }
  else { const r = await api.StartCore(env.emulator); isLocating.value = true }
}
async function stopCore() { const r = await api.StopCore(); isLocating.value = false }

// ---- 状态 ----
const status = reactive({ ok: false, gx: 0, gy: 0, gz: 0, mx: 0, my: 0, verified: false, source: '' })
const stateText = ref('就绪')
const stateDetail = ref('')
const scanInfo = ref('')
const scanPercent = ref(0)
const percentClass = computed(() => stateText.value.includes('已验证') ? 'text-emerald-400' : 'text-amber-400')
const progressClass = computed(() =>
  (stateText.value.includes('重扫') || stateText.value.includes('核对'))
    ? 'bg-amber-500 animate-pulse'
    : (stateText.value.includes('已验证') ? 'bg-emerald-500' : 'bg-amber-500'))
const stateTextClass = computed(() => {
  const t = stateText.value
  if (t.includes('已验证')) return 'text-emerald-400 font-semibold'
  if (t.includes('已锁定') || t.includes('扫描') || t.includes('重扫') || t.includes('快路径') || t.includes('核对')) return 'text-amber-400 font-semibold'
  if (t.includes('就绪')) return 'text-slate-500'
  return 'text-slate-300'
})

// ---- 步骤指示器（就绪→扫描中→验证移动→已锁定） ----
const steps = ref([
  { name: '就绪', state: 'done' },
  { name: '扫描中', state: 'idle' },
  { name: '验证移动', state: 'idle' },
  { name: '已锁定', state: 'idle' }
])
function stepClass(s) {
  return { done: 'text-emerald-400', active: 'text-amber-400 font-semibold', idle: 'text-slate-600' }[s]
}
function stepDot(s) {
  return { done: 'bg-emerald-400', active: 'bg-amber-400 animate-pulse', idle: 'bg-slate-700' }[s]
}
function setStep(doneCount, activeName) {
  const names = ['就绪', '扫描中', '验证移动', '已锁定']
  steps.value.forEach(s => { s.state = 'idle' })
  names.slice(0, doneCount).forEach(n => { const st = steps.value.find(x => x.name === n); if (st) st.state = 'done' })
  const act = steps.value.find(x => x.name === activeName)
  if (act) act.state = 'active'
}

// ---- 档案 ----
const profileText = ref('本地自动档案')
const profileNote = ref('上次验证：— 移动确认未通过')
const profileMeta = ref('启动后自动匹配档案，快路径秒定位')
function saveProfile() { profileMeta.value = '档案已保存（阶段 C 落盘）' }

// ---- 日志 ----
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
    else if (/confirmed live|verified/i.test(text)) { level = 'ok'; color = 'text-emerald-400'; levelColor = 'text-emerald-500' }
    else if (/\[sm\]/i.test(text)) { level = 'info'; color = 'text-sky-400' }
    text = text.replace(/^\[sm\]\s*/, '')
    return { time, text, level, color, levelColor }
  })
  logs.value = logs.value.concat(items)
  if (logs.value.length > 500) logs.value = logs.value.slice(logs.value.length - 500)
  nextTick(() => { if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight })
  const last = items[items.length - 1].text
  parseState(last)
}
function clearLogs() { logs.value = [] }
async function openLogDir() { await api.OpenLogDir() }

// ---- 状态解析 ----
function parseState(line) {
  const s = line
  if (/HTTP 端口被占用/.test(s)) { stateText.value = '网页端不可用'; stateDetail.value = '定位照常（双入口独立）' }
  else if (/confirmed live/.test(s)) {
    stateText.value = '已验证 · 跟随中'; stateDetail.value = '移动确认通过'
    setStep(3, '已锁定'); scanPercent.value = 100
  }
  else if (/probe -> switched/.test(s)) { stateText.value = '已锁定 · 探针换活组'; setStep(2, '验证移动') }
  else if (/scan -> lock/.test(s)) {
    stateText.value = '已锁定 · 待移动确认'; stateDetail.value = '等待玩家移动'
    setStep(2, '验证移动')
  }
  else if (/pass \d+ \(window/.test(s)) {
    stateText.value = '扫描中 (约 15s)'; setStep(1, '扫描中')
    const m = s.match(/([\d,]+) triple hits/)
    if (m) { scanInfo.value = '窗口 ±400 · ' + m[1] + ' 条命中'; scanPercent.value = Math.min(85, scanPercent.value + 20) }
  }
  else if (/structural scan/.test(s)) { stateText.value = '扫描中 (约 15s)'; setStep(1, '扫描中') }
  else if (/teleport jump/.test(s)) { stateText.value = '传送跳变接受'; stateDetail.value = '零扫描跟随' }
  else if (/UNLOCKED/.test(s)) {
    stateText.value = '未锁定 · 自动重扫'; stateDetail.value = reasonOf(s)
    setStep(0, '就绪'); scanPercent.value = 0
  }
  else if (/verify: idle 30s/.test(s)) { stateText.value = '站桩核对中'; stateDetail.value = 'keep lock' }
  else if (/save anchor/.test(s)) { stateText.value = '已识别环境'; stateDetail.value = '存档锚点就绪'; setStep(0, '就绪') }
}
function reasonOf(s) {
  if (/readings invalid/.test(s)) return '重扫原因：读数失效（场景切换）'
  if (/重启/.test(s)) return '重扫原因：游戏重启'
  if (/传送|跳变/.test(s)) return '重扫原因：传送'
  if (/站桩|核对/.test(s)) return '重扫原因：站桩核对不符'
  return ''
}

// ---- 状态事件 ----
function onStatus(st) {
  coreRunning.value = !!st.running
  if (st.ok !== undefined) {
    status.ok = st.ok; status.gx = st.gx; status.gy = st.gy; status.gz = st.gz
    status.mx = st.mx; status.my = st.my
    status.verified = st.verified; status.source = st.source
  }
  if (coreRunning.value && st.verified) {
    syncText.value = '坐标流同步中 (60 fps)'; syncDot.value = 'bg-emerald-400 animate-pulse'
    profileText.value = '本地 Cemu 1.6.0 美版'; profileNote.value = '上次验证：2026-09-29 移动确认通过'
    if (st.source) profileMeta.value = '来源：' + st.source + ' · 快路径命中'
  } else if (coreRunning.value) {
    syncText.value = '定位中，等待坐标流'; syncDot.value = 'bg-amber-400 animate-pulse'
  } else {
    syncText.value = '等待坐标流'; syncDot.value = 'bg-slate-600'
  }
  if (st.webNote && st.webNote.length) stateDetail.value = st.webNote
}

// ---- 生命周期 ----
let offStatus, offLogs
onMounted(async () => {
  ver.value = await api.Version()
  const det = await api.EnvDetect()
  envDetect.cemu = det.cemu
  envDetect.ryujinx = det.ryujinx
  const initial = await api.TailLog()
  pushLogs(initial)
  offStatus = rt.EventsOn('status:update', onStatus)
  offLogs = rt.EventsOn('log:append', pushLogs)
})
onUnmounted(() => { if (offStatus) offStatus(); if (offLogs) offLogs() })
</script>
