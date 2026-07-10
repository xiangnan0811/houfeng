import { Component, type ErrorInfo, type ReactNode } from 'react'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'

type AppErrorBoundaryProps = {
  children: ReactNode
  reloadPage?: () => void
}

type AppErrorBoundaryState = {
  hasError: boolean
}

function reloadWindow() {
  window.location.reload()
}

export class AppErrorBoundary extends Component<
  AppErrorBoundaryProps,
  AppErrorBoundaryState
> {
  state: AppErrorBoundaryState = { hasError: false }

  static getDerivedStateFromError(): AppErrorBoundaryState {
    return { hasError: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('应用渲染失败', error, errorInfo)
  }

  private retryRender = () => {
    this.setState({ hasError: false })
  }

  private reloadPage = () => {
    (this.props.reloadPage ?? reloadWindow)()
  }

  render() {
    if (!this.state.hasError) return this.props.children

    return (
      <div className="app-recovery">
        <PageState
          kind="error"
          eyebrow="应用恢复"
          title="应用暂时无法继续"
          description="界面渲染遇到异常。你可以先重试；如果问题持续，请刷新页面。"
          className="app-recovery__surface"
          action={
            <>
              <Button onClick={this.retryRender}>重试渲染</Button>
              <Button variant="secondary" onClick={this.reloadPage}>刷新页面</Button>
              <a className="btn md secondary" href="/">返回工作台</a>
            </>
          }
        />
      </div>
    )
  }
}
