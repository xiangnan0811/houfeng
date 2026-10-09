// 事实表单校验失败时把焦点移到对应字段：键盘与读屏用户不必从页首重新寻找出错位置。
// 字段可能收在“可选设置”里或被开关隐藏：先展开、打开开关，等重新渲染后再聚焦。
/** 表单收到该事件后只把对应分组设为可见（detail 为 data-revealed-by 的值）。 */
export const REVEAL_FIELD_EVENT = 'vps-facts:reveal-field'

const FIELD_FOR_ERROR: ReadonlyArray<readonly [RegExp, string]> = [
  [/VPS 名称/, 'VPS 名称'],
  [/IPv4/, 'IPv4'],
  [/IPv6/, 'IPv6 地址'],
  [/SSH 端口/, 'SSH 端口'],
  [/到期日/, 'VPS 到期日'],
]

function findField(form: HTMLElement, label: string): HTMLElement | null {
  for (const field of Array.from(form.querySelectorAll('label'))) {
    if (field.querySelector('.field__label')?.textContent?.trim() !== label) continue
    return field.querySelector<HTMLElement>('input, select, textarea')
  }
  return null
}

function revealAncestors(form: HTMLElement, control: HTMLElement): void {
  for (let node = control.parentElement; node && node !== form; node = node.parentElement) {
    if (node instanceof HTMLDetailsElement && !node.open) node.open = true
    const revealedBy = node.getAttribute('data-revealed-by')
    if (node.hidden && revealedBy) form.dispatchEvent(new CustomEvent(REVEAL_FIELD_EVENT, { detail: revealedBy }))
  }
}

export function focusFactFieldForError(formId: string, message: string): void {
  const form = document.getElementById(formId)
  const label = FIELD_FOR_ERROR.find(([pattern]) => pattern.test(message))?.[1]
  if (!form || !label) return
  const control = findField(form, label)
  if (!control) return
  revealAncestors(form, control)
  control.focus()
  // 打开开关会触发重新渲染；渲染完成后再聚焦一次，确保焦点落在已显示的字段上。
  requestAnimationFrame(() => findField(form, label)?.focus())
}
