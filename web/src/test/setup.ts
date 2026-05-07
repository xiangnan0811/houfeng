import '@testing-library/jest-dom'

function createMemoryStorage(): Storage {
  const items = new Map<string, string>()
  return {
    get length() {
      return items.size
    },
    clear() {
      items.clear()
    },
    getItem(key: string) {
      return items.get(key) ?? null
    },
    key(index: number) {
      return Array.from(items.keys())[index] ?? null
    },
    removeItem(key: string) {
      items.delete(key)
    },
    setItem(key: string, value: string) {
      items.set(key, value)
    },
  }
}

function exposeWindowStorage(name: 'localStorage' | 'sessionStorage') {
  const current = globalThis[name]
  if (
    typeof current?.getItem === 'function' &&
    typeof current?.setItem === 'function' &&
    typeof current?.removeItem === 'function' &&
    typeof current?.clear === 'function'
  ) {
    return
  }

  const storage = createMemoryStorage()
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: storage,
  })
  Object.defineProperty(window, name, {
    configurable: true,
    value: storage,
  })
}

exposeWindowStorage('localStorage')
exposeWindowStorage('sessionStorage')
