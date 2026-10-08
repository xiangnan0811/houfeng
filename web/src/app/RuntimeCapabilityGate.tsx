import { useState, type ReactNode } from 'react'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { useAuth } from '../lib/auth-context'

type GateKind = 'records' | 'comparison'

export function RuntimeCapabilityGate({ kind, children }: { kind: GateKind; children: ReactNode }) {
  const { user, loading, status, error, retry } = useAuth()
  if (loading || status === 'loading') return null
  if (status === 'error') return <CapabilityReadError message={error} onRetry={retry} />
  if (!user || status !== 'ready') return null
  const records = user.runtime_capabilities.records === true
  const comparison = records && user.runtime_capabilities.comparison === true
  if (!records) return <FeatureOff title="记录平台未启用" description="当前环境没有启用记录平台，相关页面不会加载数据。" />
  if (kind === 'comparison' && !comparison) {
    return <FeatureOff title="比较功能关闭" description="横向比较当前关闭，比较页面不会加载数据。" />
  }
  return children
}

export function CapabilityReadError({
  message,
  onRetry,
}: {
  message: string | null
  onRetry: () => Promise<void>
}) {
  const [pending, setPending] = useState(false)
  return (
    <PageState
      kind="error"
      eyebrow="运行能力"
      title="能力读取错误"
      description="暂时无法读取当前账号的运行能力。可以重试，当前会话不会因此退出。"
      technicalSummary={message}
      action={(
        <Button
          type="button"
          size="sm"
          disabled={pending}
          onClick={() => {
            setPending(true)
            void onRetry().finally(() => setPending(false))
          }}
        >
          {pending ? '正在重试' : '重试'}
        </Button>
      )}
    />
  )
}

function FeatureOff({ title, description }: { title: string; description: string }) {
  return <PageState kind="empty" title={title} description={description} />
}
