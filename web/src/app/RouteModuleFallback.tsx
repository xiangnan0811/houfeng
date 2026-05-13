type RouteModuleFallbackProps = {
  label: string
}

export function RouteModuleFallback({ label }: RouteModuleFallbackProps) {
  return (
    <section className="page-panel route-module-fallback" aria-live="polite">
      <p className="page-panel__eyebrow">正在加载</p>
      <h2 className="page-panel__title">{label}</h2>
      <p className="page-panel__description">正在读取页面模块…</p>
    </section>
  )
}
