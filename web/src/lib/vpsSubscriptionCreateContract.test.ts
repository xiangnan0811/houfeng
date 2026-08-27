import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import type { CreateVPSSubscriptionInput } from './types'

const REPOSITORY_ROOT = resolve(process.cwd(), '..')
const MANIFEST_PATH = resolve(
  REPOSITORY_ROOT,
  'internal/center/http/handlers/vps_subscription_create_fields.json',
)
const GO_SOURCE_PATH = resolve(
  REPOSITORY_ROOT,
  'internal/center/http/handlers/subscriptions.go',
)
const TS_SOURCE_PATH = resolve(process.cwd(), 'src/lib/types.ts')

type ExtraCollectionField = Extract<
  keyof CreateVPSSubscriptionInput,
  'vps_id' | 'status' | 'display_name' | 'cost_category' | 'labels' | 'trial_ends_at' | 'ends_at'
>
const _noCollectionFields: ExtraCollectionField extends never ? true : never = true

function parseManifest(): string[] {
  const fields = JSON.parse(readFileSync(MANIFEST_PATH, 'utf8')) as unknown
  if (!Array.isArray(fields) || fields.some((field) => typeof field !== 'string') || fields.length === 0) {
    throw new Error('vps subscription create manifest must be a non-empty string array')
  }
  return fields
}

function parseGoJSONTags(source: string, structName: string): string[] {
  const marker = `type ${structName} struct {`
  const start = source.indexOf(marker)
  if (start < 0) throw new Error(`${structName} not found`)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${structName} is not a flat struct`)
  return [...body.slice(0, end).matchAll(/json:"([^,"]+)/g)].map((match) => match[1] ?? '')
}

function parseTSTypeFields(source: string, typeName: string): string[] {
  const marker = `export type ${typeName} = {`
  const start = source.indexOf(marker)
  if (start < 0) throw new Error(`${typeName} not found`)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${typeName} is not a flat object type`)
  return body.slice(0, end).split('\n').flatMap((line) => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('//')) return []
    const name = trimmed.split(':')[0]?.trim().replace(/\?$/, '')
    return name ? [name] : []
  })
}

describe('CreateVPSSubscriptionInput', () => {
  it('matches the Go VPS-scoped request DTO through a shared manifest', () => {
    expect(_noCollectionFields).toBe(true)
    const manifest = parseManifest()
    const goFields = parseGoJSONTags(readFileSync(GO_SOURCE_PATH, 'utf8'), 'vpsSubscriptionCreateRequest')
    const tsFields = parseTSTypeFields(readFileSync(TS_SOURCE_PATH, 'utf8'), 'CreateVPSSubscriptionInput')
    expect(goFields).toEqual(manifest)
    expect(tsFields).toEqual(manifest)
  })
})
