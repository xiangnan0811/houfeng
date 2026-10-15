// Release builds pin this URL to the checked-out source revision. Modified deployments
// must provide their own corresponding source URL, including local changes.
export function SourceLinks() {
  const sourceURL = import.meta.env.VITE_SOURCE_URL || 'https://github.com/xiangnan0811/houfeng'
  return (
    <span className="source-links">
      <a className="text-link" href={sourceURL} target="_blank" rel="noreferrer noopener" title="Houfeng 对应源码 · AGPL-3.0">源码</a>
      <span className="source-links__separator" aria-hidden="true"> · </span>
      <a className="text-link" href="/licenses.txt" target="_blank" rel="noreferrer noopener" title="AGPL-3.0 与第三方许可声明">许可</a>
    </span>
  )
}
