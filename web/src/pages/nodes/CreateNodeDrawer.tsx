import type { FormEvent } from 'react'

import { Button, Modal, Input } from '../../components/atoms'
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
    <Modal open={open} onClose={onClose} title="节点创建" ariaLabel="创建节点表单">
      <section className="node-create-drawer">
        <div className="node-create-drawer__intro">
          <p className="node-create-drawer__eyebrow">Agent onboarding</p>
          <h3>先登记服务器，再生成一键安装命令</h3>
          <p>创建完成后将进入节点详情页，并自动打开接入抽屉生成一键安装命令。</p>
        </div>

        <form className="node-create-drawer__form" onSubmit={onSubmit}>
          <fieldset className="node-create-drawer__group">
            <legend>身份与位置</legend>
            <Input
              label="显示名称"
              name="display_name"
              value={form.display_name}
              onChange={(event) => onFieldChange('display_name', event.target.value)}
              required
              autoComplete="off"
              placeholder="例如：Tokyo Edge"
            />
            <Input
              label="Group"
              name="group"
              value={form.group}
              onChange={(event) => onFieldChange('group', event.target.value)}
              autoComplete="off"
              placeholder="prod / edge / lab"
            />
            <Input
              label="地区"
              name="region"
              value={form.region}
              onChange={(event) => onFieldChange('region', event.target.value)}
              required
              autoComplete="off"
              placeholder="ap-northeast-1"
            />
            <Input
              label="城市"
              name="city"
              value={form.city}
              onChange={(event) => onFieldChange('city', event.target.value)}
              required
              autoComplete="off"
              placeholder="Tokyo"
            />
            <Input
              label="供应商"
              name="provider"
              value={form.provider}
              onChange={(event) => onFieldChange('provider', event.target.value)}
              required
              autoComplete="off"
              placeholder="Vultr / Hetzner / 自建"
            />
          </fieldset>

          <fieldset className="node-create-drawer__group node-create-drawer__group--wide">
            <legend>接入上下文</legend>
            <div className="node-create-drawer__status-card" aria-label="生命周期状态固定为待接入">
              <span>生命周期状态</span>
              <strong>待接入</strong>
              <p>候风会先登记 Node，随后在节点详情页的接入抽屉里发放短时一次性 enrollment token。</p>
            </div>
            <Input
              label="标签"
              name="labels"
              value={labelInput}
              onChange={(event) => onLabelInputChange(event.target.value)}
              autoComplete="off"
              hint="用逗号分隔，用于筛选和批量操作。"
              placeholder="prod, edge"
            />
            <label className="node-create-drawer__field node-create-drawer__field--wide">
              <span>备注</span>
              <textarea
                name="note"
                value={form.note}
                onChange={(event) => onFieldChange('note', event.target.value)}
                rows={4}
                placeholder="记录购买渠道、机房备注或接入注意事项"
              />
            </label>
          </fieldset>

          {error ? <p className="create-form__error" role="alert">{error}</p> : null}

          <div className="node-create-drawer__actions page-form-actions">
            <Button type="button" variant="secondary" disabled={submitting} onClick={onClose}>
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? '正在创建…' : '创建并接入'}
            </Button>
          </div>
        </form>
      </section>
    </Modal>
  )
}
