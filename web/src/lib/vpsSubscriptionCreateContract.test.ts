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

type FieldContract = {
  name: string
  type: 'number' | 'string' | 'boolean' | 'date'
  required: boolean
  nullable: boolean
}

function parseManifest(): FieldContract[] {
  const fields = JSON.parse(readFileSync(MANIFEST_PATH, 'utf8')) as unknown
  if (!Array.isArray(fields) || fields.length === 0) {
    throw new Error('vps subscription create manifest must be a non-empty array')
  }
  return fields.map((field) => {
    if (
      typeof field !== 'object' ||
      field === null ||
      typeof (field as FieldContract).name !== 'string' ||
      typeof (field as FieldContract).type !== 'string' ||
      typeof (field as FieldContract).required !== 'boolean' ||
      typeof (field as FieldContract).nullable !== 'boolean'
    ) {
      throw new Error('vps subscription create manifest entries must include name, type, required, nullable')
    }
    return field as FieldContract
  })
}

function parseGoJSONTags(source: string, structName: string): Array<Pick<FieldContract, 'name' | 'type' | 'nullable'>> {
  const marker = `type ${structName} struct {`
  const start = source.indexOf(marker)
  if (start < 0) throw new Error(`${structName} not found`)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${structName} is not a flat struct`)
  return body.slice(0, end).split('\n').flatMap((line) => {
    const jsonMatch = line.match(/json:"([^,"]+)/)
    if (!jsonMatch?.[1]) return []
    const goType = line.trim().split(/\s+/)[1] ?? ''
    const nullable = goType.startsWith('*')
    return [{
      name: jsonMatch[1],
      type: goJSONTypeName(goType),
      nullable,
    }]
  })
}

function goJSONTypeName(goType: string): FieldContract['type'] {
  const named = goType.replace(/^\*/, '')
  if (named === 'float64' || named === 'int') return 'number'
  if (named === 'bool') return 'boolean'
  if (named === 'subscriptions.Date') return 'date'
  return 'string'
}

function parseTSTypeFields(source: string, typeName: string): FieldContract[] {
  const marker = `export type ${typeName} = {`
  const start = source.indexOf(marker)
  if (start < 0) throw new Error(`${typeName} not found`)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${typeName} is not a flat object type`)
  return body.slice(0, end).split('\n').flatMap((line) => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('//')) return []
    const [rawName, ...rest] = trimmed.split(':')
    if (!rawName || rest.length === 0) return []
    const required = !rawName.trim().endsWith('?')
    const name = rawName.trim().replace(/\?$/, '')
    const typeExpr = rest.join(':').replace(/;?\s*$/, '').trim()
    const nullable = /\bnull\b/.test(typeExpr)
    return [{
      name,
      type: tsJSONTypeName(typeExpr, nullable),
      required,
      nullable,
    }]
  })
}

function tsJSONTypeName(typeExpr: string, nullable: boolean): FieldContract['type'] {
  if (/\bnumber\b/.test(typeExpr)) return 'number'
  if (/\bboolean\b/.test(typeExpr)) return 'boolean'
  if (nullable && /\bstring\b/.test(typeExpr)) return 'date'
  return 'string'
}

describe('CreateVPSSubscriptionInput', () => {
  it('matches the Go VPS-scoped request DTO through a shared semantic manifest', () => {
    expect(_noCollectionFields).toBe(true)
    const manifest = parseManifest()
    const goFields = parseGoJSONTags(readFileSync(GO_SOURCE_PATH, 'utf8'), 'vpsSubscriptionCreateRequest')
    const tsFields = parseTSTypeFields(readFileSync(TS_SOURCE_PATH, 'utf8'), 'CreateVPSSubscriptionInput')
    expect(goFields.map((field) => field.name)).toEqual(manifest.map((field) => field.name))
    expect(tsFields.map((field) => field.name)).toEqual(manifest.map((field) => field.name))
    goFields.forEach((field, index) => {
      expect(field.type).toBe(manifest[index]?.type)
      expect(field.nullable).toBe(manifest[index]?.nullable)
    })
    expect(tsFields).toEqual(manifest)
  })

  it('fails closed on type, requiredness, and nullability drift', () => {
    const drifted = parseTSTypeFields(`export type Drifted = {
  price: string
  auto_renew: string
  renew_at?: string
  note?: string
}`, 'Drifted')
    expect(drifted[0]?.type).not.toBe('number')
    expect(drifted[1]?.type).not.toBe('boolean')
    expect(drifted[2]?.nullable).toBe(false)
    expect(drifted[3]?.required).toBe(false)
  })
})
