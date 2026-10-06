// regions.js — BOTW 区域锚点（由 progress_points.json 的 15 座高塔生成，坐标系 = 地图像素 mx/my）。
// 高塔名即区域名；findRegion 按最近塔匹配（BOTW 单层，layer 恒 0）。
export const REGIONS = [
  { x: 18616.0, y: 7000.0, name: '阿卡莱之塔' },
  { x: 10423.2, y: 10884.0, name: '平原之塔' },
  { x: 14034.0, y: 13428.0, name: '双子山之塔' },
  { x: 16348.0, y: 6886.0, name: '奥尔汀之塔' },
  { x: 14662.0, y: 16547.2, name: '费罗尼之塔' },
  { x: 4668.0, y: 13656.8, name: '格鲁德之塔' },
  { x: 10880.0, y: 13390.0, name: '初始之塔' },
  { x: 17471.2, y: 14267.2, name: '哈特诺之塔' },
  { x: 7654.0, y: 5932.0, name: '海布拉之塔' },
  { x: 11936.0, y: 15923.2, name: '湖之塔' },
  { x: 16516.0, y: 9782.0, name: '拉聂尔之塔' },
  { x: 8488.8, y: 8451.2, name: '丘陵之塔' },
  { x: 4772.8, y: 8020.0, name: '达邦挞之塔' },
  { x: 7386.0, y: 14875.2, name: '荒野之塔' },
  { x: 13768.0, y: 6788.8, name: '森林之塔' },
]

export function findRegion(mx, my, layer) {
  if (layer !== 0) return null
  let best = null, bestD = Infinity
  for (const r of REGIONS) {
    const d = (r.x - mx) * (r.x - mx) + (r.y - my) * (r.y - my)
    if (d < bestD) { bestD = d; best = r }
  }
  return best
}
