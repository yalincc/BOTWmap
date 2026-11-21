<template>
  <n-config-provider :theme="darkTheme" :locale="naiveThemeLocale.locale" :date-locale="naiveThemeLocale.dateLocale" :theme-overrides="themeOverrides">
    <n-message-provider placement="top">
      <MessageBridge />
      <div class="flex flex-col h-screen w-full slate-shell text-ink select-none overflow-hidden font-sans relative"
        @dragover.prevent="isDragging = true"
        @dragleave.prevent="isDragging = false"
        @drop.prevent="onDrop">

        <!-- 拖拽载入存档覆盖遮罩 -->
        <div v-if="isDragging" class="absolute inset-0 bg-base/90 border-2 border-dashed border-sheikah z-50 flex items-center justify-center backdrop-blur-xs pointer-events-none">
          <div class="bg-surface px-6 py-5 rounded-lg shadow-2xl border border-sheikah text-center">
            <div class="text-3xl mb-1">📥</div>
            <div class="text-sm font-semibold text-white">松开鼠标载入存档</div>
            <div class="text-xs text-slate-400 mt-1">支持拖入 game_data.sav 文件或包含它的目录</div>
          </div>
        </div>

        <!-- 主定位视图 -->
        <main class="flex-1 flex flex-col min-h-0 overflow-hidden">
          <LocateView />
        </main>

        <!-- 版本更新提示弹窗 -->
        <div v-if="updateInfo" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="updateInfo = null">
          <div class="bg-surface border border-edge rounded-lg p-5 w-96 shadow-2xl">
            <div class="text-sm font-bold text-sheikah mb-2">{{ t('update.available') }}</div>
            <div class="text-xs text-slate-300">{{ t('update.current') }}：{{ ver }} → {{ t('update.latest') }}：{{ updateInfo.latest }}</div>
            <div class="flex justify-end gap-2 mt-4">
              <n-button size="small" @click="updateInfo = null">{{ t('buttons.later') }}</n-button>
              <n-button size="small" type="success" @click="rt.BrowserOpenURL(updateInfo.url); updateInfo = null">{{ t('buttons.download') }}</n-button>
            </div>
          </div>
        </div>
      </div>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { NConfigProvider, NMessageProvider, NButton } from 'naive-ui'
import MessageBridge from './components/MessageBridge.vue'
import LocateView from './views/LocateView.vue'
import { darkTheme, themeOverrides, naiveLocales } from './naiveTheme'
import { ver, updateInfo, initCore, rt, applySave } from './composables/useCore'
import { notify } from './composables/message'

const { t, locale } = useI18n()
const isDragging = ref(false)

// Naive UI 主题/语言跟随应用 i18n（集中配置见 naiveTheme.js）
const naiveThemeLocale = computed(() => naiveLocales(locale.value))

async function onDrop(e) {
  isDragging.value = false
  if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
    const file = e.dataTransfer.files[0]
    const p = file.path || file.name
    if (p) {
      const msg = await applySave(p)
      notify('info', msg)
    }
  }
}

onMounted(() => {
  initCore()
  if (rt.OnFileDrop) {
    rt.OnFileDrop(async (x, y, droppedPaths) => {
      isDragging.value = false
      if (droppedPaths && droppedPaths.length > 0) {
        const msg = await applySave(droppedPaths[0])
        notify('info', msg)
      }
    })
  }
})
</script>
