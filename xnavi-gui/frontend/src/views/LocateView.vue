<template>
  <div class="flex-1 flex flex-col gap-3 min-h-0 p-3 overflow-hidden select-none">

    <!-- ① 自绘标题栏：整栏拖拽 + 设置/窗口控制 -->
    <section class="bg-[#161b22] border-b border-slate-800 px-3 py-2 shrink-0 flex items-center justify-between" style="--wails-draggable:drag;">
      <div class="flex items-center gap-2.5 shrink-0">
        <span class="w-2.5 h-2.5 rounded-full" :class="syncDot"></span>
        <span class="text-white text-sm font-bold tracking-wide">{{ t('title.main') }}</span>
        <span v-if="ver" class="text-[10px] font-mono text-sky-300 bg-blue-500/10 border border-blue-500/40 rounded px-1.5 py-0.5">v{{ ver.replace('v','') }}</span>
        <span class="text-[11px] text-slate-500 ml-1.5 pl-2.5 border-l border-slate-800">{{ emuShortLabel }} ➔ {{ headerMapHost }}</span>
      </div>
      <div class="flex items-center gap-1.5 shrink-0" style="--wails-draggable:no-drag;">
        <span class="text-[11px] font-mono mr-2" :class="syncClass">{{ syncText }}</span>
        <!-- 设置弹窗按钮 -->
        <div class="relative" @click.stop>
          <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 transition" :title="t('header.settings')" @click="toggleSettings">⚙</button>
          <SettingsPop v-if="showSettingsPop" class="absolute right-0 top-full mt-1.5 z-50" @close="showSettingsPop = false" />
        </div>
        <!-- 关于弹窗按钮 -->
        <div class="relative" @click.stop>
          <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 text-[14px] font-semibold transition" :title="t('header.about')" @click="toggleAbout">?</button>
          <AboutPop v-if="showAboutPop" class="absolute right-0 top-full mt-1.5 z-50" @close="showAboutPop = false" />
        </div>
        <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 text-xs leading-none transition" :title="t('header.minimize')" @click="minWin">—</button>
        <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-rose-400 rounded hover:bg-slate-700/60 text-xs leading-none transition" :title="t('header.close')" @click="closeWin">✕</button>
      </div>
    </section>

    <!-- ② 主操作横幅：步骤状态机 | 模拟器选择 + 打开地图 + 开始定位 -->
    <section class="bg-[#161b22] border border-slate-800 rounded-lg px-3.5 py-2.5 shrink-0 flex items-center justify-between gap-4">
      <div class="flex items-center gap-2 text-xs font-mono shrink-0 whitespace-nowrap">
        <template v-for="(s, i) in steps" :key="s.key">
          <div v-if="i > 0" class="text-slate-600 mx-0.5">/</div>
          <div class="flex items-center gap-1.5" :class="stepClass(s.state)">
            <span class="w-2 h-2 rounded-full" :class="stepDot(s.state)"></span>
            <span>{{ t(`steps.${s.key}`) }}</span>
          </div>
        </template>
      </div>
      <div class="flex items-center gap-2.5 shrink-0">
        <select v-model="cfg.emulator" @change="onEmuChange" :title="t('emu.pickTitle')"
          class="bg-[#0d1117] border border-slate-700 rounded-lg px-2.5 py-1.5 text-xs font-medium text-slate-300 hover:border-slate-500 focus:outline-none transition">
          <option value="auto">{{ t('emu.auto') }}</option>
          <option value="cemu">{{ t('emu.cemu') }}</option>
          <option value="ryujinx">{{ t('emu.ryujinx') }}</option>
          <option value="eden">{{ t('emu.eden') }}</option>
        </select>
        <button @click="openMap" class="w-[124px] py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-sky-400 text-xs font-medium border border-slate-700 flex items-center justify-center gap-1.5 transition">
          🧭 {{ t('buttons.openMap') }}
        </button>
        <button @click="toggleLocating"
          :class="isLocating
            ? 'bg-amber-600 hover:bg-amber-500 shadow-amber-950/50'
            : 'bg-blue-600 hover:bg-blue-500 shadow-blue-950/50'"
          class="w-[128px] py-1.5 rounded-lg text-white text-xs font-bold flex items-center justify-center gap-1.5 transition shadow-lg active:scale-95">
          <span>{{ isLocating ? '■' : '▶' }}</span>
          <span>{{ isLocating ? t('buttons.locating') : t('buttons.start') }}</span>
        </button>
      </div>
    </section>

    <!-- ③ 两列：左 4 列 实时数据 ｜ 右 6 列 本地解析进度与存档 -->
    <div class="grid grid-cols-10 gap-3 items-stretch shrink-0">

      <!-- 左 4 列 -->
      <div class="col-span-4 flex flex-col gap-3">

        <!-- 实时同步坐标卡 -->
        <section class="bg-[#161b22] border border-slate-800 rounded-xl p-3 flex-1">
          <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2">
            <span class="font-bold text-sky-400 flex items-center gap-1.5">{{ t('locate.liveCoords') }}</span>
            <span class="text-[10px] font-mono text-slate-400 truncate max-w-[170px]" :title="pos.lockAddr || '等待锁定'">
              {{ t('locate.offset') }}: <span class="text-sky-300 font-semibold">{{ pos.lockAddr || t('locate.waitingAddr') }}</span>
            </span>
          </div>
          <div class="grid grid-cols-3 gap-2 text-center font-mono">
            <div class="bg-[#0d1117] p-2 rounded-lg border border-slate-800">
              <div class="text-[10px] text-slate-500">X</div>
              <div class="text-sm font-bold text-white mt-0.5">{{ fmt(pos.gx) }}</div>
            </div>
            <div class="bg-[#0d1117] p-2 rounded-lg border border-slate-800">
              <div class="text-[10px] text-slate-500">Y</div>
              <div class="text-sm font-bold text-white mt-0.5">{{ fmt(pos.gy) }}</div>
            </div>
            <div class="bg-[#0d1117] p-2 rounded-lg border border-slate-800">
              <div class="text-[10px] text-slate-500">{{ t('locate.axisZ') }}</div>
              <div class="text-sm font-bold text-white mt-0.5">{{ fmt(pos.gz) }}</div>
            </div>
          </div>
          <div class="mt-2 flex items-center justify-between text-xs bg-[#0d1117] px-2.5 py-1.5 rounded-lg border border-slate-800">
            <span class="text-slate-400">{{ t('locate.currentLayer') }}:</span>
            <span class="font-bold text-emerald-400 font-mono">{{ regionText }}<span class="text-blue-300 text-[10px] ml-2">[{{ pos.source || '-' }}]</span></span>
          </div>
        </section>

        <!-- 坐标校准卡（BOTW 定向找人） -->
        <section class="bg-[#161b22] border border-slate-800 rounded-xl p-3 shrink-0">
          <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2">
            <span class="font-bold text-emerald-400 flex items-center gap-1.5">🧭 {{ t('locate.calibration') }}</span>
            <div class="flex items-center gap-2">
              <span class="text-[10px] text-slate-500">{{ t('locate.calWhenFail') }}</span>
              <button @click="doCalibrate" :disabled="calBusy"
                class="bg-emerald-600 hover:bg-emerald-500 text-white px-2 py-0.5 rounded text-[10px] font-bold transition flex items-center gap-1 active:scale-95 disabled:opacity-60">
                <span>↻</span>
                <span>{{ t('buttons.submitCalib') }}</span>
              </button>
            </div>
          </div>
          <div class="text-[10px] text-slate-500 mb-1.5 leading-relaxed">{{ t('locate.calHint') }}</div>
          <div class="flex items-center gap-2 mb-2">
            <label class="flex-1 bg-[#0d1117] rounded border border-slate-800 px-2 py-1 flex items-center gap-1.5">
              <span class="text-[10px] text-slate-500 shrink-0">X</span>
              <input v-model="calX" type="number" step="0.1" placeholder="-215.0"
                class="bg-transparent outline-none text-white font-mono text-xs w-full placeholder:text-slate-600" />
            </label>
            <label class="flex-1 bg-[#0d1117] rounded border border-slate-800 px-2 py-1 flex items-center gap-1.5">
              <span class="text-[10px] text-slate-500 shrink-0">Z</span>
              <input v-model="calZ" type="number" step="0.1" placeholder="250.0"
                class="bg-transparent outline-none text-white font-mono text-xs w-full placeholder:text-slate-600" />
            </label>
          </div>
          <div class="flex items-center gap-1.5 flex-wrap text-[10px]">
            <span class="text-slate-500">预设:</span>
            <button @click="fillPreset(-215, 250)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300 transition">平原之塔</button>
            <button @click="fillPreset(-1071, 1913)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300 transition">复苏神庙</button>
            <button @click="fillPreset(3435, 1968)" class="px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300 transition">哈特诺塔</button>
          </div>
        </section>
      </div>

      <!-- 右 6 列：本地解析进度与存档 -->
      <div class="col-span-6 flex flex-col min-h-0">
        <section class="bg-[#161b22] border border-slate-800 rounded-xl p-3 flex-1 flex flex-col justify-between">
          <div>
            <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2.5">
              <div class="flex items-center gap-2">
                <span class="font-bold text-emerald-400">{{ t('locate.localProgress') }}</span>
                <span v-if="saveSourceTag" class="text-[10px] px-1.5 py-0.2 rounded bg-slate-800 border border-slate-700 text-sky-400 font-mono">{{ saveSourceTag }}</span>
              </div>
              <span class="text-[11px] font-mono text-slate-400">{{ t('locate.playtime') }}: <span class="text-white font-bold">{{ playtimeText }}</span></span>
            </div>

            <!-- 18 项探索度（4 组，与网页口径一致；数据全部来自存档） -->
            <div class="space-y-1">
              <div v-for="g in STAT_GROUPS" :key="g.title">
                <div class="flex items-center gap-1.5 mb-0.5">
                  <span class="text-[10px] font-semibold text-slate-500">{{ t('locate.' + g.title) }}</span>
                  <span class="flex-1 h-px bg-slate-800"></span>
                </div>
                <div class="grid grid-cols-3 gap-1">
                  <div v-for="it in g.items" :key="it.k"
                       class="bg-[#0d1117] px-1.5 py-1 rounded border border-slate-800 flex items-center justify-between gap-1">
                    <span class="text-slate-400 text-[10px] truncate">{{ t('locate.' + it.t) }}</span>
                    <span class="font-mono text-white font-bold text-[10px] shrink-0">{{ countText(it.k) }}</span>
                  </div>
                </div>
              </div>
              <div class="text-[9px] text-slate-600 pt-0.5">{{ t('locate.saveOnlyHint') }}</div>
            </div>
          </div>

          <!-- 生效存档管理条 -->
          <div class="mt-2 pt-2 border-t border-slate-800/80 text-xs">
            <div class="flex items-center justify-between gap-2">
              <span class="text-[11px] text-slate-400 font-medium shrink-0">{{ t('locate.saveInfo') }}:</span>
              <span class="text-[10px] font-mono text-slate-300 truncate flex-1 text-right" :title="progressData.save || '未检测到存档'">
                {{ progressData.save || '未检测到存档（启动后自动探测）' }}
              </span>
            </div>
            <div class="flex items-center justify-between gap-2 mt-2 flex-wrap">
              <!-- 四个动作按钮 -->
              <div class="flex items-center gap-1.5">
                <button @click="pickSaveFile" class="bg-blue-600 hover:bg-blue-500 text-white px-2 py-1 rounded text-[11px] font-medium transition active:scale-95">
                  {{ t('buttons.chooseFile') }}
                </button>
                <button @click="pickSaveDir" class="bg-slate-700 hover:bg-slate-600 text-slate-200 px-2 py-1 rounded text-[11px] transition active:scale-95">
                  {{ t('buttons.chooseDir') }}
                </button>
                <button v-if="progressData.save" @click="doOpenSave" class="bg-slate-700 hover:bg-slate-600 text-slate-200 px-2 py-1 rounded text-[11px] transition active:scale-95">
                  {{ t('buttons.locateFile') }}
                </button>
                <button @click="doDetectSave" class="bg-amber-600/80 hover:bg-amber-600 text-white px-2 py-1 rounded text-[11px] transition active:scale-95">
                  {{ t('buttons.detectSave') }}
                </button>
              </div>

              <!-- 右侧：自动检测存档开关 + 最新存档相对时间提醒 -->
              <div class="flex items-center gap-2.5 ml-auto text-[11px]">
                <label class="flex items-center gap-1 cursor-pointer select-none text-slate-400 hover:text-slate-300">
                  <input v-model="cfg.autoDetectSave" @change="saveSettings" type="checkbox" class="accent-blue-600 w-3.5 h-3.5 rounded" />
                  <span>{{ t('locate.autoDetectSave') }}</span>
                </label>
                <div v-if="saveTimeAgoText" class="font-mono text-[11px]" :class="saveTimeAgoClass">
                  {{ t('locate.latestSaveTime') }}: {{ saveTimeAgoText }}
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>

    <!-- ④ 实时日志 -->
    <LogPanel />

  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LogPanel from '../components/LogPanel.vue'
import SettingsPop from '../components/SettingsPop.vue'
import AboutPop from '../components/AboutPop.vue'
import {
  pos, steps, stepClass, stepDot, isLocating, toggleLocating, progressData,
  openMap, buildMapUrl, cfg, saveSettings, ver, syncText, syncClass, syncDot,
  rt, manualCalibrate, openSave, pickSaveFile, pickSaveDir, detectSave,
  saveTimeAgoText, saveTimeAgoClass
} from '../composables/useCore'

const { t } = useI18n()

const showSettingsPop = ref(false)
const showAboutPop = ref(false)

function toggleSettings() {
  showSettingsPop.value = !showSettingsPop.value
  if (showSettingsPop.value) showAboutPop.value = false
}

function toggleAbout() {
  showAboutPop.value = !showAboutPop.value
  if (showAboutPop.value) showSettingsPop.value = false
}

function minWin() { rt.WindowMinimise() }
function closeWin() { rt.Quit() }

const emuShortLabel = computed(() => {
  const e = cfg.emulator || 'auto'
  return { auto: '自动探测', cemu: 'Cemu', ryujinx: 'Ryujinx', eden: 'Eden' }[e] || e
})

const headerMapHost = computed(() => {
  if (cfg.customMapURL && cfg.customMapURL.trim()) {
    try {
      const u = new URL(cfg.customMapURL.trim())
      return u.host || '自定义网页'
    } catch {
      return '本地网页'
    }
  }
  return 'botw.yalin.site'
})

function fmt(v) { return (!pos.ok || v == null) ? '-' : Number(v).toFixed(1) }

const regionText = computed(() => {
  if (!pos.ok) return '等待连接'
  if (pos.gz < -5000 || pos.gz > 5000) return '未知区域'
  return '海拉鲁世界'
})

const playtimeText = computed(() => {
  const secs = progressData.value?.playtime || 0
  if (!secs) return '-'
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60)
  return `${h}h${String(m).padStart(2,'0')}m`
})

const saveSourceTag = computed(() => progressData.value?.saveSource || '')

// 18 项探索度分组（与网页探索度面板口径一致）
// 探索 6 项来自核心既有 counts，其余 12 项来自 v3.2.1 新增的 stats
const STAT_GROUPS = [
  { title: 'grpExplore', items: [
    { k: 'shrine', t: 'shrine' }, { k: 'tower', t: 'tower' }, { k: 'beast', t: 'beast' },
    { k: 'korok', t: 'korok' }, { k: 'memory_photo', t: 'memoryPhoto' }, { k: 'memory_final', t: 'memoryFinal' }
  ] },
  { title: 'grpQuest', items: [
    { k: 'quest_main', t: 'questMain' }, { k: 'quest_side', t: 'questSide' },
    { k: 'quest_shrine', t: 'questShrine' }, { k: 'quest_dlc', t: 'questDlc' }
  ] },
  { title: 'grpCollect', items: [
    { k: 'chest', t: 'chest' }, { k: 'weapon', t: 'weapon' }, { k: 'pb_equip', t: 'pbEquip' },
    { k: 'pb_mat', t: 'pbMat' }, { k: 'pb_enemy', t: 'pbEnemy' }
  ] },
  { title: 'grpSubjugate', items: [
    { k: 'boss_hinox', t: 'bossHinox' }, { k: 'boss_talus', t: 'bossTalus' }, { k: 'boss_molduga', t: 'bossMolduga' }
  ] }
]

function countText(k) {
  const c = progressData.value?.stats?.[k] || progressData.value?.counts?.[k]
  if (!c) return '-'
  return `${c[0]}/${c[1]}`
}

const calX = ref('')
const calZ = ref('')
const calBusy = ref(false)

function fillPreset(x, z) {
  calX.value = String(x)
  calZ.value = String(z)
}

async function doCalibrate() {
  if (!calX.value || !calZ.value) return
  calBusy.value = true
  try {
    const msg = await manualCalibrate(calX.value, calZ.value)
    alert(msg || '已提交校准请求')
  } finally {
    calBusy.value = false
  }
}

async function doOpenSave() {
  const msg = await openSave()
  if (msg && !msg.includes('已在资源管理器')) alert(msg)
}

async function doDetectSave() {
  const msg = await detectSave()
  if (msg) alert(msg)
}

async function onEmuChange() {
  await saveSettings()
}
</script>
