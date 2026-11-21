<template>
  <div class="bg-[var(--slate-panel)] text-[color:var(--slate-ink)] border border-[color:var(--slate-line)] rounded-lg max-w-[calc(100vw-2rem)] p-4 shadow-2xl text-xs space-y-2.5 w-[360px]" style="--wails-draggable:no-drag;">
    <div class="slate-heading">
      <span>{{ t('about.title') }}</span>
      <button type="button" :aria-label="t('header.close')" class="close-control" @click="$emit('close')">✕</button>
    </div>
    <div class="text-[color:var(--slate-ink)]">{{ t('about.desc') }}</div>
    <div class="text-[color:var(--slate-muted)] font-mono">{{ t('about.version') }}：<span class="text-[color:var(--slate-cyan)] font-bold">{{ ver }}</span></div>
    <div class="text-[color:var(--slate-muted)] font-mono">{{ t('about.map') }}：<a href="https://botw.yalin.site/" target="_blank" class="text-[color:var(--slate-cyan)] underline underline-offset-2">botw.yalin.site</a></div>
    <div class="text-[color:var(--slate-muted)] font-mono">{{ t('about.github') }}：<a href="https://github.com/yalincc/BOTWmap" target="_blank" class="text-[color:var(--slate-cyan)] underline underline-offset-2">yalincc/BOTWmap</a></div>
    <div class="text-[color:var(--slate-muted)] pt-1 border-t border-[color:var(--slate-line)]">{{ t('about.note') }}</div>
    <div class="flex justify-end gap-2 pt-2 border-t border-[color:var(--slate-line)]">
      <n-button size="small" @click="checkUpdate">{{ t('buttons.checkUpdate') }}</n-button>
      <n-button size="small" type="primary" @click="$emit('close')">{{ t('header.close') }}</n-button>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { NButton } from 'naive-ui'
import { notify } from '../composables/message'
import { ver, rt } from '../composables/useCore'

defineEmits(['close'])
const { t } = useI18n()

function checkUpdate() {
  rt.EventsOn("update:latest", () => notify('info', t('update.latestHint')))
  rt.EventsEmit("update:check")
}
</script>

<style scoped>
.slate-heading {
  display: flex;
  align-items: center;
  gap: .65rem;
  font-size: .875rem;
  font-weight: 600;
}
.slate-heading::after {
  content: '';
  flex: 1;
  height: 1px;
  background: linear-gradient(90deg, var(--slate-gold), transparent);
  opacity: .55;
}
.close-control {
  order: 1;
  width: 1.75rem;
  height: 1.75rem;
  border-radius: .25rem;
  color: var(--slate-muted);
}
.close-control:hover { color: var(--slate-cyan); }
:focus-visible { outline: 2px solid var(--slate-cyan); outline-offset: 2px; }
</style>
