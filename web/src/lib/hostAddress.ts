// 与 center 的 ipidentity 解析口径一致：IPv4 只收点分四段、不允许前导零；IPv6 不允许 zone。

// Go strings.TrimSpace 的 Unicode 空白集合（unicode.IsSpace）。JS trim() 多含 U+FEFF、少 U+0085，不能直接用。
const GO_SPACE = '\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000'
const GO_TRIM_PATTERN = new RegExp(`^[${GO_SPACE}]+|[${GO_SPACE}]+$`, 'g')

export function trimHostAddress(value: string): string {
  return value.replace(GO_TRIM_PATTERN, '')
}

const IPV4_OCTET = '(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)'
const IPV4_PATTERN = new RegExp(`^${IPV4_OCTET}(?:\\.${IPV4_OCTET}){3}$`)

export function isValidIPv4(value: string): boolean {
  return IPV4_PATTERN.test(value)
}

export function isValidIPv6(value: string): boolean {
  if (!value.includes(':') || !/^[0-9a-fA-F:.]+$/.test(value)) return false
  try {
    new URL(`http://[${value}]/`)
    return true
  } catch {
    return false
  }
}
