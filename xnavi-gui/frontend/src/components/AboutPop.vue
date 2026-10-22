<template>
  <div class="bg-[#161b22] border border-slate-700 rounded-xl p-4 shadow-2xl text-xs space-y-2.5 w-[360px]" style="--wails-draggable:no-drag;">
    <div class="text-sm font-bold text-white mb-1 flex items-center justify-between">
      <span>{{ t('about.title') }}</span>
      <button class="text-slate-500 hover:text-white text-xs" @click="$emit('close')">✕</button>
    </div>
    <div class="text-slate-300">{{ t('about.desc') }}</div>
    <div class="text-slate-400 font-mono">{{ t('about.version') }}：<span class="text-sky-400 font-bold">{{ ver }}</span></div>
    <div class="text-slate-400 font-mono">{{ t('about.map') }}：<a href="https://botw.yalin.site/" target="_blank" class="text-blue-400 hover:underline">botw.yalin.site</a></div>
    <div class="text-slate-400 font-mono">{{ t('about.github') }}：<a href="https://github.com/yalincc/BOTWmap" target="_blank" class="text-blue-400 hover:underline">yalincc/BOTWmap</a></div>
    <div class="text-slate-500 pt-1 border-t border-slate-800">{{ t('about.note') }}</div>
    <div class="flex justify-end gap-2 pt-2 border-t border-slate-800">
      <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs text-slate-200" @click="checkUpdate">{{ t('buttons.checkUpdate') }}</button>
      <button class="bg-blue-600 hover:bg-blue-500 px-3 py-1.5 rounded text-xs text-white" @click="$emit('close')">{{ t('header.close') }}</button>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ver, rt } from '../composables/useCore'

defineEmits(['close'])
const { t } = useI18n()

function checkUpdate() {
  rt.EventsOn("update:latest", () => { alert("已经是最新版") })
  rt.EventsEmit("update:check")
}
</script>

