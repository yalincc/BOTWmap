<template>
  <div class="slate-map flex-1 flex flex-col gap-3 min-h-0 p-3 overflow-y-auto select-none">

    <!-- ① 自绘标题栏：整栏拖拽 + 设置/窗口控制 -->
    <section class="slate-mast px-3 py-2 shrink-0 flex items-center justify-between" style="--wails-draggable:drag;">
      <div class="flex items-center gap-2.5 shrink-0">
        <span class="sync-dot w-2.5 h-2.5 rounded-full" :class="[syncDot, { acquired: coreRunning && pos.ok && pos.verified }]"></span>
        <span class="mast-title">{{ t('title.main') }}</span>
        <span v-if="ver" class="text-[10px] font-mono text-sky-300 bg-blue-500/10 border border-blue-500/40 rounded px-1.5 py-0.5">v{{ ver.replace('v','') }}</span>
        <span class="text-[11px] text-slate-500 ml-1.5 pl-2.5 border-l border-slate-800">{{ emuShortLabel }} ➔ {{ headerMapHost }}</span>
      </div>
      <div class="flex items-center gap-1.5 shrink-0" style="--wails-draggable:no-drag;">
        <span class="ui-caption mr-2" :class="syncClass">{{ syncText }}</span>
        <!-- 设置弹窗按钮 -->
        <div class="relative" @click.stop>
          <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 transition" :title="t('header.settings')" @click="toggleSettings">⚙</button>
          <Transition name="slate-pop">
            <SettingsPop v-if="showSettingsPop" class="absolute right-0 top-full mt-1.5 z-50" @close="showSettingsPop = false" />
          </Transition>
        </div>
        <!-- 关于弹窗按钮 -->
        <div class="relative" @click.stop>
          <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 text-[14px] font-semibold transition" :title="t('header.about')" @click="toggleAbout">?</button>
          <Transition name="slate-pop">
            <AboutPop v-if="showAboutPop" class="absolute right-0 top-full mt-1.5 z-50" @close="showAboutPop = false" />
          </Transition>
        </div>
        <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-white rounded hover:bg-slate-700/60 text-xs leading-none transition" :title="t('header.minimize')" @click="minWin">—</button>
        <button class="w-7 h-7 flex items-center justify-center text-slate-400 hover:text-rose-400 rounded hover:bg-slate-700/60 text-xs leading-none transition" :title="t('header.close')" @click="closeWin">✕</button>
      </div>
    </section>

    <!-- ② 主操作横幅：步骤状态机 | 模拟器选择 + 打开地图 + 开始定位 -->
    <section class="slate-panel px-3.5 py-2.5 shrink-0 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <n-steps size="small" class="min-w-0 basis-full min-[1160px]:flex-1">
        <n-step v-for="s in steps" :key="s.key" :status="stepStatus[s.state]" :title="t(`steps.${s.key}`)" />
      </n-steps>
      <div class="ml-auto flex max-w-full flex-wrap items-center justify-end gap-2.5">
        <n-select v-if="edition !== 'pc'" :value="cfg.emulator" :options="emuOptions" :disabled="coreBusy" @update:value="v => runCoreAction(() => changeLocateConfig(v, cfg.locateMode))"
          size="small" :title="t('emu.pickTitle')" class="w-36" :consistent-menu-width="false" />
        <span v-else class="bg-[#0d1117] border border-slate-700/80 rounded-lg px-2.5 py-1.5 text-xs font-medium text-sky-300">{{ t('emu.pc') }}</span>
        <n-select v-if="edition !== 'pc'" :value="cfg.locateMode" :options="modeOptions" :disabled="coreBusy" @update:value="v => runCoreAction(() => changeLocateConfig(cfg.emulator, v))"
          size="small" :title="t('locateMode.hint')" class="w-40" :consistent-menu-width="false" />
        <div class="inline-flex shrink-0 flex-nowrap items-center gap-2 whitespace-nowrap">
          <n-button size="small" secondary @click="openMap" class="w-[124px]">
            🧭 {{ t('buttons.openMap') }}
          </n-button>
          <n-button size="small" :type="locateState === 'idle' ? 'primary' : 'warning'" class="w-[128px] font-bold" :disabled="coreBusy" @click="runCoreAction(toggleLocating)">
            {{ locateState === 'working' ? '■' : '▶' }} {{ t('buttons.' + (locateState === 'working' ? 'locating' : 'start')) }}
          </n-button>
        </div>
      </div>
    </section>

    <!-- ③ 两列：左 4 列 实时数据 ｜ 右 6 列 本地解析进度与存档 -->
    <div class="instrument-grid grid grid-cols-10 gap-3 items-stretch shrink-0">

      <!-- 左 4 列 -->
      <div class="col-span-4 flex flex-col gap-3">

        <!-- 实时同步坐标卡 -->
        <section class="slate-panel survey p-3 flex-1">
          <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2">
            <span class="font-bold text-sky-400 flex items-center gap-1.5">{{ t('locate.liveCoords') }}</span>
            <span class="ui-caption text-muted min-w-0 truncate text-right" :title="pos.lockAddr || t('locate.waitingAddr')">
              {{ t('locate.offset') }}: <span :class="['text-sky-300', { 'font-mono tabular-nums': pos.lockAddr }]">{{ pos.lockAddr || t('locate.waitingAddr') }}</span>
            </span>
          </div>
          <dl class="readouts">
            <div v-for="(axis, i) in ['gx', 'gy', 'gz']" :key="axis">
              <dt>{{ i === 0 ? 'X' : t(i === 1 ? 'locate.axisY' : 'locate.axisZ') }}</dt>
              <dd :class="{ pending: !pos.ok || pos[axis] == null }">{{ fmt(pos[axis]) }}</dd>
            </div>
          </dl>
          <div class="layer-line flex items-center justify-between text-xs">
            <span class="text-slate-400">{{ t('locate.currentLayer') }}:</span>
            <span class="font-bold text-emerald-400 font-mono">{{ regionText }}<span class="text-blue-300 text-[10px] ml-2">[{{ pos.source || '-' }}]</span></span>
          </div>
        </section>

        <!-- 坐标校准卡（BOTW 定向找人）：模组注入模式下坐标由游戏内模组直接提供，此功能不可用 -->
        <section class="relative slate-panel p-3 shrink-0">
          <div :inert="calibrationDisabled" :class="calibrationDisabled ? 'blur-[3px] opacity-40 pointer-events-none select-none' : ''">
          <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2">
            <span class="font-bold text-emerald-400 flex items-center gap-1.5">🧭 {{ t('locate.calibration') }}</span>
            <div class="flex items-center gap-2">
              <span class="text-[10px] text-slate-500">{{ t('locate.calWhenFail') }}</span>
              <n-button size="tiny" type="primary" :loading="calBusy" @click="doCalibrate">
                ↻ {{ t('buttons.submitCalib') }}
              </n-button>
            </div>
          </div>
          <div class="text-[10px] text-slate-500 mb-1.5 leading-relaxed">{{ t('locate.calHint') }}</div>
          <div class="grid grid-cols-3 gap-1.5 mb-2">
            <n-input-number v-model:value="calX" size="small" :step="0.1" :show-button="false"
              :min="-7000" :max="7000" placeholder="-215.0" class="font-mono">
              <template #prefix><span class="text-[10px] text-slate-500">X</span></template>
            </n-input-number>
            <n-input-number v-model:value="calY" size="small" :step="0.1" :show-button="false"
              :min="-600" :max="5000" placeholder="Y" class="font-mono">
              <template #prefix><span class="text-[10px] text-slate-500">Y</span></template>
            </n-input-number>
            <n-input-number v-model:value="calZ" size="small" :step="0.1" :show-button="false"
              :min="-7000" :max="7000" placeholder="250.0" class="font-mono">
              <template #prefix><span class="text-[10px] text-slate-500">Z</span></template>
            </n-input-number>
          </div>
          <div class="flex items-center gap-1.5 flex-wrap text-[10px]">
            <span class="text-slate-500">预设:</span>
            <n-button size="tiny" tertiary @click="fillPreset(-215, 236, 250)">平原之塔</n-button>
            <n-button size="tiny" tertiary @click="fillPreset(-1071, 248, 1913)">复苏神庙</n-button>
            <n-button size="tiny" tertiary @click="fillPreset(3435, 228, 1968)">哈特诺塔</n-button>
          </div>
          </div>
          <div v-if="calibrationDisabled"
            class="absolute inset-0 flex flex-col items-center justify-center gap-1.5 rounded-xl px-4 text-center">
            <span class="text-sm font-bold text-emerald-300">🧲 {{ t('locateMode.modActive') }}</span>
            <span class="text-[10px] text-slate-400">{{ t('locateMode.modActiveHint') }}</span>
          </div>
        </section>
      </div>

      <!-- 右 6 列：本地解析进度与存档 -->
      <div class="col-span-6 flex flex-col min-h-0">
        <section class="slate-panel p-3 flex flex-col flex-1 w-full">
          <div class="shrink-0">
            <div class="flex items-center justify-between text-xs pb-1.5 border-b border-slate-800/80 mb-2.5">
              <div class="flex items-center gap-2">
                <span class="font-bold text-emerald-400">{{ t('locate.localProgress') }}</span>
                <span v-if="saveSourceTag" class="text-[10px] px-1.5 py-0.2 rounded bg-slate-800 border border-slate-700 text-sky-400 font-mono">{{ saveSourceTag }}</span>
              </div>
              <span class="text-[11px] font-mono text-slate-400">{{ t('locate.playtime') }}: <span class="text-white font-bold">{{ playtimeText }}</span></span>
            </div>

            <!-- 18 项探索度：平铺，不分组（按 探索→任务→收集→讨伐 顺序） -->
            <div class="ledger grid grid-cols-3 gap-x-3">
              <div v-for="it in STAT_ITEMS" :key="it.k"
                   class="ledger-entry py-1 flex items-center justify-between gap-1"
                   :title="t('locate.saveOnlyHint')">
                <span class="text-slate-400 text-xs truncate">{{ t('locate.' + it.t) }}</span>
                <span class="font-mono text-white font-bold text-xs shrink-0">{{ countText(it.k) }}</span>
              </div>
            </div>
          </div>

          <!-- 生效存档管理条：按钮在上，存档路径在下 -->
          <div class="mt-3 pt-2.5 border-t border-slate-800/80 text-xs">
            <div class="flex items-center justify-between gap-2 flex-wrap">
              <!-- 四个动作按钮 -->
              <div class="flex items-center gap-1.5">
                <n-button size="small" type="primary" @click="pickSaveFile">{{ t('buttons.chooseFile') }}</n-button>
                <n-button size="small" @click="pickSaveDir">{{ t('buttons.chooseDir') }}</n-button>
                <n-button v-if="progressData.save" size="small" @click="doOpenSave">{{ t('buttons.locateFile') }}</n-button>
                <n-button size="small" type="warning" secondary @click="doDetectSave">{{ t('buttons.detectSave') }}</n-button>
              </div>

              <!-- 右侧：自动检测存档开关 + 最新存档相对时间提醒 -->
              <div class="flex items-center gap-2.5 ml-auto text-[11px]">
                <n-checkbox v-model:checked="cfg.autoDetectSave" size="small" @update:checked="saveSettings">
                  <span class="text-slate-400">{{ t('locate.autoDetectSave') }}</span>
                </n-checkbox>
                <div v-if="saveTimeAgoText" class="font-mono text-[11px] shrink-0" :class="saveTimeAgoClass">
                  {{ t('locate.latestSaveTime') }}: {{ saveTimeAgoText }}
                </div>
              </div>
            </div>
            <n-input size="small" readonly class="mt-2 font-mono" :value="progressData.save || ''"
              :placeholder="t('locate.savePlaceholder')" />
          </div>
        </section>
      </div>
    </div>

    <!-- ④ 实时日志 -->
    <LogPanel class="slate-panel" style="min-height: 8rem" />

  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NSelect, NButton, NCheckbox, NSteps, NStep, NInputNumber, NInput } from 'naive-ui'
import LogPanel from '../components/LogPanel.vue'
import SettingsPop from '../components/SettingsPop.vue'
import AboutPop from '../components/AboutPop.vue'
import { notify } from '../composables/message'
import {
  pos, steps, coreRunning, coreBusy, changeLocateConfig, toggleLocating, progressData,
  openMap, cfg, saveSettings, ver, edition, syncText, syncClass, syncDot,
  rt, calibrateCoords, openSave, pickSaveFile, pickSaveDir, detectSave,
  saveTimeAgoText, saveTimeAgoClass
} from '../composables/useCore'

const { t } = useI18n()

const stepStatus = { done: 'finish', active: 'process', idle: 'wait' }
const calibrationDisabled = computed(() => cfg.locateMode === 'mod-inject')

// Naive UI 下拉选项（label 随 i18n 实时变化）
const emuOptions = computed(() => [
  { label: t('emu.auto'), value: 'auto' },
  { label: t('emu.cemu'), value: 'cemu' },
  { label: t('emu.ryujinx'), value: 'ryujinx' },
  { label: t('emu.eden'), value: 'eden' }
])
const modeOptions = computed(() => [
  { label: t('locateMode.auto'), value: 'auto' },
  { label: t('locateMode.modInject'), value: 'mod-inject',
    disabled: cfg.emulator !== 'auto' && cfg.emulator !== 'eden' }
])

async function runCoreAction(action) {
  try {
    await action()
  } catch (error) {
    notify('error', String(error?.message || error))
  }
}

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
  if (edition.value === 'pc') return t('emu.pc')
  const e = cfg.emulator || 'auto'
  return { auto: t('emu.auto'), cemu: 'Cemu', ryujinx: 'Ryujinx', eden: 'Eden' }[e] || e
})

const headerMapHost = 'botw.yalin.site'

// 定位按钮两态：开始定位（蓝）/ 停止定位（橙）——秒锁时代"定位中/成功"中间态已无信息量
const locateState = computed(() => {
  if (!coreRunning.value) return 'idle'
  return 'working'
})

function fmt(v) { return (!pos.ok || v == null) ? '-' : Number(v).toFixed(1) }

const regionText = computed(() => {
  if (!pos.ok) return '等待连接'
  if (Math.abs(pos.gx) > 7000 || Math.abs(pos.gz) > 7000) return '未知区域'
  return '海拉鲁世界'
})

const playtimeText = computed(() => {
  const secs = progressData.value?.playtime || 0
  if (!secs) return '-'
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60)
  return `${h}h${String(m).padStart(2,'0')}m`
})

const saveSourceTag = computed(() => progressData.value?.saveSource || '')

// 18 项探索度（与网页面板同口径）：平铺顺序 = 探索 6 → 任务 4 → 收集 5 → 讨伐 3
// 探索 6 项来自核心既有 counts，其余 12 项来自 v3.2.1 新增的 stats
const STAT_ITEMS = [
  { k: 'shrine', t: 'shrine' }, { k: 'tower', t: 'tower' }, { k: 'beast', t: 'beast' },
  { k: 'korok', t: 'korok' }, { k: 'memory_photo', t: 'memoryPhoto' }, { k: 'memory_final', t: 'memoryFinal' },
  { k: 'quest_main', t: 'questMain' }, { k: 'quest_side', t: 'questSide' },
  { k: 'quest_shrine', t: 'questShrine' }, { k: 'quest_dlc', t: 'questDlc' },
  { k: 'chest', t: 'chest' }, { k: 'weapon', t: 'weapon' }, { k: 'pb_equip', t: 'pbEquip' },
  { k: 'pb_mat', t: 'pbMat' }, { k: 'pb_enemy', t: 'pbEnemy' },
  { k: 'boss_hinox', t: 'bossHinox' }, { k: 'boss_talus', t: 'bossTalus' }, { k: 'boss_molduga', t: 'bossMolduga' }
]

function countText(k) {
  const c = progressData.value?.stats?.[k] || progressData.value?.counts?.[k]
  if (!c) return '-'
  return `${c[0]}/${c[1]}`
}

const calX = ref(null)
const calY = ref(null)
const calZ = ref(null)
const calBusy = ref(false)

function fillPreset(x, y, z) {
  if (calibrationDisabled.value) return
  calX.value = x
  calY.value = y
  calZ.value = z
}

async function doCalibrate() {
  if (calibrationDisabled.value || calBusy.value) return
  if (calX.value == null || calZ.value == null) {
    notify('warning', t('locate.calNeedXZ'))
    return
  }
  calBusy.value = true
  try {
    const msg = await calibrateCoords(calX.value, calY.value ?? 0, calZ.value)
    notify('success', msg || t('locate.calSubmitted'))
  } finally {
    calBusy.value = false
  }
}

async function doOpenSave() {
  const msg = await openSave()
  if (msg && !msg.includes('已在资源管理器')) notify('info', msg)
}

async function doDetectSave() {
  const msg = await detectSave()
  if (msg) notify('info', msg)
}

</script>

<style scoped>
.slate-map {
  --line: #315461;
  --aqua: #86ceda;
  --paper: #f2eada;
  color: var(--paper);
  background-color: #09171f;
  background-image: radial-gradient(#36525c66 .7px, transparent .9px);
  background-size: 16px 16px;
}
.slate-mast {
  position: relative;
  z-index: 2;
  border-block: 1px solid var(--line);
  background: linear-gradient(100deg, #16323d, #0a1c25 68%);
}
.ui-caption { font: 500 13px/1.45 "Segoe UI", "Microsoft YaHei UI", sans-serif; }
.mast-title { font-size: 16px; font-weight: 650; letter-spacing: .065em; }
.slate-panel {
  position: relative;
  background-color: #10232c;
  border: 1px solid var(--line);
  border-radius: 2px;
}
.slate-panel::before, .slate-panel::after {
  content: '';
  position: absolute;
  width: 17px;
  height: 7px;
  border-color: var(--aqua);
  border-style: solid;
  pointer-events: none;
}
.slate-panel::before { top: 2px; left: 2px; border-width: 1px 0 0 1px; }
.slate-panel::after { bottom: 2px; right: 2px; border-width: 0 1px 1px 0; }
.survey {
  display: flex;
  flex-direction: column;
  container-type: inline-size;
  background-image: repeating-radial-gradient(ellipse at 105% 160%, transparent 0 17px, #609baa23 18px, transparent 19px 29px), linear-gradient(112deg, #102b35 30%, #183a45);
}
.readouts {
  flex: 1;
  display: grid;
  align-content: center;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding-block: 15px 20px;
  font-family: 'IBM Plex Mono', Consolas, monospace;
}
.readouts > div + div { padding-left: 8px; border-left: 1px solid var(--line); }
.readouts dt { font-size: 11px; letter-spacing: .08em; color: var(--aqua); }
.readouts dd {
  margin-top: 5px;
  color: var(--paper);
  font-size: clamp(19px, 6.5cqw, 36px);
  line-height: 1.15;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.readouts .pending { color: #9aafb3; }
.layer-line { border-top: 1px solid var(--line); padding-top: 7px; }
.ledger-entry { border-bottom: 1px solid #28414b; }
.slate-panel .text-white { color: var(--paper); }
.slate-panel .text-slate-500, .slate-mast .text-slate-500 { color: #a0b3b7; }
.slate-panel .text-emerald-400, .slate-panel .text-sky-400 { color: var(--aqua); }
@media (max-width: 900px) {
  .instrument-grid { grid-template-columns: minmax(0, 1fr); }
  .instrument-grid > div { grid-column: auto; }
}
</style>
