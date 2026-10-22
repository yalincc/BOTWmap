<template>
  <div class="bg-[#161b22] border border-slate-700 rounded-xl p-4 shadow-2xl text-xs space-y-3 w-[440px] max-h-[85vh] overflow-y-auto" style="--wails-draggable:no-drag;">
    <div class="text-[11px] font-semibold text-slate-400 uppercase tracking-wider flex items-center justify-between">
      <span>{{ t('settings.title') }}</span>
      <button class="text-slate-500 hover:text-white" @click="$emit('close')">✕</button>
    </div>

    <!-- 自动打开地图 -->
    <div class="bg-[#0d1117] border border-slate-800 rounded-lg p-2.5 space-y-2">
      <label class="flex items-center gap-2 cursor-pointer select-none">
        <input v-model="cfg.autoOpenMap" type="checkbox" class="accent-blue-600 w-3.5 h-3.5 rounded" />
        <span class="text-white font-medium">{{ t('settings.autoOpenMap') }}</span>
      </label>
    </div>

    <!-- 模拟器存档路径 -->
    <div class="space-y-2.5">
      <div>
        <label class="block text-slate-400 mb-1">{{ t('settings.cemuPath') }}</label>
        <div class="flex gap-1">
          <input v-model="cfg.cemuSaveDir" :placeholder="t('settings.pathAuto')" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white font-mono text-[11px] focus:border-blue-500 focus:outline-none placeholder-slate-600" />
          <button @click="pickDir('cemu')" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">{{ t('buttons.browse') }}</button>
        </div>
      </div>
      <div>
        <label class="block text-slate-400 mb-1">{{ t('settings.ryuPath') }}</label>
        <div class="flex gap-1">
          <input v-model="cfg.ryujinxSaveDir" :placeholder="t('settings.pathAuto')" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white font-mono text-[11px] focus:border-blue-500 focus:outline-none placeholder-slate-600" />
          <button @click="pickDir('ryu')" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">{{ t('buttons.browse') }}</button>
        </div>
      </div>
      <div>
        <label class="block text-slate-400 mb-1">{{ t('settings.edenPath') }}</label>
        <div class="flex gap-1">
          <input v-model="cfg.edenSaveDir" :placeholder="t('settings.pathAuto')" class="flex-1 bg-[#0d1117] border border-slate-700 rounded px-2 py-1.5 text-white font-mono text-[11px] focus:border-blue-500 focus:outline-none placeholder-slate-600" />
          <button @click="pickDir('eden')" class="bg-slate-700 hover:bg-slate-600 px-2 rounded text-xs">{{ t('buttons.browse') }}</button>
        </div>
      </div>
    </div>

    <!-- 手机防息屏 -->
    <div class="pt-1 border-t border-slate-800">
      <label class="flex items-center gap-2 cursor-pointer select-none py-1">
        <input v-model="cfg.tls" type="checkbox" class="accent-blue-600 w-3.5 h-3.5" />
        <span class="text-slate-300">{{ t('settings.tls') }}</span>
        <span class="text-[10px] text-amber-500/90">{{ t('settings.tlsWarn') }}</span>
      </label>
    </div>

    <!-- 语言选择 -->
    <div class="border-t border-slate-800 pt-1">
      <LangSelect />
    </div>

    <div class="flex items-center justify-between pt-2 border-t border-slate-800">
      <span v-if="savedMsg" class="text-[11px] font-mono text-emerald-400">{{ savedMsg }}</span>
      <div class="ml-auto flex gap-2">
        <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs text-slate-300" @click="$emit('close')">{{ t('buttons.cancel') }}</button>
        <button class="bg-blue-600 hover:bg-blue-500 px-4 py-1.5 rounded text-xs text-white font-medium" @click="doSave">{{ t('buttons.save') }}</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LangSelect from './LangSelect.vue'
import { cfg, saveSettings, api } from '../composables/useCore'

const emit = defineEmits(['close'])
const { t } = useI18n()
const savedMsg = ref('')

async function pickDir(kind) {
  const d = await api.PickDir("选择 BOTW 存档目录")
  if (d) {
    if (kind === 'cemu') cfg.cemuSaveDir = d
    else if (kind === 'ryu') cfg.ryujinxSaveDir = d
    else if (kind === 'eden') cfg.edenSaveDir = d
  }
}

async function doSave() {
  await saveSettings()
  savedMsg.value = t('settings.saved')
  setTimeout(() => {
    savedMsg.value = ''
    emit('close')
  }, 800)
}
</script>

