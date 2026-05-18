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
      <form onSubmit={onSubmit}>
        <p>
          <label>
            Probe 类型
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
        </p>
        <p>
          <label>
            <input
              name="enabled"
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => onFieldChange('enabled', event.target.checked)}
            />
            启用 ProbeItem
          </label>
        </p>
        <p>
          <label>
            频率档位
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
        </p>
        <p>
          <label>
            超时秒数
            <input
              name="timeoutSeconds"
              inputMode="numeric"
              value={form.timeoutSeconds}
              onChange={(event) => onFieldChange('timeoutSeconds', event.target.value)}
            />
          </label>
        </p>
        {form.probeKind !== 'http' ? (
          <p>
            <label>
              端口
              <input
                name="port"
                inputMode="numeric"
                value={form.port}
                onChange={(event) => onFieldChange('port', event.target.value)}
              />
            </label>
          </p>
        ) : null}
        {form.probeKind === 'http' ? (
          <>
            <p>
              <label>
                HTTP 协议
                <select
                  name="httpScheme"
                  value={form.httpScheme}
                  onChange={(event) => onFieldChange('httpScheme', event.target.value)}
                >
                  <option value="http">http</option>
                  <option value="https">https</option>
                </select>
              </label>
            </p>
            <p>
              <label>
                HTTP 路径
                <input
                  name="httpPath"
                  value={form.httpPath}
                  onChange={(event) => onFieldChange('httpPath', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                HTTP 方法
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
            </p>
            <p>
              <label>
                期望状态码起点
                <input
                  name="expectedStatusStart"
                  inputMode="numeric"
                  value={form.expectedStatusStart}
                  onChange={(event) => onFieldChange('expectedStatusStart', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                期望状态码终点
                <input
                  name="expectedStatusEnd"
                  inputMode="numeric"
                  value={form.expectedStatusEnd}
                  onChange={(event) => onFieldChange('expectedStatusEnd', event.target.value)}
                />
              </label>
            </p>
          </>
        ) : null}
        {form.probeKind === 'tls' ? (
          <p>
            <label>
              证书预警天数
              <input
                name="tlsExpiryWarningDays"
                inputMode="numeric"
                value={form.tlsExpiryWarningDays}
                onChange={(event) => onFieldChange('tlsExpiryWarningDays', event.target.value)}
              />
            </label>
          </p>
        ) : null}
        {error ? <p>{error}</p> : null}
        <div className="page-form-actions">
          <button type="submit" className="btn btn--primary btn--md" disabled={submitting}>
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
