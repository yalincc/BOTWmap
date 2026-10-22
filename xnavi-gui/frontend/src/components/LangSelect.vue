<template>
  <div class="flex items-center justify-between py-1 text-slate-400">
    <span class="text-xs">界面语言 / Language</span>
    <select v-model="selected" @change="onChange"
      class="bg-[#0d1117] border border-slate-700 rounded px-2 py-1 text-xs text-white focus:border-blue-500 focus:outline-none">
      <option :value="LOCALES.SYSTEM">自动 / Follow System</option>
      <option :value="LOCALES.ZH">简体中文</option>
      <option :value="LOCALES.EN">English</option>
    </select>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { LOCALES, currentLocale, setLocale } from '../i18n'

const selected = ref(currentLocale())

function onChange() {
  setLocale(selected.value)
}
</script>

