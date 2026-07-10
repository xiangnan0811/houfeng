(function () {
  try {
    var preset = localStorage.getItem('houfeng.theme.preset')
    var mode = localStorage.getItem('houfeng.theme.mode')
    if (preset !== 'houfeng' && preset !== 'classic') preset = 'houfeng'
    if (mode !== 'dark' && mode !== 'light' && mode !== 'system') mode = 'dark'
    var scheme = mode === 'system'
      ? (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : mode
    var themeClass = preset === 'classic' && scheme === 'light'
      ? 'theme-houfeng-light'
      : 'theme-' + preset + '-' + scheme
    document.documentElement.classList.add(themeClass)
  } catch (_) {
    document.documentElement.classList.add('theme-houfeng-dark')
  }
})()
