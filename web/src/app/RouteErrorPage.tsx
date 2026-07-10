import { useEffect } from 'react'
import { Link, useLocation, useNavigate, useRouteError } from 'react-router-dom'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'

type RouteErrorPageProps = {
  reloadPage?: () => void
}

function reloadWindow() {
  window.location.reload()
}

export function RouteErrorPage({ reloadPage = reloadWindow }: RouteErrorPageProps) {
  const error = useRouteError()
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    console.error('路由渲染失败', error)
  }, [error])

  function retryRoute() {
    navigate(`${location.pathname}${location.search}${location.hash}`, { replace: true })
  }

  return (
    <div className="app-recovery">
      <PageState
        kind="error"
        eyebrow="页面恢复"
        title="页面暂时无法显示"
        description="页面模块加载或渲染失败。重试无效时，请刷新页面。"
        className="app-recovery__surface"
        action={
          <>
            <Button onClick={retryRoute}>重试当前页面</Button>
            <Button variant="secondary" onClick={reloadPage}>刷新页面</Button>
            <Link className="btn md secondary" to="/">返回工作台</Link>
          </>
        }
      />
    </div>
  )
}
