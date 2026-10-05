// bytes formats a byte count the way the packet capture status line shows it.
export function bytes(n) {
  const units = ['B', 'KB', 'MB', 'GB']
  let v = Number(n) || 0
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const digits = i === 0 || v >= 10 ? 0 : 1
  return `${v.toFixed(digits)} ${units[i]}`
}
