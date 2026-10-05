// captureSourceAllowed matches drivers.CaptureSourceAllowed. prefixes are
// the CLI words from GET /api/capture/platforms (Ethernet, Port-Channel).
// A source name starts with one of those words and then a digit.
// Subinterfaces (Ethernet2.210, or a row with parent_id) are not sources.

export function platformKey(entry) {
  const name = typeof entry === 'string' ? entry : entry?.platform
  return String(name ?? '')
    .trim()
    .toLowerCase()
}

export function platformPrefixes(platforms, platform) {
  const key = String(platform ?? '')
    .trim()
    .toLowerCase()
  const entry = (platforms ?? []).find((p) => platformKey(p) === key)
  if (!entry || typeof entry === 'string' || !Array.isArray(entry.interfaces)) return null
  return entry.interfaces
}

export function captureSourceAllowed(name, prefixes, { parentId } = {}) {
  if (!prefixes?.length) return false
  if (Number(parentId) > 0) return false
  const n = String(name ?? '')
  const dot = n.lastIndexOf('.')
  if (dot > 0 && /^\d+$/.test(n.slice(dot + 1))) return false
  return prefixes.some((prefix) => {
    if (!n.startsWith(prefix) || n.length <= prefix.length) return false
    const c = n.charCodeAt(prefix.length)
    return c >= 48 && c <= 57
  })
}
