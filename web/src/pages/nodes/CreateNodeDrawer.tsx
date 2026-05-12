import type { FormEvent } from 'react'

import { Drawer } from '../../components/atoms'
import type { CreateNodeInput } from '../../lib/types'

type CreateNodeDrawerProps = {
  open: boolean
  form: CreateNodeInput
  labelInput: string
  submitting: boolean
  error: string | null
  onClose: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onFieldChange: <K extends keyof CreateNodeInput>(field: K, value: CreateNodeInput[K]) => void
  onLabelInputChange: (value: string) => void
}

export function CreateNodeDrawer({
  open,
  form,
  labelInput,
  submitting,
  error,
  onClose,
  onSubmit,
  onFieldChange,
  onLabelInputChange,
}: CreateNodeDrawerProps) {
  return (
    <Drawer open={open} onClose={onClose} title="节点创建" ariaLabel="创建节点表单">
      <p className="page-panel__description">创建完成后将立即生成接入 Token，并跳转到节点接入准备页。</p>
      <form onSubmit={onSubmit}>
        <p>
          <label>
            显示名称
            <input
              name="display_name"
              value={form.display_name}
              onChange={(event) => onFieldChange('display_name', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>
            Group
            <input
              name="group"
              value={form.group}
              onChange={(event) => onFieldChange('group', event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            地区
            <input
              name="region"
              value={form.region}
              onChange={(event) => onFieldChange('region', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>
            城市
            <input
              name="city"
              value={form.city}
              onChange={(event) => onFieldChange('city', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>
            供应商
            <input
              name="provider"
              value={form.provider}
              onChange={(event) => onFieldChange('provider', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>生命周期状态固定为待接入</label>
        </p>
        <p>
          <label>
            标签
            <input
              name="labels"
              value={labelInput}
              onChange={(event) => onLabelInputChange(event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            备注
            <textarea
              name="note"
              value={form.note}
              onChange={(event) => onFieldChange('note', event.target.value)}
              rows={3}
            />
          </label>
        </p>
        {error ? <p>{error}</p> : null}
        <div className="page-form-actions">
          <button type="submit" className="btn btn--primary btn--md" disabled={submitting}>
            {submitting ? '正在创建…' : '创建并生成 Token'}
          </button>
        </div>
      </form>
    </Drawer>
  )
}
