const INTERACTIVE_ROW_TARGET_SELECTOR = [
  'a[href]',
  'button',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="link"]',
].join(',')

export function isInteractiveRowTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(INTERACTIVE_ROW_TARGET_SELECTOR) != null
}
