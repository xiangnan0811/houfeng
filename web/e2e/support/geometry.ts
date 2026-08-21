import { expect, type Locator, type Page } from '@playwright/test'

export async function expectLocatorNotClipped(locator: Locator): Promise<void> {
  const geometry = await locator.evaluate((element) => {
    const htmlElement = element as HTMLElement
    const style = getComputedStyle(htmlElement)
    const rect = htmlElement.getBoundingClientRect()
    return {
      left: rect.left,
      right: rect.right,
      top: rect.top,
      bottom: rect.bottom,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      clippedX: htmlElement.scrollWidth > htmlElement.clientWidth + 1 && style.overflowX !== 'visible',
      clippedY: htmlElement.scrollHeight > htmlElement.clientHeight + 1 && style.overflowY !== 'visible',
      textOverflow: style.textOverflow,
      lineClamp: style.webkitLineClamp,
    }
  })

  expect(geometry.left).toBeGreaterThanOrEqual(-1)
  expect(geometry.right).toBeLessThanOrEqual(geometry.viewportWidth + 1)
  expect(geometry.top).toBeGreaterThanOrEqual(-1)
  expect(geometry.bottom).toBeLessThanOrEqual(geometry.viewportHeight + 1)
  expect(geometry.clippedX).toBe(false)
  expect(geometry.clippedY).toBe(false)
  expect(geometry.textOverflow).not.toBe('ellipsis')
  expect(geometry.lineClamp).toBe('none')
}

export async function expectMinTouchTarget(locator: Locator): Promise<void> {
  const box = await locator.boundingBox()
  if (!box) throw new Error('expected command to have a bounding box')
  expect(box.width).toBeGreaterThanOrEqual(44)
  expect(box.height).toBeGreaterThanOrEqual(44)
}

export async function expectNoDocumentOverflow(page: Page): Promise<void> {
  const geometry = await page.evaluate(() => ({
    viewportWidth: window.innerWidth,
    documentWidth: document.documentElement.scrollWidth,
    bodyWidth: document.body.scrollWidth,
  }))
  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewportWidth + 1)
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth + 1)
}
