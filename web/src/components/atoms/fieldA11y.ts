export function mergeAriaTokens(...values: Array<string | undefined>): string | undefined {
  const tokens = values.flatMap((value) => value?.split(/\s+/).filter(Boolean) ?? [])
  const merged = [...new Set(tokens)]
  return merged.length > 0 ? merged.join(' ') : undefined
}
