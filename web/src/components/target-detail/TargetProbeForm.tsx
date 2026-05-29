import type { FormEvent } from 'react'

import type { FrequencyTier, ProbeKind } from '../../lib/types'

export type ProbeCreateFormState = {
  probeKind: ProbeKind
  enabled: boolean
  frequencyTier: FrequencyTier
  timeoutSeconds: string
  port: string
  httpScheme: string
  httpPath: string
  httpMethod: 'GET' | 'HEAD'
  expectedStatusStart: string
  expectedStatusEnd: string
  tlsExpiryWarningDays: string
}

export type ProbeFormMode = { kind: 'create' } | { kind: 'edit'; probeItemId: string }

const PROBE_KIND_OPTIONS = [
  { value: 'tcp', label: 'TCP' },
  { value: 'http', label: 'HTTP' },
  { value: 'tls', label: 'TLS' },
] as const

const FREQUENCY_TIER_OPTIONS = [
  { value: '5s', label: '5 秒' },
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
] as const

type TargetProbeFormProps = {
  mode: ProbeFormMode
  form: ProbeCreateFormState
  submitting: boolean
  error: string | null
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onProbeKindChange: (probeKind: ProbeKind) => void
  onFieldChange: <K extends keyof ProbeCreateFormState>(
    field: K,
    value: ProbeCreateFormState[K],
  ) => void
}

export function TargetProbeForm({
  mode,
  form,
  submitting,
  error,
  onSubmit,
  onProbeKindChange,
  onFieldChange,
}: TargetProbeFormProps) {
  return (
    <section className="page-panel">
      <p className="page-panel__eyebrow">
        {mode.kind === 'edit' ? 'ProbeItem 编辑' : 'ProbeItem 创建'}
      </p>
      <h3 className="page-panel__title">
        {mode.kind === 'edit' ? '编辑 ProbeItem' : '创建 ProbeItem'}
      </h3>
      <form className="target-probe-drawer__form" onSubmit={onSubmit}>
        <label>
          <span>Probe 类型</span>
          <select
            name="probeKind"
            value={form.probeKind}
            onChange={(event) => onProbeKindChange(event.target.value as ProbeKind)}
          >
            {PROBE_KIND_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label className="target-probe-drawer__check">
          <input
            name="enabled"
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => onFieldChange('enabled', event.target.checked)}
          />
          <span>启用 ProbeItem</span>
        </label>
        <label>
          <span>频率档位</span>
          <select
            name="frequencyTier"
            value={form.frequencyTier}
            onChange={(event) =>
              onFieldChange('frequencyTier', event.target.value as FrequencyTier)
            }
          >
            {FREQUENCY_TIER_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>超时秒数</span>
          <input
            name="timeoutSeconds"
            inputMode="numeric"
            value={form.timeoutSeconds}
            onChange={(event) => onFieldChange('timeoutSeconds', event.target.value)}
          />
        </label>
        {form.probeKind !== 'http' ? (
          <label>
            <span>端口</span>
            <input
              name="port"
              inputMode="numeric"
              value={form.port}
              onChange={(event) => onFieldChange('port', event.target.value)}
            />
          </label>
        ) : null}
        {form.probeKind === 'http' ? (
          <>
            <label>
              <span>HTTP 协议</span>
              <select
                name="httpScheme"
                value={form.httpScheme}
                onChange={(event) => onFieldChange('httpScheme', event.target.value)}
              >
                <option value="http">http</option>
                <option value="https">https</option>
              </select>
            </label>
            <label>
              <span>HTTP 路径</span>
              <input
                name="httpPath"
                value={form.httpPath}
                onChange={(event) => onFieldChange('httpPath', event.target.value)}
              />
            </label>
            <label>
              <span>HTTP 方法</span>
              <select
                name="httpMethod"
                value={form.httpMethod}
                onChange={(event) =>
                  onFieldChange(
                    'httpMethod',
                    event.target.value as ProbeCreateFormState['httpMethod'],
                  )
                }
              >
                <option value="GET">GET</option>
                <option value="HEAD">HEAD</option>
              </select>
            </label>
            <label>
              <span>期望状态码起点</span>
              <input
                name="expectedStatusStart"
                inputMode="numeric"
                value={form.expectedStatusStart}
                onChange={(event) => onFieldChange('expectedStatusStart', event.target.value)}
              />
            </label>
            <label>
              <span>期望状态码终点</span>
              <input
                name="expectedStatusEnd"
                inputMode="numeric"
                value={form.expectedStatusEnd}
                onChange={(event) => onFieldChange('expectedStatusEnd', event.target.value)}
              />
            </label>
          </>
        ) : null}
        {form.probeKind === 'tls' ? (
          <label>
            <span>证书预警天数</span>
            <input
              name="tlsExpiryWarningDays"
              inputMode="numeric"
              value={form.tlsExpiryWarningDays}
              onChange={(event) => onFieldChange('tlsExpiryWarningDays', event.target.value)}
            />
          </label>
        ) : null}
        {error ? <p className="create-form__error">{error}</p> : null}
        <div className="page-form-actions">
          <button type="submit" className="btn md primary" disabled={submitting}>
            {submitting
              ? mode.kind === 'edit'
                ? '正在保存…'
                : '正在创建…'
              : mode.kind === 'edit'
                ? '保存 ProbeItem'
                : '创建 ProbeItem'}
          </button>
        </div>
      </form>
    </section>
  )
}
