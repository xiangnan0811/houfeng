import { useEffect, useState } from 'react'

import { Button, Hostname, Timestamp } from '../../components/atoms'
import { listMonitoringInstancePhases } from '../../lib/api'
import type { MonitoringInstancePhase } from '../../lib/types'

export function MonitoringInstancePhases({ monitoringInstanceId, refreshKey = '' }: { monitoringInstanceId: string; refreshKey?: string }) {
  const [open, setOpen] = useState(false)
  const [revision, setRevision] = useState(0)
  const [state, setState] = useState<{ id: string; phases: MonitoringInstancePhase[] | null; error: string | null }>({ id: '', phases: null, error: null })
  useEffect(() => {
    if (!open) return
    let current = true
    void listMonitoringInstancePhases(monitoringInstanceId).then((phases) => {
      if (current) setState({ id: monitoringInstanceId, phases, error: null })
    }).catch(() => {
      if (current) setState({ id: monitoringInstanceId, phases: null, error: '接入阶段历史暂不可用' })
    })
    return () => { current = false }
  }, [monitoringInstanceId, open, revision, refreshKey])
  const phases = state.id === monitoringInstanceId ? state.phases : null
  const error = state.id === monitoringInstanceId ? state.error : null
  return (
    <section aria-label="接入阶段历史">
      <Button variant="ghost" aria-expanded={open} onClick={() => setOpen((value) => !value)}>接入阶段历史</Button>
      {open ? <div>
        {error ? <p role="alert">{error} <Button variant="ghost" onClick={() => setRevision((value) => value + 1)}>重试</Button></p>
          : phases === null ? <p role="status">正在加载接入阶段…</p>
            : phases.length === 0 ? <p>尚无接入会话</p>
              : <ul className="monitoring-detail-history__list">{phases.map((phase) => <li key={phase.session_id}>
                <dl className="monitoring-detail-binding-dialog__facts">
                  <div><dt>会话</dt><dd><Hostname>{phase.session_id}</Hostname></dd></div>
                  <div><dt>权限</dt><dd>{phase.capability === 'evidence_only' ? '仅在线证据' : phase.capability === 'full' ? '采集与命令' : phase.capability}</dd></div>
                  <div><dt>指纹摘要</dt><dd><Hostname>{phase.fingerprint_hash || '尚未确认'}</Hostname></dd></div>
                  <div><dt>开始</dt><dd><Timestamp value={phase.started_at} /></dd></div>
                  <div><dt>结束</dt><dd>{phase.ended_at ? <Timestamp value={phase.ended_at} /> : '进行中'}</dd></div>
                  <div><dt>最后可信在线</dt><dd>{phase.last_trusted_online_at ? <Timestamp value={phase.last_trusted_online_at} /> : '尚无'}</dd></div>
                  <div><dt>曾接入</dt><dd>{phase.ever_connected ? '是' : '否'}</dd></div>
                </dl>
              </li>)}</ul>}
      </div> : null}
    </section>
  )
}
