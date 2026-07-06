export function vpsDetailPath(vpsID: string): string {
  return `/vps/${encodeURIComponent(vpsID)}`
}

export function vpsWorkbenchPath(vpsID: string, workbench: 'cancellation' | 'subscription'): string {
  return `${vpsDetailPath(vpsID)}?workbench=${workbench}`
}
