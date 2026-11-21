<template>
  <section class="bg-[var(--slate-panel)] text-[color:var(--slate-ink)] border border-[color:var(--slate-line)] rounded-md flex flex-col text-[11px] min-h-0 flex-1">
    <div class="flex items-center justify-between px-2.5 py-1.5 border-b border-[color:var(--slate-line)] shrink-0">
      <span class="log-title font-sans text-[11px] font-semibold uppercase tracking-wider">{{ t('log.title') }}</span>
      <n-checkbox v-model:checked="issuesOnly" size="small">{{ t('issuesOnly') }}</n-checkbox>
    </div>
    <div ref="logBox" tabindex="0" role="region" :aria-label="t('log.title')" class="flex-1 overflow-y-auto px-2.5 py-1.5 space-y-0.5 leading-relaxed text-[color:var(--slate-muted)] min-h-0 select-text font-mono">
      <div v-for="(log, idx) in visibleLogs" :key="idx" class="log-row" :data-level="log.level">
        <span class="text-[color:var(--slate-muted)]">{{ log.time }}</span>
        <span class="log-level mr-1">{{ log.level }}</span><span class="log-message min-w-0 break-words">{{ log.text }}</span>
      </div>
      <div v-if="!visibleLogs.length" class="text-[color:var(--slate-muted)] py-4 text-center">{{ t('log.none') }}</div>
    </div>
    <div v-if="showUpload" class="flex items-center gap-2 px-2.5 py-1.5 border-t border-[color:var(--slate-line)] bg-black/10 shrink-0 text-[11px] font-sans">
      <n-input v-model:value="uploadNote" size="tiny" :placeholder="t('buttons.notePlaceholder')" class="flex-1" @keyup.enter="doUpload" />
      <span v-if="uploadMsg" class="text-[11px] text-[color:var(--slate-gold)] shrink-0">{{ uploadMsg }}</span>
      <n-button size="tiny" type="primary" :disabled="uploading" @click="doUpload">{{ t('buttons.send') }}</n-button>
      <n-button size="tiny" quaternary @click="showUpload = false">{{ t('buttons.cancel') }}</n-button>
    </div>
    <div class="flex items-center justify-end flex-wrap gap-x-3 gap-y-1 px-2.5 py-1.5 border-t border-[color:var(--slate-line)] shrink-0 text-[11px] font-sans">
      <n-input v-model:value="filter" size="tiny" :placeholder="t('log.filter')" style="width: 10rem; max-width: 25%" />
      <span class="shrink-0 whitespace-nowrap text-[color:var(--slate-muted)]">{{ shown }} / {{ total }}</span>
      <n-button size="tiny" quaternary type="error" @click="clearLogs">{{ t('buttons.clear') }}</n-button>
      <n-button size="tiny" quaternary type="info" @click="showUpload = !showUpload; uploadMsg = ''">{{ t('buttons.uploadDiag') }}</n-button>
      <n-button size="tiny" quaternary @click="exportDiag">{{ t('buttons.exportDiag') }}</n-button>
      <n-button size="tiny" quaternary @click="openLogDir">{{ t('buttons.openDir') }}</n-button>
    </div>
  </section>
</template>

<script setup>
import { ref, watch, nextTick, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { NInput, NButton, NCheckbox } from 'naive-ui'
import { logs, filteredLogs, logFilter, clearLogs, exportDiag, uploadDiag, openLogDir } from '../composables/useCore'

const { t } = useI18n({ messages: { 'zh-CN': { issuesOnly: '仅警告和错误' }, 'en-US': { issuesOnly: 'Warnings and errors only' } } })
const issuesOnly = ref(false)
const visibleLogs = computed(() => issuesOnly.value
  ? filteredLogs.value.filter(row => row.level === 'warn' || row.level === 'error')
  : filteredLogs.value)

const showUpload = ref(false)
const uploadNote = ref('')
const uploadMsg = ref('')
const uploading = ref(false)
async function doUpload() {
  if (uploading.value) return
  uploading.value = true
  uploadMsg.value = '...'
  try {
    uploadMsg.value = await uploadDiag(uploadNote.value)
    if (uploadMsg.value.includes('感谢')) uploadNote.value = ''
  } finally {
    uploading.value = false
  }
}

const filter = computed({
  get: () => logFilter.value,
  set: v => { logFilter.value = v }
})
const total = computed(() => logs.value.length)
const shown = computed(() => visibleLogs.value.length)

const logBox = ref(null)
let prevLen = 0
watch(() => logs.value.length, (n) => {
  if (n > prevLen) {
    prevLen = n
    nextTick(() => { if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight })
  }
})
</script>


<style scoped>
.log-title { display: inline-flex; align-items: center; gap: .55rem; }
.log-title::before { content: ''; width: 1rem; height: 1px; background: var(--slate-gold); }
[tabindex="0"]:focus-visible { outline: 2px solid var(--slate-cyan); outline-offset: -2px; }
.log-row { display: grid; grid-template-columns: 13ch 6ch minmax(0, 1fr); gap: .5rem; font-variant-numeric: tabular-nums; }
.log-row[data-level="ok"] { --log-label: #6ee7b7; --log-body: #a7f3d0; }
.log-row[data-level="warn"] { --log-label: #fbbf24; --log-body: #fde68a; }
.log-row[data-level="error"] { --log-label: #f87171; --log-body: #fecaca; }
.log-level { color: var(--log-label, var(--slate-cyan)); font-weight: 600; }
.log-message { color: var(--log-body, var(--slate-ink)); }
</style>
