import assert from 'node:assert/strict'
import { appendFileSync, readFileSync, statSync } from 'node:fs'

const dist = new URL('../frontend/dist/', import.meta.url)
const html = readFileSync(new URL('index.html', dist), 'utf8')
const styles = Array.from(html.matchAll(/<link\b[^>]*\bhref="([^"]+\.css)"/g), m => m[1])
const scripts = Array.from(html.matchAll(/<script\b[^>]*\bsrc="([^"]+\.js)"/g), m => m[1])
assert(styles.length && scripts.length, 'dist/index.html must reference CSS and JS bundles')

function checkAsset(reference, base) {
  const asset = new URL(reference, base)
  assert(asset.href.startsWith(dist.href), `Asset outside dist: ${reference}`)
  const stat = statSync(asset)
  assert(stat.isFile() && stat.size > 0, `Empty or invalid asset: ${reference}`)
  return asset
}

for (const script of scripts) checkAsset(script, dist)
const fonts = new Set()
for (const style of styles) {
  const asset = checkAsset(style, dist)
  const css = readFileSync(asset, 'utf8')
  for (const [, font] of css.matchAll(/url\(["']?([^"'()]+\.otf)["']?\)/g)) {
    fonts.add(checkAsset(font, asset).href)
  }
}
for (const weight of ['Regular', 'Bold']) {
  const font = `fonts/Calamity-${weight}.otf`
  assert(fonts.has(new URL(font, dist).href), `Missing CSS reference to ${font}`)
}
// Vite empties dist on each build. Restore the tracked go:embed bootstrap file.
appendFileSync(new URL('.gitkeep', dist), '')
console.log('Verified dist entry, JS, CSS and both Calamity fonts')
