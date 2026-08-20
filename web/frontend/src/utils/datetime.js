function pad(n) {
  return String(n).padStart(2, '0')
}

// YYYY-MM-DD HH:MM:SS, 24-hour, local time.
export function formatDateTime(value) {
  const d = new Date(value)
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  return `${date} ${time}`
}

// HH:MM:SS, 24-hour, local time.
export function formatTime(value) {
  const d = new Date(value)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
