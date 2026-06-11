export type MonitoringInstanceFilterState = {
  group: string | null
  region: string | null
  city: string | null
  provider: string | null
  lifecycle: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
  abnormal: boolean
  onboardingPending: boolean
}

export type MonitoringInstanceListView = 'all' | 'runtime-attention' | 'binding-conflict'

export type MonitoringInstanceQuickView = 'all' | 'abnormal' | 'onboarding' | 'runtime-attention' | 'binding-conflict'

export type MonitoringInstanceFilterOption = {
  value: string
  label: string
}
