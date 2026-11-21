<template>
  <div class="bg-[var(--slate-panel)] text-[color:var(--slate-ink)] border border-[color:var(--slate-line)] rounded-lg max-w-[calc(100vw-2rem)] p-4 shadow-2xl text-xs space-y-3 w-[440px] max-h-[85vh] overflow-y-auto" style="--wails-draggable:no-drag;">
    <div class="slate-heading">
      <span>{{ t('settings.title') }}</span>
      <button type="button" :aria-label="t('header.close')" class="close-control" @click="$emit('close')">✕</button>
    </div>

    <!-- 自动打开地图 -->
    <div class="bg-black/15 border border-[color:var(--slate-line)] rounded-lg p-2.5 space-y-2">
      <n-checkbox v-model:checked="cfg.autoOpenMap">
        <span class="text-[color:var(--slate-ink)] font-medium">{{ t('settings.autoOpenMap') }}</span>
      </n-checkbox>
    </div>

    <!-- 模拟器存档路径（仅模拟器版显示） -->
    <div v-if="edition !== 'pc'" class="space-y-2.5">
      <div>
        <label class="block text-[color:var(--slate-muted)] mb-1">{{ t('settings.cemuPath') }}</label>
        <div class="flex gap-1">
          <n-input v-model:value="cfg.cemuSaveDir" size="small" :placeholder="t('settings.pathAuto')" class="flex-1 font-mono" />
          <n-button size="small" @click="pickDir('cemu')">{{ t('buttons.browse') }}</n-button>
        </div>
      </div>
      <div>
        <label class="block text-[color:var(--slate-muted)] mb-1">{{ t('settings.ryuPath') }}</label>
        <div class="flex gap-1">
          <n-input v-model:value="cfg.ryujinxSaveDir" size="small" :placeholder="t('settings.pathAuto')" class="flex-1 font-mono" />
          <n-button size="small" @click="pickDir('ryu')">{{ t('buttons.browse') }}</n-button>
        </div>
      </div>
      <div>
        <label class="block text-[color:var(--slate-muted)] mb-1">{{ t('settings.edenPath') }}</label>
        <div class="flex gap-1">
          <n-input v-model:value="cfg.edenSaveDir" size="small" :placeholder="t('settings.pathAuto')" class="flex-1 font-mono" />
          <n-button size="small" @click="pickDir('eden')">{{ t('buttons.browse') }}</n-button>
        </div>
      </div>
    </div>

    <!-- 手机防息屏 -->
    <div class="pt-1 border-t border-[color:var(--slate-line)]">
      <label class="flex items-center gap-2 cursor-pointer select-none py-1">
        <n-checkbox v-model:checked="cfg.tls" size="small" />
        <span class="text-[color:var(--slate-ink)]">{{ t('settings.tls') }}</span>
        <span class="text-[10px] text-[color:var(--slate-gold)]">{{ t('settings.tlsWarn') }}</span>
      </label>
    </div>

    <!-- 语言选择 -->
    <div class="border-t border-[color:var(--slate-line)] pt-1">
      <LangSelect />
    </div>

    <div class="flex items-center justify-between pt-2 border-t border-[color:var(--slate-line)]">
      <span v-if="savedMsg" class="text-[11px] font-mono text-[color:var(--slate-cyan)]">{{ savedMsg }}</span>
      <div class="ml-auto flex gap-2">
        <n-button size="small" @click="$emit('close')">{{ t('buttons.cancel') }}</n-button>
        <n-button size="small" type="primary" @click="doSave">{{ t('buttons.save') }}</n-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCheckbox, NInput, NButton } from 'naive-ui'
import LangSelect from './LangSelect.vue'
import { cfg, edition, saveSettings, api } from '../composables/useCore'

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
