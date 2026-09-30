import { formatDateTime } from './datetime.js'

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

// Local calendar day index. Avoids DST making a day 23 or 25 hours.
function localDayNumber(date) {
  return Math.floor(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / 86400000)
}

function plural(n, singular, pluralWord) {
  return `${n} ${n === 1 ? singular : pluralWord}`
}

// Age of an Oxidized backup. Under a year this is whole calendar days
// ("12 days"). At one year or more it is complete years plus leftover days
// ("1 year 12 days", "2 years 0 days").
export function formatOxidizedAge(value, now = new Date()) {
  if (!value || value === 'never') {
    return value === 'never' ? 'never' : '—'
  }
  const start = new Date(value)
  const end = new Date(now)
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || end < start) {
    return Number.isNaN(start.getTime()) ? '—' : '0 days'
  }

  let years = end.getFullYear() - start.getFullYear()
  const anniversary = new Date(start.getTime())
  anniversary.setFullYear(start.getFullYear() + years)
  if (anniversary > end) {
    years -= 1
    anniversary.setFullYear(start.getFullYear() + years)
  }

  if (years >= 1) {
    const days = localDayNumber(end) - localDayNumber(anniversary)
    return `${plural(years, 'year', 'years')} ${plural(Math.max(0, days), 'day', 'days')}`
  }
  const days = localDayNumber(end) - localDayNumber(start)
  return plural(Math.max(0, days), 'day', 'days')
}

export function apiError(err, fallback) {
  return err.response?.data?.error ?? fallback
}
