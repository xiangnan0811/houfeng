import { PageState } from '../components/PageState'

type RouteModuleFallbackProps = {
  label: string
}

export function RouteModuleFallback({ label }: RouteModuleFallbackProps) {
  return (
    <PageState
      kind="loading"
      eyebrow="正在加载"
      title={label}
      description="正在读取页面模块…"
      className="route-module-fallback"
    />
  )
}
