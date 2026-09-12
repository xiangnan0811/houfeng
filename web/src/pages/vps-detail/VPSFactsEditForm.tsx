import { useId, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Input, Select } from '../../components/atoms'
import {
  CUSTOM_OPTION_VALUE,
  countryOptionsWithExisting,
  displayOption,
  normalizeCountry,
  optionSelectValue,
} from '../../lib/assetOptions'
import type { ProviderRecord, VPSUsageStatus } from '../../lib/types'
import type { FactEditFormState } from './types'
import { USAGE_OPTIONS } from './vpsDetailOptions'
import { VPSFormSection } from './VPSDetailDialog'

type VPSFactsEditFormProps = {
  formId: string
  draft: FactEditFormState
  providers: ProviderRecord[]
  providersLoading: boolean
  providersError: string | null
  submitting: boolean
  onDraftChange: (draft: FactEditFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSFactsEditForm({
  formId,
  draft,
  providers,
  providersLoading,
  providersError,
  submitting,
  onDraftChange,
  onSubmit,
}: VPSFactsEditFormProps) {
  const providerSelectId = useId()
  const noteId = useId()
  const [countryOptions] = useState(() => countryOptionsWithExisting([draft.country]))
  const [countrySelectValue, setCountrySelectValue] = useState(() => optionSelectValue(draft.country, countryOptions))
  const [customCountry, setCustomCountry] = useState(() => (optionSelectValue(draft.country, countryOptions) === CUSTOM_OPTION_VALUE ? draft.country : ''))
  const [ipv6Enabled, setIPv6Enabled] = useState(() => Boolean(draft.ipv6.trim()))
  const [sshHostDiffers, setSSHHostDiffers] = useState(() => Boolean(draft.sshHost.trim() && draft.sshHost.trim() !== draft.ipv4.trim()))

  function handleProviderChange(providerID: string) {
    const provider = providers.find((item) => item.provider_id === providerID)
    onDraftChange({
      ...draft,
      providerID,
      providerName: provider ? provider.name : draft.providerName,
    })
  }

  function updateIPv4(value: string) {
    onDraftChange({
      ...draft,
      ipv4: value,
      sshHost: sshHostDiffers ? draft.sshHost : value,
    })
  }

  function updateCountry(value: string) {
    setCountrySelectValue(value)
    if (value === CUSTOM_OPTION_VALUE) {
      onDraftChange({ ...draft, country: normalizeCountry(customCountry) })
      return
    }
    onDraftChange({ ...draft, country: normalizeCountry(value) })
  }

  function updateCustomCountry(value: string) {
    setCustomCountry(value)
    onDraftChange({ ...draft, country: normalizeCountry(value) })
  }

  function updateIPv6Enabled(checked: boolean) {
    setIPv6Enabled(checked)
    if (!checked) {
      onDraftChange({ ...draft, ipv6: '' })
    }
  }

  function updateSSHHostDiffers(checked: boolean) {
    setSSHHostDiffers(checked)
    onDraftChange({ ...draft, sshHost: checked ? draft.sshHost : draft.ipv4 })
  }

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit}>
      <VPSFormSection title="身份 / 服务商">
        <div className="vps-form-grid">
          <div className="vps-wide">
            <Input label="VPS 名称" value={draft.displayName} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, displayName: event.target.value })} />
          </div>
          <Input label="产品名" value={draft.productName} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, productName: event.target.value })} />
          <Input label="订单号" value={draft.orderRef} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, orderRef: event.target.value })} />
          <div className="vps-wide">
            <label className="input-field" htmlFor={providerSelectId}>
              <span className="input-field__label">资产服务商</span>
              <select
                id={providerSelectId}
                aria-label="资产服务商"
                className="input"
                value={draft.providerID}
                disabled={providersLoading || submitting}
                onChange={(event) => handleProviderChange(event.target.value)}
              >
                <option value="">未关联服务商</option>
                {providers.map((provider) => (
                  <option key={provider.provider_id} value={provider.provider_id}>
                    {provider.name} · {provider.country || '地区未填'} · {provider.provider_id}
                  </option>
                ))}
              </select>
              <span className="input-field__hint">
                {providersLoading
                  ? '正在读取服务商…'
                  : providersError
                    ? `服务商不可用：${providersError}`
                    : providers.length === 0
                      ? '还没有服务商主数据，请先创建或保留名称快照。'
                      : '选择服务商会同步更新名称快照，仍可手动修正快照。'}
                {' '}
                <Link className="text-link" to="/providers">服务商列表</Link>
              </span>
            </label>
          </div>
          <div className="vps-wide">
            <Input label="服务商名称快照" value={draft.providerName} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, providerName: event.target.value })} />
          </div>
        </div>
      </VPSFormSection>

      <VPSFormSection title="位置">
        <div className="vps-form-grid">
          <Select label="国家 / 地区" value={countrySelectValue} disabled={submitting} onChange={(event) => updateCountry(event.target.value)}>
            <option value="">未选择</option>
            {countryOptions.map((option) => (
              <option key={option.value} value={option.value}>{displayOption(option)}</option>
            ))}
            <option value={CUSTOM_OPTION_VALUE}>自定义 / 其他</option>
          </Select>
          {countrySelectValue === CUSTOM_OPTION_VALUE ? (
            <Input label="自定义国家 / 地区" value={customCountry} disabled={submitting} onChange={(event) => updateCustomCountry(event.target.value)} />
          ) : null}
          <Input label="区域" value={draft.region} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, region: event.target.value })} />
          <Input label="城市" value={draft.city} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, city: event.target.value })} />
          <Input label="数据中心" value={draft.datacenter} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, datacenter: event.target.value })} />
        </div>
      </VPSFormSection>

      <VPSFormSection title="连接 / 主机">
        <div className="vps-form-grid">
          <div className="vps-wide">
            <Input label="IPv4 / 主入口" value={draft.ipv4} disabled={submitting} onChange={(event) => updateIPv4(event.target.value)} />
          </div>
          <div className="vps-wide vps-inline">
            <label className="tg" htmlFor="vps-facts-ipv6-enabled">
              <input id="vps-facts-ipv6-enabled" type="checkbox" checked={ipv6Enabled} disabled={submitting} onChange={(event) => updateIPv6Enabled(event.target.checked)} />
              <span className="tg-track" />
              <span>启用 IPv6</span>
            </label>
            {ipv6Enabled ? (
              <Input label="IPv6 地址" value={draft.ipv6} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, ipv6: event.target.value })} />
            ) : null}
          </div>
          <div className="vps-wide vps-inline">
            <label className="tg" htmlFor="vps-facts-ssh-host-differs">
              <input id="vps-facts-ssh-host-differs" type="checkbox" checked={sshHostDiffers} disabled={submitting} onChange={(event) => updateSSHHostDiffers(event.target.checked)} />
              <span className="tg-track" />
              <span>单独填写</span>
            </label>
            <Input
              label="SSH Host"
              value={sshHostDiffers ? draft.sshHost : draft.ipv4}
              disabled={submitting || !sshHostDiffers}
              onChange={(event) => onDraftChange({ ...draft, sshHost: event.target.value })}
            />
            <div className="vps-short">
              <Input label="SSH 端口" type="number" min="1" max="65535" value={draft.sshPort} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, sshPort: event.target.value })} />
            </div>
          </div>
          <Input label="SSH 用户" value={draft.sshUser} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, sshUser: event.target.value })} />
          <Input label="操作系统" value={draft.osName} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, osName: event.target.value })} />
          <Input label="虚拟化" value={draft.virtualization} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, virtualization: event.target.value })} />
        </div>
      </VPSFormSection>

      <VPSFormSection title="用途 / 备注">
        <div className="vps-form-grid">
          <Select
            label="用途状态"
            aria-label="用途状态"
            value={draft.usageStatus}
            disabled={submitting}
            onChange={(event) => onDraftChange({
              ...draft,
              usageStatus: event.target.value as VPSUsageStatus,
            })}
          >
            {USAGE_OPTIONS.map(([value, label]) => (
              <option key={value} value={value}>{label}</option>
            ))}
          </Select>
          <Input label="重要性" value={draft.importance} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, importance: event.target.value })} />
          <div className="vps-wide">
            <Input label="标签" hint="用逗号分隔" value={draft.labels} disabled={submitting} onChange={(event) => onDraftChange({ ...draft, labels: event.target.value })} />
          </div>
          <div className="vps-wide">
            <label className="input-field" htmlFor={noteId}>
              <span className="input-field__label">备注</span>
              <div className="input-field__shell">
                <input
                  id={noteId}
                  className="input"
                  value={draft.note}
                  disabled={submitting}
                  onChange={(event) => onDraftChange({ ...draft, note: event.target.value })}
                />
              </div>
            </label>
          </div>
        </div>
      </VPSFormSection>
    </form>
  )
}
