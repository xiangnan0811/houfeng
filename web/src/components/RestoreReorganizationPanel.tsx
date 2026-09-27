import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { getVPSAsset, listVPSMonitoringInstances } from '../lib/api'
import { ApiError } from '../lib/apiRequest'
import type { VPSAssetDetail } from '../lib/types'
import { Button } from './atoms'

type RestoreReorganizationPanelProps = {
  vpsId: string
  refreshGeneration?: number
  onEditUsage: () => void
  onEditDecision: () => void
  onCreateMonitoring: () => void
}

export function RestoreReorganizationPanel({
  vpsId,
  refreshGeneration = 0,
  onEditUsage,
  onEditDecision,
  onCreateMonitoring,
}: RestoreReorganizationPanelProps) {
  const [detail, setDetail] = useState<VPSAssetDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loadedId, setLoadedId] = useState<string | null>(null)
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
    void Promise.all([getVPSAsset(targetId), listVPSMonitoringInstances(targetId, 'all')])
      .then(([next, history]) => {
        if (
          !live ||
          !mountedRef.current ||
          currentVpsIdRef.current !== targetId ||
          generation !== detailReadGenerationRef.current
        ) {
          return
        }
        setDetail({ ...next, monitoring_instance_links: history.filter((instance) => !instance.vps_id || instance.vps_id === targetId) })
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

  const current = visible?.monitoring_instance_links.filter((instance) => instance.lifecycle_status !== '已退役' && instance.is_current !== false) ?? []
  const canEnroll = visible?.lifecycle_status === 'active' && !visibleError

  return (
    <section className="page-panel" aria-label="整理恢复记录">
      <h2>整理恢复记录</h2>
      <p>恢复后进入管理中，用途默认为闲置。续费意向保持原值，历史监控仍为已退役；服务关联、探测、命令及旧会话采集权限均不自动恢复。</p>
      {visible?.archived_state_snapshot ? (
        <p>
          最近快照来自{visible.archived_state_snapshot.source === 'archive' ? '归档' : '迁移观察'}：
          {visible.archived_state_snapshot.lifecycle_status} / {(visible.archived_state_snapshot.usage_tags ?? []).join('、')} / {visible.archived_state_snapshot.renewal_decision}
        </p>
      ) : null}
      <div className="page-form-actions">
        <Button type="button" variant="secondary" onClick={onEditUsage}>调整用途</Button>
        <Button type="button" variant="secondary" onClick={onEditDecision}>续费决策</Button>
        <Button type="button" disabled={!canEnroll} onClick={onCreateMonitoring}>{current.length > 0 ? '接入当前监控' : '创建并接入'}</Button>
      </div>
      {visibleError ? <p role="alert">{visibleError}</p> : null}
      {!visible && !visibleError ? <p role="status">正在读取当前关联…</p> : null}
      {visible ? (
        <ul>
          {visible.monitoring_instance_links.length === 0 ? <li>当前没有监控实例或接入历史。</li> : null}
          {visible.monitoring_instance_links.map((link) => (
            <li key={link.monitoring_instance_id}>
              <Link to={`/monitoring/${encodeURIComponent(link.monitoring_instance_id)}`}>{link.display_name}</Link>
              {' · '}
              {link.lifecycle_status} / {link.monitoring_status}
              {canEnroll && (link.lifecycle_status !== '已退役' || current.length === 0) ? (
                <Link to={`/monitoring/${encodeURIComponent(link.monitoring_instance_id)}?onboarding=1&return_vps=${encodeURIComponent(vpsId)}`}>
                  {link.lifecycle_status === '已退役' ? '重新接入历史实例' : '重新接入当前实例'}
                </Link>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      <p>重新接入沿用实例身份并创建新阶段；请核对新会话权限与采集控制。旧会话仍可提交最小在线证据。</p>
    </section>
  )
}
