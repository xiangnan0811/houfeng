import type { ConfigEnv, UserConfig } from 'vite'
import { describe, expect, it } from 'vitest'

import viteConfig from './vite.config'

const CSP_POLICY = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'"

async function resolvedConfig(command: ConfigEnv['command']): Promise<UserConfig> {
  if (typeof viteConfig !== 'function') return viteConfig
  return viteConfig({ command, mode: 'test', isSsrBuild: false, isPreview: command === 'serve' })
}

describe('Vite CSP contract', () => {
  it('serves the shared Center policy in dev and preview', async () => {
    const config = await resolvedConfig('serve')

    expect(config.server?.headers).toMatchObject({ 'Content-Security-Policy': CSP_POLICY })
    expect(config.preview?.headers).toMatchObject({ 'Content-Security-Policy': CSP_POLICY })
  })
})
