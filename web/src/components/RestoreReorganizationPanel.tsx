import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { getVPSAsset, unlinkVPSMonitoringInstance } from '../lib/api'
import { ApiError } from '../lib/apiRequest'
import type { VPSAssetDetail, VPSMonitoringInstanceSummary } from '../lib/types'
import { Button } from './atoms'

type RestoreReorganizationPanelProps = {
  vpsId: string
  refreshGeneration?: number
  onEditUsage: () => void
  onEditDecision: () => void
  onRelink: () => void
  onCreateMonitoring: () => void
  onChanged: () => void
}

export function RestoreReorganizationPanel({
  vpsId,
  refreshGeneration = 0,
  onEditUsage,
  onEditDecision,
  onRelink,
  onCreateMonitoring,
  onChanged,
}: RestoreReorganizationPanelProps) {
  const [detail, setDetail] = useState<VPSAssetDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loadedId, setLoadedId] = useState<string | null>(null)
  const [pending, setPending] = useState<VPSMonitoringInstanceSummary | null>(null)
  const [unlinking, setUnlinking] = useState(false)
  // Unlink failures belong to the write; a detail read must not clear them.
  const [unlinkError, setUnlinkError] = useState<string | null>(null)
  const detailReadGenerationRef = useRef(0)
  const currentVpsIdRef = useRef(vpsId)
  const mountedRef = useRef(true)

  useLayoutEffect(() => {
    mountedRef.current = true
    currentVpsIdRef.current = vpsId
    return () => {
      mountedRef.current = false
    }
  }, [vpsId])

  useEffect(() => {
    const generation = ++detailReadGenerationRef.current
    const targetId = vpsId
    let live = true
    void getVPSAsset(targetId)
      .then((next) => {
        if (
          !live ||
          !mountedRef.current ||
          currentVpsIdRef.current !== targetId ||
          generation !== detailReadGenerationRef.current
        ) {
          return
        }
        setDetail(next)
        setError(null)
        setLoadedId(targetId)
      })
      .catch((caught: unknown) => {
        if (
          !live ||
          !mountedRef.current ||
          currentVpsIdRef.current !== targetId ||
          generation !== detailReadGenerationRef.current
        ) {
          return
        }
        const message = caught instanceof ApiError ? caught.message : '加载恢复后的关联失败'
        setError(message)
        setLoadedId(targetId)
      })
    return () => {
      live = false
    }
  }, [refreshGeneration, vpsId])

  const visible = loadedId === vpsId ? detail : null
  const visibleError = loadedId === vpsId ? error : null

  async function confirmUnlink() {
    if (!pending || unlinking) return
    const actionVpsId = vpsId
    const unlink = pending
    setUnlinking(true)
    setUnlinkError(null)
    try {
      await unlinkVPSMonitoringInstance(actionVpsId, {
        monitoring_instance_id: unlink.monitoring_instance_id,
        note: unlink.note,
      })
      if (!mountedRef.current || currentVpsIdRef.current !== actionVpsId) return

      setPending(null)
      onChanged()

      const generation = ++detailReadGenerationRef.current
      try {
        const next = await getVPSAsset(actionVpsId)
        if (
          !mountedRef.current ||
          currentVpsIdRef.current !== actionVpsId ||
          generation !== detailReadGenerationRef.current
        ) {
          return
        }
        setDetail(next)
        setError(null)
        setLoadedId(actionVpsId)
      } catch (caught: unknown) {
        if (
          !mountedRef.current ||
          currentVpsIdRef.current !== actionVpsId ||
          generation !== detailReadGenerationRef.current
        ) {
          return
        }
        setError(caught instanceof ApiError ? caught.message : '加载恢复后的关联失败')
        setLoadedId(actionVpsId)
      }
    } catch (caught: unknown) {
      if (!mountedRef.current || currentVpsIdRef.current !== actionVpsId) return
      setUnlinkError(caught instanceof ApiError ? caught.message : '解除监控实例关联失败')
    } finally {
      if (mountedRef.current && currentVpsIdRef.current === actionVpsId) {
        setUnlinking(false)
      }
    }
  }

  return (
    <section className="page-panel" aria-label="整理恢复记录">
      <h2>整理恢复记录</h2>
      <p>恢复后生命周期是闲置，续费决策保持原值，监控没有自动启用。旧关联仍在；解除后只写解除时间，失败的新建不会复活旧关联。</p>
      {visible?.archived_state_snapshot ? (
        <p>
          最近快照来自{visible.archived_state_snapshot.source === 'archive' ? '归档' : '迁移观察'}：
          {visible.archived_state_snapshot.lifecycle_status} / {visible.archived_state_snapshot.usage_status} / {visible.archived_state_snapshot.renewal_decision}
        </p>
      ) : null}
      <div className="page-form-actions">
        <Button type="button" variant="secondary" onClick={onEditUsage}>调整用途</Button>
        <Button type="button" variant="secondary" onClick={onEditDecision}>续费决策</Button>
        <Button type="button" variant="secondary" onClick={onRelink}>关联已有监控</Button>
        <Button type="button" onClick={onCreateMonitoring}>创建并接入</Button>
      </div>
      {visibleError ? <p role="alert">{visibleError}</p> : null}
      {!visible && !visibleError ? <p role="status">正在读取当前关联…</p> : null}
      {visible ? (
        <ul>
          {visible.monitoring_instance_links.length === 0 ? <li>当前没有未解除的监控关联。</li> : null}
          {visible.monitoring_instance_links.map((link) => (
            <li key={link.monitoring_instance_id}>
              <Link to={`/monitoring/${encodeURIComponent(link.monitoring_instance_id)}`}>{link.display_name}</Link>
              {' · '}
              {link.lifecycle_status} / {link.monitoring_status}
              {link.archived_at ? ` · 已归档 ${link.archived_at}` : ''}
              <Button type="button" variant="ghost" size="sm" disabled={unlinking} onClick={() => { setUnlinkError(null); setPending(link) }}>解除旧关联</Button>
            </li>
          ))}
        </ul>
      ) : null}
      {pending ? (
        <section role="alertdialog" aria-label="确认解除监控实例关联">
          <h3>确认解除监控实例关联</h3>
          <p>当前：{pending.display_name || pending.monitoring_instance_id} 仍关联到这台已恢复的 VPS。</p>
          <p>操作后：只写入解除时间。失败的新建不会复活这条旧关联。</p>
          {unlinkError ? <p role="alert">{unlinkError}</p> : null}
          <div className="page-form-actions">
            <Button type="button" variant="secondary" disabled={unlinking} onClick={() => { setUnlinkError(null); setPending(null) }}>取消</Button>
            <Button type="button" variant="danger" disabled={unlinking} onClick={() => void confirmUnlink()}>
              {unlinking ? '解除中…' : '确认解除关联'}
            </Button>
          </div>
        </section>
      ) : null}
      <p>真实接入后，再在监控实例详情显式恢复监控。这里不会自动恢复 token。</p>
    </section>
  )
}
