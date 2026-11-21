const tokens = require('./theme-tokens.json')
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{vue,js}'],
  theme: {
    extend: {
      colors: tokens.colors,
      fontFamily: {
        // 捆绑的 Calamity Sans（BOTW UI 复刻）优先，中文回退雅黑
        sans: tokens.sans,
        mono: ['"Cascadia Mono"', '"Cascadia Code"', 'Consolas', 'monospace']
      },
      borderRadius: {
        DEFAULT: '4px',
        lg: '6px',
        xl: '8px'
      }
    }
  },
  plugins: []
}
