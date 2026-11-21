// Naive UI 主题集中配置：唯一主题来源，App.vue 与各组件统一引用。
// 设计基调 = 希卡之石：青色荧光主色 + 深夜蓝底 + 琥珀警告（BotW UI 语汇）。
import { darkTheme, zhCN, enUS, dateZhCN, dateEnUS } from 'naive-ui'

export { darkTheme, zhCN, enUS, dateZhCN, dateEnUS }

// 应用色板（与 Tailwind theme.extend.colors 对应）
import tokens from '../theme-tokens.json'
const palette = tokens.colors

// 主色（希卡青）之上的深色文字 —— BotW 青光 UI 一贯的"亮底深字"
const SHEIKAH_INK = '#062229'

export const themeOverrides = {
  common: {
    fontFamily: tokens.sans.join(', '),
    bodyColor: palette.base,
    cardColor: palette.surface,
    modalColor: palette.surface,
    popoverColor: palette.surface,
    tableColor: palette.surface,
    inputColor: palette.base,
    actionColor: palette.surface,
    textColorBase: '#e6edf3',
    textColor1: palette.ink,
    textColor2: '#c9d1d9',
    textColor3: palette.muted,
    borderColor: palette.edge,
    dividerColor: palette.edge,
    fillColor: '#21262d',
    fillColorHover: '#292e36',
    primaryColor: palette.azure,
    primaryColorHover: '#47b1ee',
    primaryColorPressed: '#1e86cf',
    primaryColorSuppl: palette.azure,
    successColor: palette.stamina,
    successColorHover: '#5ce0ab',
    successColorPressed: '#2bbd83',
    successColorSuppl: palette.stamina,
    warningColor: palette.warningAccent,
    warningColorHover: '#f7b64d',
    warningColorPressed: '#d98e12',
    warningColorSuppl: palette.warningAccent,
    errorColor: palette.malice,
    errorColorHover: '#fb8f8f',
    errorColorPressed: '#ef5353',
    errorColorSuppl: palette.malice,
    borderRadius: '3px',
    borderRadiusSmall: '4px',
    heightSmall: '26px',
    heightMedium: '30px'
  },
  Button: {
    // 希卡青主按钮 = 亮底深字；琥珀警告钮 = 深字；其余类型保持浅色文字
    textColorPrimary: SHEIKAH_INK,
    textColorHoverPrimary: SHEIKAH_INK,
    textColorPressedPrimary: SHEIKAH_INK,
    textColorFocusPrimary: SHEIKAH_INK,
    textColorWarning: '#221503',
    textColorHoverWarning: '#221503',
    textColorPressedWarning: '#221503',
    textColorFocusWarning: '#221503'
  },
  Steps: {
    indicatorColor: palette.base,
    indicatorTextColor: '#8b949e',
    indicatorBorderColor: palette.edge,
    splitorColor: palette.edge,
    stepHeaderFontWeight: '600',
    indicatorSizeSmall: '20px',
    indicatorFontSizeSmall: '11px'
  }
}

// 语言跟随应用 i18n（'zh-CN' | 'en-US'）
export function naiveLocales(locale) {
  const zh = locale === 'zh-CN'
  return { locale: zh ? zhCN : enUS, dateLocale: zh ? dateZhCN : dateEnUS }
}
