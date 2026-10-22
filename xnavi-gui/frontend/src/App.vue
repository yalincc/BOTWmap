<template>
  <div class="flex flex-col h-screen w-full bg-[#0d1117] text-slate-200 select-none overflow-hidden font-sans relative"
    @dragover.prevent="isDragging = true"
    @dragleave.prevent="isDragging = false"
    @drop.prevent="onDrop">

    <!-- 拖拽载入存档覆盖遮罩 -->
    <div v-if="isDragging" class="absolute inset-0 bg-blue-950/80 border-2 border-dashed border-blue-400 z-50 flex items-center justify-center backdrop-blur-xs pointer-events-none">
      <div class="bg-[#161b22] px-6 py-5 rounded-lg shadow-2xl border border-blue-500 text-center">
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
      <div class="bg-[#161b22] border border-emerald-500/50 rounded-lg p-5 w-96 shadow-2xl">
        <div class="text-sm font-bold text-emerald-400 mb-2">{{ t('update.available') }}</div>
        <div class="text-xs text-slate-300">{{ t('update.current') }}：{{ ver }} → {{ t('update.latest') }}：{{ updateInfo.latest }}</div>
        <div class="flex justify-end gap-2 mt-4">
          <button class="bg-slate-700 hover:bg-slate-600 px-3 py-1.5 rounded text-xs" @click="updateInfo = null">{{ t('buttons.later') }}</button>
          <button class="bg-emerald-600 hover:bg-emerald-500 px-3 py-1.5 rounded text-xs text-white" @click="rt.BrowserOpenURL(updateInfo.url); updateInfo = null">{{ t('buttons.download') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import LocateView from './views/LocateView.vue'
import { ver, updateInfo, initCore, rt, applySave } from './composables/useCore'

const { t } = useI18n()
const isDragging = ref(false)

async function onDrop(e) {
  isDragging.value = false
  if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
    const file = e.dataTransfer.files[0]
    const p = file.path || file.name
    if (p) {
      const msg = await applySave(p)
      alert(msg)
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
        alert(msg)
      }
    })
  }
})
</script>
