<template>
  <div class="flex-1 flex flex-col gap-3 min-h-0 p-3 overflow-y-auto">
    <!-- 总览 -->
    <section class="bg-[#161b22] border border-emerald-500/30 rounded-md p-3 shrink-0">
      <div class="flex items-center justify-between">
        <div class="text-[11px] font-semibold text-emerald-400 uppercase tracking-wider">存档进度</div>
        <div class="text-[10px] text-slate-500 font-mono truncate max-w-[60%]" :title="progressData.save">{{ saveBasename }}</div>
      </div>
      <div v-if="progressData.counts" class="flex items-center gap-3 mt-2">
        <div class="text-xs text-slate-400">总体探索度</div>
        <div class="flex-1 h-2 bg-[#0d1117] border border-slate-800 rounded overflow-hidden">
          <div class="h-full bg-gradient-to-r from-emerald-600 to-emerald-400 transition-all" :style="{ width: overallPct + '%' }"></div>
        </div>
        <div class="text-sm font-mono text-white">{{ overallPct.toFixed(1) }}%</div>
      </div>
      <div v-else class="text-[11px] text-slate-500 mt-2">等待读取存档...（需先开始定位）</div>
    </section>

    <!-- 分类列表 -->
    <section v-if="progressData.counts" class="grid grid-cols-1 md:grid-cols-3 gap-2 shrink-0">
      <div v-for="c in categories" :key="c.key" class="bg-[#161b22] border border-slate-800 rounded-md p-2.5">
        <div class="flex items-center justify-between text-xs">
          <span class="text-slate-300">{{ c.label }}</span>
          <span class="font-mono text-white">{{ done(c.key) }}<span class="text-slate-500">/{{ total(c.key) }}</span></span>
        </div>
        <div class="h-1.5 bg-[#0d1117] border border-slate-800 rounded overflow-hidden mt-1.5">
          <div class="h-full bg-emerald-500/80 transition-all" :style="{ width: pct(c.key) + '%' }"></div>
        </div>
      </div>
    </section>

    <section v-else class="text-center text-slate-600 text-xs py-10">暂无存档数据</section>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { progressData } from '../composables/useCore'

// 与 progress_botw.go counts 全量对齐（core status.json 现算；BOTW 5 大类）
const categories = [
  { key: 'shrine', label: '神庙' },
  { key: 'tower', label: '高塔' },
  { key: 'korok', label: '克洛格' },
  { key: 'memory', label: '回忆' },
  { key: 'beast', label: '英杰神兽' }
]

function count(key) { return progressData.value?.counts?.[key] || null }
function done(key) { const c = count(key); return c ? c.done : '-' }
function total(key) { const c = count(key); return c ? c.total : '-' }
function pct(key) {
  const c = count(key)
  if (!c || !c.total) return 0
  return Math.round(c.done / c.total * 100)
}
const overallPct = computed(() => {
  const counts = progressData.value?.counts
  if (!counts) return 0
  let d = 0, t = 0
  for (const k of categories) {
    const c = counts[k]
    if (c && c.total) { d += c.done; t += c.total }
  }
  // 保留 1 位小数（0.5% 级别可显示），进度条宽度同步用该值
  return t ? Math.round(d / t * 1000) / 10 : 0
})
const saveBasename = computed(() => {
  const s = progressData.value?.save
  if (!s) return ''
  return s.split(/[\\/]/).slice(-2).join('\\')
})
</script>
