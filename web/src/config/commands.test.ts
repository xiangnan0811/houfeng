import { describe, expect, it } from 'vitest'

import {
  COMMAND_LABELS,
  COMMAND_LIST,
  COMMAND_OPTIONS,
  commandSensitivity,
} from './commands'

describe('shared command metadata', () => {
  it('owns the complete backend command presentation contract once', () => {
    expect(COMMAND_LIST.map((command) => command.id)).toEqual([
      'df_h',
      'free_m',
      'uptime',
      'top_head',
      'journalctl_u',
      'systemctl_status',
      'dmesg_err',
      'docker_ps',
    ])
    expect(COMMAND_LABELS.systemctl_status).toBe('systemctl status')
    expect(commandSensitivity('docker_ps')).toBe('sensitive')
    expect(commandSensitivity('uptime')).toBe('standard')
    expect(commandSensitivity('unknown')).toBeUndefined()
    expect(COMMAND_OPTIONS[0]).toEqual({ value: '', label: '全部命令' })
    expect(COMMAND_OPTIONS).toContainEqual({ value: 'uptime', label: 'uptime' })
  })
})
