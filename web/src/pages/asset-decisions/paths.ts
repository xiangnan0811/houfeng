export function vpsDetailPath(vpsID: string): string {
  return `/vps/${encodeURIComponent(vpsID)}`
}

export function vpsWorkbenchPath(vpsID: string, workbench: 'archive' | 'subscription'): string {
  return `${vpsDetailPath(vpsID)}?workbench=${workbench}`
}
