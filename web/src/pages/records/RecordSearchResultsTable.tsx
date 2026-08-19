import { Link } from 'react-router-dom'

import { Badge, DataTable, Timestamp, type DataTableColumn } from '../../components/atoms'
import type { RecordDetail } from '../../lib/types'
import {
  RECORD_LIFECYCLE_LABELS,
  RECORD_SUBJECT_KIND_LABELS,
  RECORD_TYPE_LABELS,
} from './recordLabels'
import { BUSINESS_STATUS_LABELS } from './recordWorkspaceModel'

function primarySubjectLabel(record: RecordDetail): string {
  const subjects = record.current.subjects
  const primary = subjects.find((subject) => subject.primary) ?? subjects[0]
  if (!primary) return '—'
  const name = primary.identity.display_name || primary.source_id
  return `${RECORD_SUBJECT_KIND_LABELS[primary.kind]} · ${name}`
}

const COLUMNS: DataTableColumn<RecordDetail>[] = [
  {
    key: 'title',
    label: '标题',
    render: (record) => (
      <Link className="record-search-results__title" to={`/records/${record.record_id}`}>
        {record.current.title}
      </Link>
    ),
  },
  {
    key: 'type',
    label: '类型',
    render: (record) => RECORD_TYPE_LABELS[record.current.record_type],
  },
  {
    key: 'status',
    label: '状态',
    render: (record) => (record.current.business_status
      ? <Badge variant="state" tone="neutral">{BUSINESS_STATUS_LABELS[record.current.business_status]}</Badge>
      : '—'),
  },
  {
    key: 'subject',
    label: '主对象',
    render: primarySubjectLabel,
  },
  {
    key: 'tags',
    label: '标签',
    render: (record) => (record.current.tags.length ? record.current.tags.join('、') : '—'),
  },
  {
    key: 'occurred_at',
    label: '发生时间',
    render: (record) => (record.current.occurred_at
      ? <Timestamp value={record.current.occurred_at} />
      : '—'),
  },
  {
    key: 'updated_at',
    label: '更新时间',
    render: (record) => <Timestamp value={record.updated_at} />,
  },
  {
    key: 'lifecycle',
    label: '生命周期',
    render: (record) => RECORD_LIFECYCLE_LABELS[record.lifecycle],
  },
]

export function RecordSearchResultsTable({ rows }: { rows: RecordDetail[] }) {
  return (
    <DataTable
      className="record-search-results__table"
      caption="记录搜索结果"
      columns={COLUMNS}
      rows={rows}
      rowKey={(record) => record.record_id}
      density="compact"
    />
  )
}
