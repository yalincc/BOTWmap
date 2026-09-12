// Extract the TC->EN name dictionary from gamertw chunk 7872
const fs = require('fs');
const src = fs.readFileSync('gamertw_chunk_7872.js', 'utf-8');

// Find the dictionary object: contains 神廟:"Shrine" style pairs. Locate first occurrence and grab the enclosing object literal.
function extractObjectContaining(source, probe) {
  const idx = source.indexOf(probe);
  if (idx < 0) return null;
  // walk back to '{' that starts the object
  let i = idx;
  while (i >= 0 && source[i] !== '{') i--;
  if (i < 0) return null;
  const start = i;
  let depth = 0;
  let j = start;
  let inStr = false, strCh = '';
  for (; j < source.length; j++) {
    const c = source[j];
    if (inStr) {
      if (c === '\\') { j++; continue; }
      if (c === strCh) inStr = false;
      continue;
    }
    if (c === '"' || c === "'") { inStr = true; strCh = c; continue; }
    if (c === '{') depth++;
    else if (c === '}') { depth--; if (depth === 0) break; }
  }
  return source.slice(start, j + 1);
}

const probe1 = '鍋子:"Cooking Pot"';
const objText = extractObjectContaining(src, probe1);
if (!objText) { console.error('dictionary not found'); process.exit(1); }
console.log('dict length:', objText.length);

// Evaluate it as JS
const vm = require('vm');
const sandbox = {};
const ctx = vm.createContext(sandbox);
let dict;
try {
  dict = vm.runInContext('(' + objText + ')', ctx);
} catch (e) {
  console.error('EVAL ERROR:', e.message);
  console.log(objText.slice(0, 500));
  process.exit(1);
}
console.log('dict entries:', Object.keys(dict).length);
fs.writeFileSync('gamertw_tc_en_dict.json', JSON.stringify(dict, null, 1));

// show sample keys
const keys = Object.keys(dict);
console.log('sample TC keys:', keys.slice(0, 40).join(' | '));
// shrine entries
const shrineKeys = keys.filter(k => k.includes('神廟'));
console.log('shrine entries:', shrineKeys.length);
console.log(shrineKeys.slice(0, 10).join(' | '));
