import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import type { CreateVPSSubscriptionInput } from './types'

const TEST_DIRECTORY = dirname(fileURLToPath(import.meta.url))
const REPOSITORY_ROOT = resolve(TEST_DIRECTORY, '../../..')
const MANIFEST_PATH = resolve(
  REPOSITORY_ROOT,
  'internal/center/http/handlers/vps_subscription_create_fields.json',
)
const GO_SOURCE_PATH = resolve(
  REPOSITORY_ROOT,
  'internal/center/http/handlers/subscriptions.go',
)
const TS_SOURCE_PATH = resolve(TEST_DIRECTORY, 'types.ts')

type ExtraCollectionField = Extract<
  keyof CreateVPSSubscriptionInput,
  'vps_id' | 'status' | 'display_name' | 'cost_category' | 'labels' | 'trial_ends_at' | 'ends_at'
>
const _noCollectionFields: ExtraCollectionField extends never ? true : never = true

type FieldContract = {
  name: string
  type: 'number' | 'string' | 'boolean'
  format?: 'date'
  required: boolean
  nullable: boolean
}

const APPROVED_STRING_ALIAS_NAMES = ['BillingPeriodUnit', 'RenewalMode'] as const
const ISO_DATE_ALIAS_NAME = 'ISODate'

function parseManifest(): FieldContract[] {
  const fields = JSON.parse(readFileSync(MANIFEST_PATH, 'utf8')) as unknown
  return parseManifestFields(fields)
}

function parseManifestFields(fields: unknown): FieldContract[] {
  if (!Array.isArray(fields) || fields.length === 0) {
    throw new Error('vps subscription create manifest must be a non-empty array')
  }
  return fields.map((field) => {
    const format = typeof field === 'object' && field !== null && Object.prototype.hasOwnProperty.call(field, 'format')
      ? (field as FieldContract).format
      : undefined
    if (
      typeof field !== 'object' ||
      field === null ||
      typeof (field as FieldContract).name !== 'string' ||
      !['number', 'string', 'boolean'].includes((field as FieldContract).type) ||
      (format !== undefined && format !== 'date') ||
      (format === 'date' && (field as FieldContract).type !== 'string') ||
      typeof (field as FieldContract).required !== 'boolean' ||
      typeof (field as FieldContract).nullable !== 'boolean'
    ) {
      throw new Error('vps subscription create manifest entries must include valid type, optional format, required, nullable semantics')
    }
    return field as FieldContract
  })
}

function parseGoJSONTags(source: string, structName: string): FieldContract[] {
  const marker = `type ${structName} struct {`
  const start = findUniqueLiveDeclarationStart(source, marker, structName, false)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${structName} is not a flat struct`)
  return body.slice(0, end).split('\n').flatMap((line) => {
    const trimmed = stripGoCommentsOutsideRawTags(line).trim()
    if (!trimmed || trimmed.startsWith('//')) return []
    const jsonTag = goStructTagValue(trimmed, 'json')
    const declaration = trimmed.replace(/\s+`[^`]*`\s*$/, '').trim()
    const declarationParts = declaration.split(/\s+/)
    if (declarationParts.length === 1) {
      if (jsonTag === '-') return []
      throw new Error(`Anonymous Go JSON field is not supported: ${JSON.stringify(declaration)}`)
    }
    const [fieldName = '', goType = ''] = declarationParts
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(fieldName) || !goType) {
      throw new Error(`Unsupported Go struct field declaration: ${JSON.stringify(trimmed)}`)
    }
    if (!/^[A-Z]/.test(fieldName)) return []

    if (jsonTag === undefined) {
      throw new Error(`Exported Go JSON field must declare a usable json tag: ${fieldName}`)
    }
    if (jsonTag === '-') return []
    const [name] = jsonTag.split(',')
    if (!name) {
      throw new Error(`Exported Go JSON field must declare a usable json tag: ${fieldName}`)
    }

    const nullable = goType.startsWith('*') || goType === 'subscriptions.OptionalDate'
    const typeContract = goJSONTypeContract(goType)
    return [{
      name,
      ...typeContract,
      required: goStructTagValue(trimmed, 'required') === 'true',
      nullable,
    }]
  })
}

function goStructTagValue(line: string, key: string): string | undefined {
  const tagMatch = line.match(/\s+`([^`]*)`\s*$/)
  const tagBody = tagMatch?.[1]
  if (tagBody === undefined) return undefined
  const escapedKey = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const valueMatch = tagBody.match(
    new RegExp(`(?:^|[\\t ])${escapedKey}:"((?:[^"\\\\]|\\\\.)*)"(?:$|[\\t ])`),
  )
  return valueMatch?.[1]
}

function stripGoCommentsOutsideRawTags(line: string): string {
  let stripped = ''
  let inRawString = false
  for (let index = 0; index < line.length; index += 1) {
    if (line[index] === '`') {
      inRawString = !inRawString
      stripped += line[index]
      continue
    }
    if (!inRawString && line[index] === '/' && line[index + 1] === '/') {
      return stripped
    }
    if (!inRawString && line[index] === '/' && line[index + 1] === '*') {
      const commentEnd = line.indexOf('*/', index + 2)
      if (commentEnd < 0) {
        throw new Error(`Unterminated inline Go block comment: ${JSON.stringify(line.trim())}`)
      }
      stripped += ' '
      index = commentEnd + 1
      continue
    }
    stripped += line[index]
  }
  return stripped
}

function goJSONTypeName(goType: string): FieldContract['type'] {
  const named = goType.replace(/^\*/, '')
  if (
    named === 'float64' ||
    named === 'int' ||
    named === 'subscriptions.OptionalFloat' ||
    named === 'subscriptions.OptionalInt'
  ) return 'number'
  if (named === 'bool' || named === 'subscriptions.OptionalBool') return 'boolean'
  if (named === 'subscriptions.Date' || named === 'subscriptions.OptionalDate') return 'string'
  if (named === 'string' || named === 'subscriptions.OptionalString') return 'string'
  throw new Error(`Unsupported Go JSON field type: ${JSON.stringify(goType)}`)
}

function goJSONTypeContract(goType: string): Pick<FieldContract, 'type' | 'format'> {
  const type = goJSONTypeName(goType)
  const named = goType.replace(/^\*/, '')
  return named === 'subscriptions.Date' || named === 'subscriptions.OptionalDate'
    ? { type, format: 'date' }
    : { type }
}

function parseTSTypeFields(source: string, typeName: string): FieldContract[] {
  const marker = `export type ${typeName} = {`
  const start = findUniqueLiveDeclarationStart(source, marker, typeName, true)
  const body = source.slice(start + marker.length)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error(`${typeName} is not a flat object type`)
  const suffixStart = end + 2
  const nextLine = body.indexOf('\n', suffixStart)
  const suffix = body.slice(suffixStart, nextLine < 0 ? body.length : nextLine)
  if (!/^[\t ]*;?[\t ]*\r?$/.test(suffix)) {
    throw new Error(`${typeName} has unsupported declaration suffix: ${JSON.stringify(suffix.trim())}`)
  }
  if (suffix.trim() !== ';' && nextLine >= 0 && startsWithTypeContinuationAfterTrivia(body.slice(nextLine + 1))) {
    throw new Error(`${typeName} has unsupported declaration suffix after its closing brace`)
  }
  const stringAliases = verifiedStringLiteralAliases(source)
  const dateAliases = verifiedISODateAliases(source)
  return body.slice(0, end).split('\n').flatMap((line) => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('//')) return []
    const [rawName, ...rest] = trimmed.split(':')
    if (!rawName || rest.length === 0) return []
    const required = !rawName.trim().endsWith('?')
    const name = rawName.trim().replace(/\?$/, '')
    const typeExpr = rest.join(':').replace(/;?\s*$/, '').trim()
    const classification = classifyTSTypeUnion(typeExpr, stringAliases, dateAliases)
    return [{
      name,
      ...classification,
      required,
    }]
  })
}

function findUniqueLiveDeclarationStart(
  source: string,
  marker: string,
  declarationName: string,
  backtickEscapes: boolean,
): number {
  let rawCount = 0
  for (let offset = source.indexOf(marker); offset >= 0; offset = source.indexOf(marker, offset + marker.length)) {
    rawCount += 1
  }
  if (rawCount > 1) {
    throw new Error(`${declarationName} declared more than once`)
  }

  const liveStarts: number[] = []
  let state: 'code' | 'line-comment' | 'block-comment' | 'single-quote' | 'double-quote' | 'backtick' = 'code'
  let escaped = false
  let lineStart = 0
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index]
    const nextCharacter = source[index + 1]
    if (character === '\n') lineStart = index + 1

    if (state === 'line-comment') {
      if (character === '\n') state = 'code'
      continue
    }
    if (state === 'block-comment') {
      if (character === '*' && nextCharacter === '/') {
        state = 'code'
        index += 1
      }
      continue
    }
    if (state !== 'code') {
      const quote = state === 'single-quote' ? "'" : state === 'double-quote' ? '"' : '`'
      if (state === 'backtick' && !backtickEscapes) {
        if (character === quote) state = 'code'
      } else if (escaped) {
        escaped = false
      } else if (character === '\\') {
        escaped = true
      } else if (character === quote) {
        state = 'code'
      }
      continue
    }

    if (character === '/' && nextCharacter === '/') {
      state = 'line-comment'
      index += 1
    } else if (character === '/' && nextCharacter === '*') {
      state = 'block-comment'
      index += 1
    } else if (character === "'") {
      state = 'single-quote'
    } else if (character === '"') {
      state = 'double-quote'
    } else if (character === '`') {
      state = 'backtick'
    } else if (
      source.startsWith(marker, index) &&
      /^[\t ]*$/.test(source.slice(lineStart, index))
    ) {
      liveStarts.push(index)
      index += marker.length - 1
    }
  }

  if (rawCount !== 1 || liveStarts.length !== 1) {
    throw new Error(`${declarationName} not found`)
  }
  return liveStarts[0] ?? -1
}

function verifiedStringLiteralAliases(source: string): ReadonlySet<string> {
  const aliases = new Set<string>()
  for (const aliasName of APPROVED_STRING_ALIAS_NAMES) {
    const marker = `export type ${aliasName} =`
    if (!source.includes(marker)) continue
    const start = findUniqueLiveDeclarationStart(source, marker, aliasName, true)
    const definitionStart = start + marker.length
    const lineEnd = source.indexOf('\n', definitionStart)
    if (lineEnd >= 0 && startsWithTypeContinuationAfterTrivia(source.slice(lineEnd + 1))) {
      continue
    }
    const definition = source
      .slice(definitionStart, lineEnd < 0 ? source.length : lineEnd)
      .replace(/[\t ]*;[\t ]*\r?$/, '')
      .trim()
    const members = splitTypeScriptUnionMembers(definition)?.map((member) => member.trim())
    if (members && members.length > 0 && members.every(isNonEmptyTypeScriptStringLiteral)) {
      aliases.add(aliasName)
    }
  }
  return aliases
}

function verifiedISODateAliases(source: string): ReadonlySet<string> {
  const aliases = new Set<string>()
  const marker = `export type ${ISO_DATE_ALIAS_NAME} =`
  if (!source.includes(marker)) return aliases
  const start = findUniqueLiveDeclarationStart(source, marker, ISO_DATE_ALIAS_NAME, true)
  const definitionStart = start + marker.length
  const lineEnd = source.indexOf('\n', definitionStart)
  if (lineEnd >= 0 && startsWithTypeContinuationAfterTrivia(source.slice(lineEnd + 1))) {
    return aliases
  }
  const definition = source
    .slice(definitionStart, lineEnd < 0 ? source.length : lineEnd)
    .replace(/[\t ]*;[\t ]*\r?$/, '')
    .trim()
  if (definition === 'string') aliases.add(ISO_DATE_ALIAS_NAME)
  return aliases
}

function startsWithTypeContinuationAfterTrivia(source: string): boolean {
  for (let index = 0; index < source.length;) {
    const character = source[index]
    const nextCharacter = source[index + 1]
    if (character !== undefined && /\s/.test(character)) {
      index += 1
      continue
    }
    if (character === '/' && nextCharacter === '/') {
      const lineEnd = source.indexOf('\n', index + 2)
      if (lineEnd < 0) return false
      index = lineEnd + 1
      continue
    }
    if (character === '/' && nextCharacter === '*') {
      const commentEnd = source.indexOf('*/', index + 2)
      if (commentEnd < 0) return true
      index = commentEnd + 2
      continue
    }
    return character === '&' || character === '|'
  }
  return false
}

function splitTypeScriptUnionMembers(expression: string): string[] | undefined {
  const members: string[] = []
  let memberStart = 0
  let quote: "'" | '"' | undefined
  let escaped = false
  for (let index = 0; index < expression.length; index += 1) {
    const character = expression[index]
    if (quote !== undefined) {
      if (escaped) {
        escaped = false
      } else if (character === '\\') {
        escaped = true
      } else if (character === quote) {
        quote = undefined
      }
      continue
    }
    if (character === "'" || character === '"') {
      quote = character
    } else if (character === '|') {
      members.push(expression.slice(memberStart, index))
      memberStart = index + 1
    }
  }
  if (quote !== undefined || escaped) return undefined
  members.push(expression.slice(memberStart))
  return members
}

function isNonEmptyTypeScriptStringLiteral(member: string): boolean {
  return /^(?:'(?:[^'\\]|\\.)+'|"(?:[^"\\]|\\.)+")$/.test(member)
}

function classifyTSTypeUnion(
  typeExpr: string,
  stringAliases: ReadonlySet<string> = new Set(),
  dateAliases: ReadonlySet<string> = new Set(),
): Pick<FieldContract, 'type' | 'format' | 'nullable'> {
  const primitiveKinds = new Set<'number' | 'string' | 'boolean'>()
  let nullable = false
  let hasDateAlias = false
  let hasOrdinaryString = false

  for (const rawMember of typeExpr.split('|')) {
    const member = rawMember.trim()
    if (!member) {
      throw new Error(`TypeScript type expression has an empty union member: ${JSON.stringify(typeExpr)}`)
    }
    switch (member) {
      case 'null':
        nullable = true
        break
      case 'number':
        primitiveKinds.add('number')
        break
      case 'boolean':
        primitiveKinds.add('boolean')
        break
      case 'string':
        primitiveKinds.add('string')
        hasOrdinaryString = true
        break
      default:
        if (dateAliases.has(member)) {
          primitiveKinds.add('string')
          hasDateAlias = true
          break
        }
        if (stringAliases.has(member)) {
          primitiveKinds.add('string')
          hasOrdinaryString = true
          break
        }
        throw new Error(`Unsupported TypeScript union member: ${JSON.stringify(member)}`)
    }
  }

  if (primitiveKinds.size !== 1) {
    throw new Error(`TypeScript type expression must contain exactly one primitive kind: ${JSON.stringify(typeExpr)}`)
  }
  const primitiveKind = [...primitiveKinds][0]
  if (!primitiveKind) {
    throw new Error(`TypeScript type expression has no primitive kind: ${JSON.stringify(typeExpr)}`)
  }
  if (hasDateAlias && hasOrdinaryString) {
    throw new Error(`TypeScript date alias must not be mixed with an ordinary string: ${JSON.stringify(typeExpr)}`)
  }
  return hasDateAlias
    ? { type: 'string', format: 'date', nullable }
    : { type: primitiveKind, nullable }
}

describe('CreateVPSSubscriptionInput', () => {
  it('matches the Go VPS-scoped request DTO through a shared semantic manifest', () => {
    expect(_noCollectionFields).toBe(true)
    const manifest = parseManifest()
    const goFields = parseGoJSONTags(readFileSync(GO_SOURCE_PATH, 'utf8'), 'vpsSubscriptionCreateRequest')
    const tsFields = parseTSTypeFields(readFileSync(TS_SOURCE_PATH, 'utf8'), 'CreateVPSSubscriptionInput')
    expect(goFields.map((field) => field.name)).toEqual(manifest.map((field) => field.name))
    expect(tsFields.map((field) => field.name)).toEqual(manifest.map((field) => field.name))
    expect(goFields).toEqual(manifest)
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

  it('keeps an ordinary nullable string unformatted', () => {
    expect(parseTSTypeFields(`export type Sample = {
  value: string | null
}`, 'Sample')).toEqual([{
      name: 'value',
      type: 'string',
      required: true,
      nullable: true,
    }])
  })

  it('maps only an exact same-source ISODate alias to string format date', () => {
    expect(parseTSTypeFields(`export type ISODate = string
export type Sample = {
  value: ISODate | null
}`, 'Sample')).toEqual([{
      name: 'value',
      type: 'string',
      format: 'date',
      required: true,
      nullable: true,
    }])
  })

  it('expresses manifest date format independently from nullability', () => {
    const manifest = parseManifest()
    expect(manifest.find((field) => field.name === 'renew_at')).toEqual({
      name: 'renew_at',
      type: 'string',
      format: 'date',
      required: false,
      nullable: true,
    })
    expect(manifest.find((field) => field.name === 'note')).toEqual({
      name: 'note',
      type: 'string',
      required: true,
      nullable: false,
    })
  })

  for (const { name, field } of [
    { name: 'a missing type', field: { name: 'value', required: true, nullable: false } },
    { name: 'a null type', field: { name: 'value', type: null, required: true, nullable: false } },
    { name: 'an unknown type', field: { name: 'value', type: 'date', required: true, nullable: false } },
    { name: 'a null format', field: { name: 'value', type: 'string', format: null, required: true, nullable: false } },
    { name: 'an unknown format', field: { name: 'value', type: 'string', format: 'datetime', required: true, nullable: false } },
  ]) {
    it(`rejects manifest entries with ${name}`, () => {
      expect(() => parseManifestFields([field])).toThrow('vps subscription create manifest')
    })
  }

  it('rejects unknown Go field types instead of guessing string', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  Value subscriptions.UnknownString \`json:"value"\`
}`, 'Sample')).toThrow('Unsupported Go JSON field type')
  })

  for (const { name, declaration } of [
    { name: 'a missing json tag', declaration: 'Value string' },
    { name: 'an empty json tag', declaration: 'Value string `json:""`' },
    { name: 'an empty json name with options', declaration: 'Value string `json:",omitempty"`' },
  ]) {
    it(`rejects exported Go fields with ${name}`, () => {
      expect(() => parseGoJSONTags(`type Sample struct {
  ${declaration}
}`, 'Sample')).toThrow('Exported Go JSON field must declare a usable json tag')
    })
  }

  it('does not treat near-miss Go struct tag keys as json or required', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  Value string \`notjson:"value"\`
}`, 'Sample')).toThrow('Exported Go JSON field must declare a usable json tag')

    expect(parseGoJSONTags(`type Sample struct {
  Value string \`json:"value" notrequired:"true"\`
}`, 'Sample')).toEqual([{
      name: 'value',
      type: 'string',
      required: false,
      nullable: false,
    }])
  })

  it('only ignores an explicit Go json dash field', () => {
    expect(parseGoJSONTags(`type Sample struct {
  Ignored string \`json:"-"\`
  NotIgnored string \`json:"-,omitempty"\`
  Visible string \`json:"visible"\`
}`, 'Sample')).toEqual([{
      name: '-',
      type: 'string',
      required: false,
      nullable: false,
    }, {
      name: 'visible',
      type: 'string',
      required: false,
      nullable: false,
    }])
  })

  for (const { name, declaration } of [
    { name: 'an unexported embedded value without a json tag', declaration: 'embeddedFields `required:"true"`' },
    { name: 'an unexported embedded value with a named tag', declaration: 'embeddedFields `json:"embedded"`' },
    { name: 'an unexported embedded pointer with a named tag', declaration: '*embeddedFields `json:"embedded"`' },
    { name: 'an embedded value whose dash tag also has options', declaration: 'embeddedFields `json:"-,omitempty"`' },
    { name: 'an exported embedded value with a named tag', declaration: 'EmbeddedFields `json:"embedded"`' },
  ]) {
    it(`rejects ${name} instead of omitting an encoding/json-visible surface`, () => {
      expect(() => parseGoJSONTags(`type Sample struct {
  ${declaration}
}`, 'Sample')).toThrow('Anonymous Go JSON field is not supported')
    })
  }

  it('ignores an anonymous Go field only for an exact json dash tag', () => {
    expect(parseGoJSONTags(`type Sample struct {
  embeddedFields \`json:"-"\`
  EmbeddedFields \`json:"-"\`
  Visible string \`json:"visible"\`
}`, 'Sample')).toEqual([{
      name: 'visible',
      type: 'string',
      required: false,
      nullable: false,
    }])
  })

  for (const { name, typeExpr, want } of [
    { name: 'number', typeExpr: 'number', want: { type: 'number', nullable: false } },
    { name: 'boolean', typeExpr: 'boolean', want: { type: 'boolean', nullable: false } },
    { name: 'string', typeExpr: 'string', want: { type: 'string', nullable: false } },
    { name: 'nullable string', typeExpr: 'string | null', want: { type: 'string', nullable: true } },
    { name: 'reordered nullable string', typeExpr: 'null | string', want: { type: 'string', nullable: true } },
  ] as const) {
    it(`classifies supported ${name} members exactly`, () => {
      expect(classifyTSTypeUnion(typeExpr)).toEqual(want)
    })
  }

  for (const { name, typeExpr, error } of [
    { name: 'a mixed primitive union', typeExpr: 'number | string', error: 'exactly one primitive kind' },
    { name: 'a mixed boolean union', typeExpr: 'boolean | string', error: 'exactly one primitive kind' },
    { name: 'an undefined member', typeExpr: 'string | undefined', error: 'Unsupported TypeScript union member' },
    { name: 'an unknown member', typeExpr: 'string | UnknownAlias', error: 'Unsupported TypeScript union member' },
    { name: 'an empty member', typeExpr: 'string |', error: 'empty union member' },
    { name: 'a union without a primitive', typeExpr: 'null', error: 'exactly one primitive kind' },
  ]) {
    it(`rejects ${name} instead of guessing a manifest type`, () => {
      expect(() => parseTSTypeFields(`export type Drifted = {
  value: ${typeExpr}
}`, 'Drifted')).toThrow(error)
    })
  }

  it('classifies approved aliases only from same-source string-literal definitions', () => {
    expect(parseTSTypeFields(`export type BillingPeriodUnit = 'day' | 'month'
export type RenewalMode = 'auto' | 'manual'
export type Sample = {
  billing_period_unit?: BillingPeriodUnit | string
  renewal_mode?: string | RenewalMode
}`, 'Sample')).toEqual([
      {
        name: 'billing_period_unit',
        type: 'string',
        required: false,
        nullable: false,
      },
      {
        name: 'renewal_mode',
        type: 'string',
        required: false,
        nullable: false,
      },
    ])
  })

  it('accepts non-empty alias literals containing escapes and an embedded pipe', () => {
    expect(parseTSTypeFields(String.raw`export type BillingPeriodUnit = 'day|night' | 'owner\'s' | "quote\"" | 'back\\slash'
export type Sample = {
  billing_period_unit?: BillingPeriodUnit | string
}`, 'Sample')).toEqual([{
      name: 'billing_period_unit',
      type: 'string',
      required: false,
      nullable: false,
    }])
  })

  for (const { name, source, error } of [
    {
      name: 'a missing alias definition',
      source: `export type Sample = {
  value?: BillingPeriodUnit | string
}`,
      error: 'Unsupported TypeScript union member',
    },
    {
      name: 'duplicate alias definitions',
      source: `export type BillingPeriodUnit = 'day'
export type BillingPeriodUnit = 'month'
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
      error: 'BillingPeriodUnit declared more than once',
    },
  ]) {
    it(`rejects ${name}`, () => {
      expect(() => parseTSTypeFields(source, 'Sample')).toThrow(error)
    })
  }

  for (const { name, source, error } of [
    {
      name: 'a missing ISODate definition',
      source: `export type Sample = {
  value: ISODate | null
}`,
      error: 'Unsupported TypeScript union member',
    },
    {
      name: 'a widened ISODate definition',
      source: `export type ISODate = string | number
export type Sample = {
  value: ISODate | null
}`,
      error: 'Unsupported TypeScript union member',
    },
    {
      name: 'a nullable ISODate definition',
      source: `export type ISODate = string | null
export type Sample = {
  value: ISODate | null
}`,
      error: 'Unsupported TypeScript union member',
    },
    {
      name: 'an ISODate field widened with raw string',
      source: `export type ISODate = string
export type Sample = {
  value: ISODate | string | null
}`,
      error: 'date alias must not be mixed',
    },
  ]) {
    it(`rejects ${name}`, () => {
      expect(() => parseTSTypeFields(source, 'Sample')).toThrow(error)
    })
  }

  for (const { name, source, error } of [
    {
      name: 'an alias marker that exists only inside a block comment',
      source: `/*
export type BillingPeriodUnit = 'day'
*/
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
      error: 'BillingPeriodUnit not found',
    },
    {
      name: 'a block-comment shadow plus a live alias definition',
      source: `/*
export type BillingPeriodUnit = 'day'
*/
export type BillingPeriodUnit = 'month'
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
      error: 'BillingPeriodUnit declared more than once',
    },
  ]) {
    it(`rejects ${name}`, () => {
      expect(() => parseTSTypeFields(source, 'Sample')).toThrow(error)
    })
  }

  for (const { name, aliasName, definition } of [
    { name: 'BillingPeriodUnit widened with number', aliasName: 'BillingPeriodUnit', definition: "'day' | number" },
    { name: 'BillingPeriodUnit widened with undefined', aliasName: 'BillingPeriodUnit', definition: "'day' | undefined" },
    { name: 'RenewalMode widened with number', aliasName: 'RenewalMode', definition: "'auto' | number" },
    { name: 'RenewalMode widened with undefined', aliasName: 'RenewalMode', definition: "'auto' | undefined" },
    { name: 'an alias with an unknown member', aliasName: 'BillingPeriodUnit', definition: "'day' | OtherUnit" },
    { name: 'an alias with an empty member', aliasName: 'RenewalMode', definition: "'auto' |" },
    { name: 'an alias with an empty string literal', aliasName: 'BillingPeriodUnit', definition: "'day' | ''" },
  ]) {
    it(`rejects ${name} instead of trusting the alias name`, () => {
      expect(() => parseTSTypeFields(`export type ${aliasName} = ${definition}
export type Sample = {
  value?: ${aliasName} | string
}`, 'Sample')).toThrow('Unsupported TypeScript union member')
    })
  }

  for (const { name, aliasName, firstMember, widenedMember } of [
    {
      name: 'BillingPeriodUnit widened with number on a continuation line',
      aliasName: 'BillingPeriodUnit',
      firstMember: "'day'",
      widenedMember: 'number',
    },
    {
      name: 'BillingPeriodUnit widened with undefined on a continuation line',
      aliasName: 'BillingPeriodUnit',
      firstMember: "'day'",
      widenedMember: 'undefined',
    },
    {
      name: 'RenewalMode widened with number on a continuation line',
      aliasName: 'RenewalMode',
      firstMember: "'auto'",
      widenedMember: 'number',
    },
    {
      name: 'RenewalMode widened with undefined on a continuation line',
      aliasName: 'RenewalMode',
      firstMember: "'auto'",
      widenedMember: 'undefined',
    },
  ]) {
    it(`rejects ${name} instead of validating only the first line`, () => {
      expect(() => parseTSTypeFields(`export type ${aliasName} = ${firstMember}
  | ${widenedMember}
export type Sample = {
  value?: ${aliasName} | string
}`, 'Sample')).toThrow('Unsupported TypeScript union member')
    })
  }

  for (const { name, aliasName, firstMember, trivia, widenedMember } of [
    {
      name: 'BillingPeriodUnit widened after line-comment trivia',
      aliasName: 'BillingPeriodUnit',
      firstMember: "'day'",
      trivia: '  // keep the union continuation hidden behind trivia\n',
      widenedMember: 'number',
    },
    {
      name: 'RenewalMode widened after multiline block-comment trivia',
      aliasName: 'RenewalMode',
      firstMember: "'auto'",
      trivia: '  /* keep looking\n     across this comment */\n',
      widenedMember: 'undefined',
    },
  ]) {
    it(`rejects ${name}`, () => {
      expect(() => parseTSTypeFields(`export type ${aliasName} = ${firstMember}
${trivia}  | ${widenedMember}
export type Sample = {
  value?: ${aliasName} | string
}`, 'Sample')).toThrow('Unsupported TypeScript union member')
    })
  }

  it('rejects an object type intersection after the first closing brace', () => {
    expect(() => parseTSTypeFields(`export type Sample = {
  value: string
} & { debug?: string }`, 'Sample')).toThrow('unsupported declaration suffix')
  })

  it('accepts one optional object-type semicolon before a following declaration', () => {
    expect(parseTSTypeFields(`export type Sample = {
  value: string
};
export type Following = { debug?: string }`, 'Sample')).toEqual([{
      name: 'value',
      type: 'string',
      required: true,
      nullable: false,
    }])
  })

  it('rejects an object type continuation on a later line after the closing brace', () => {
    expect(() => parseTSTypeFields(`export type Sample = {
  value: string
}
& { debug?: string }`, 'Sample')).toThrow('unsupported declaration suffix')
  })

  for (const { name, trivia } of [
    {
      name: 'line-comment trivia',
      trivia: '  // the union continuation belongs to Sample\n',
    },
    {
      name: 'multiline block-comment trivia',
      trivia: '  /* keep looking\n     across this comment */\n',
    },
  ]) {
    it(`rejects an object union after ${name}`, () => {
      expect(() => parseTSTypeFields(`export type Sample = {
  value: string
}

${trivia}  | { debug?: string }`, 'Sample')).toThrow('unsupported declaration suffix')
    })
  }

  it('rejects commented shadow declaration markers instead of parsing the first substring', () => {
    expect(() => parseTSTypeFields(`/*
export type Sample = {
  value: string
}
*/
export type Sample = {
  value: number
}`, 'Sample')).toThrow('declared more than once')

    expect(() => parseGoJSONTags(`/*
type Sample struct {
  Value string \`json:"value"\`
}
*/
type Sample struct {
  Value int \`json:"value"\`
}`, 'Sample')).toThrow('declared more than once')
  })

  it('rejects a declaration marker that exists only inside a block comment', () => {
    expect(() => parseTSTypeFields(`/*
export type Sample = {
  value: string
}
*/`, 'Sample')).toThrow('Sample not found')

    expect(() => parseGoJSONTags(`/*
type Sample struct {
  Value string \`json:"value"\`
}
*/`, 'Sample')).toThrow('Sample not found')
  })

  it('accepts a CRLF object-type semicolon before a following declaration', () => {
    const source = `export type Sample = {
  value: string
};
export type Following = { debug?: string }`.replaceAll('\n', '\r\n')
    expect(parseTSTypeFields(source, 'Sample')).toEqual([{
      name: 'value',
      type: 'string',
      required: true,
      nullable: false,
    }])
  })

  it('rejects a trailing-comment anonymous Go embedding instead of skipping it as unexported', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  embeddedFields // encoding/json can promote exported members
}`, 'Sample')).toThrow('Anonymous Go JSON field is not supported')
  })

  it('rejects an inline-block-comment anonymous Go embedding instead of skipping it as unexported', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  embeddedFields /* promoted fields */
}`, 'Sample')).toThrow('Anonymous Go JSON field is not supported')
  })

  it('strips only true Go comments and preserves comment markers inside raw struct tags', () => {
    expect(parseGoJSONTags(`type Sample struct {
  EmbeddedFields \`note:"https://example.test/* embedded */" json:"-"\` // exact dash stays ignored
  Visible /* field type follows */ string \`note:"https://example.test/* visible */" json:"visible"\` // true trailing comment
}`, 'Sample')).toEqual([{
      name: 'visible',
      type: 'string',
      required: false,
      nullable: false,
    }])
  })

  it('fails closed on an unterminated Go block comment outside a raw struct tag', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  embeddedFields /* promoted fields
}`, 'Sample')).toThrow('Unterminated inline Go block comment')
  })

  it('fails closed on a Go block comment that spans source lines', () => {
    expect(() => parseGoJSONTags(`type Sample struct {
  embeddedFields /* promoted fields
     continue here */
}`, 'Sample')).toThrow('Unterminated inline Go block comment')
  })
})
