import { MonoDigits, Timestamp } from '../../../../components/atoms'
import type { CommandAuditEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: CommandAuditEvidenceReadModel
}

export function CommandAuditEvidenceRenderer({ model }: Props) {
  return (
    <section className="page-panel evidence-renderer evidence-renderer--command" aria-label="命令审计证据">
      <header className="evidence-renderer__header">
        <h3>命令审计</h3>
        <span><MonoDigits>{model.audit_count}</MonoDigits> 条</span>
      </header>
      <ol className="evidence-renderer__timeline">
        {model.audits.map((audit) => (
          <li key={audit.audit_id}>
            <div>
              <strong>{audit.command_id}</strong>
              <span>{audit.event_type} · {audit.outcome} · {audit.source}</span>
              <span>{audit.exit_code === undefined ? '无退出码' : `退出码 ${audit.exit_code}`}</span>
            </div>
            <Timestamp value={audit.occurred_at} />
          </li>
        ))}
      </ol>
    </section>
  )
}
