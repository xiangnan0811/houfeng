import { Link, useNavigate } from 'react-router-dom'

import { Button } from '../components/atoms/Button'
import { PageState } from '../components/PageState'

export function NotFoundPage() {
  const navigate = useNavigate()

  function goBack() {
    const index: unknown = window.history.state?.idx
    if (typeof index === 'number' && index > 0) {
      navigate(-1)
    } else {
      navigate('/', { replace: true })
    }
  }

  return (
    <PageState
      kind="notfound"
      title="没有这一页"
      description="这个地址没有对应的页面。你可以返回上一页，或从工作台继续。"
      action={
        <>
          <Link className="btn md primary" to="/">返回工作台</Link>
          <Button variant="secondary" onClick={goBack}>返回上一页</Button>
        </>
      }
    />
  )
}
