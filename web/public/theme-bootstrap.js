(function () {
  try {
    var preset = localStorage.getItem('houfeng.theme.preset')
    var mode = localStorage.getItem('houfeng.theme.mode')
    if (preset !== 'houfeng' && preset !== 'precision' && preset !== 'observatory') preset = 'houfeng'
    if (mode !== 'dark' && mode !== 'light' && mode !== 'system') mode = 'dark'
    // 与 preferredScheme 一致：没有 matchMedia 时按深色处理。
    var scheme = mode === 'system'
      ? (!window.matchMedia || window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : mode
    // 与 src/lib/theme.ts 的 themeClass 保持一致：观测台仅深色，浅色回退候风浅色。
    var themeClass = preset === 'observatory' && scheme === 'light'
      ? 'theme-houfeng-light'
      : 'theme-' + preset + '-' + scheme
    document.documentElement.classList.add(themeClass)
  } catch (_) {
    document.documentElement.classList.add('theme-houfeng-dark')
  }
})()
