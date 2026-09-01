import { formatDateTime } from '@/utils/datetime'

export function nodeKey(node) {
  return node?.full_name || node?.name || ''
}

// Oxidized stores FQDNs; factum device names are often the short hostname.
export function nodeMatches(node, want) {
  if (!want) {
    return false
  }
  if (nodeKey(node) === want || node.name === want || node.ip === want) {
    return true
  }
  const wantHost = String(want).split('/').pop()
  if (node.name === wantHost) {
    return true
  }
  return (node.name || '').split('.')[0] === wantHost.split('.')[0]
}

export function statusColor(status) {
  switch ((status ?? '').toLowerCase()) {
    case 'success':
      return 'success'
    case 'never':
      return 'neutral'
    case 'no_connection':
    case 'fail':
      return 'error'
    default:
      return 'warning'
  }
}

export function formatOxidizedTime(value) {
  if (!value || value === 'never') {
    return value === 'never' ? 'never' : '—'
  }
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) {
    return value
  }
  return formatDateTime(d)
}

export function apiError(err, fallback) {
  return err.response?.data?.error ?? fallback
}
