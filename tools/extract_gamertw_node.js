// Extract gamertw BOTW map data: categories (h) and markers (T) as JSON
const fs = require('fs');

const src = fs.readFileSync('gamertw_chunk_7872.js', 'utf-8');

function extractArray(varName, source) {
  // find "varName=[" or "varName = [" or similar patterns
  const re = new RegExp(varName + '\\s*=\\s*\\[', 'g');
  let m;
  const starts = [];
  while ((m = re.exec(source)) !== null) starts.push(m.index + m[0].length - 1); // index of '['
  if (!starts.length) throw new Error('not found ' + varName);
  // take the longest array (data arrays are big)
  let best = null;
  for (const start of starts) {
    let depth = 0, i = start;
    for (; i < source.length; i++) {
      const c = source[i];
      if (c === '[') depth++;
      else if (c === ']') { depth--; if (depth === 0) break; }
    }
    const text = source.slice(start, i + 1);
    if (!best || text.length > best.text.length) best = { text, start };
  }
  return best.text;
}

const hText = extractArray('h', src);
const tText = extractArray('T', src);

// Build a sandboxed eval: define minimal globals the literal might reference
const sandbox = { undefined: undefined, NaN: NaN, Infinity: Infinity };
const vm = require('vm');
const ctx = vm.createContext(sandbox);
let h, T;
try {
  h = vm.runInContext('(' + hText + ')', ctx);
  T = vm.runInContext('(' + tText + ')', ctx);
} catch (e) {
  console.error('EVAL ERROR:', e.message);
  process.exit(1);
}

console.log('categories (h):', h.length);
for (const c of h) console.log(' -', c.key, '|', c.english, '|', c.chineseTW, '|', c.image);

let totalMarkers = 0;
const perCat = {};
for (const layerDef of T) {
  const catName = layerDef.name;
  let cnt = 0;
  for (const layer of layerDef.layers || []) {
    cnt += (layer.markers || []).length;
  }
  perCat[catName] = cnt;
  totalMarkers += cnt;
}
console.log('top-level T entries:', T.length);
console.log('per-category marker counts:', JSON.stringify(perCat, null, 1));
console.log('total markers:', totalMarkers);

fs.writeFileSync('gamertw_h_categories.json', JSON.stringify(h, null, 1));
fs.writeFileSync('gamertw_T_markers.json', JSON.stringify(T, null, 1));
console.log('saved gamertw_h_categories.json + gamertw_T_markers.json');
