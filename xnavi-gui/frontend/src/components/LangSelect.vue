<template>
  <div class="flex items-center justify-between gap-3 py-1 text-[color:var(--slate-muted)]">
    <span class="text-xs">界面语言 / Language</span>
    <n-select aria-label="界面语言 / Language" v-model:value="selected" :options="langOptions" @update:value="onChange"
      size="small" class="language-select" :consistent-menu-width="false" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { NSelect } from 'naive-ui'
import { LOCALES, currentLocale, setLocale } from '../i18n'

const selected = ref(currentLocale())
const langOptions = [
  { label: '自动 / Follow System', value: LOCALES.SYSTEM },
  { label: '简体中文', value: LOCALES.ZH },
  { label: 'English', value: LOCALES.EN }
]

function onChange() {
  setLocale(selected.value)
}
</script>

<style scoped>
.language-select { width: 10rem; max-width: 55%; }
</style>
