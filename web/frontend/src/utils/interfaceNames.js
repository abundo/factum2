// Expand interface names with numeric ranges, NetBox-style.
// Ethernet[1-48] → Ethernet1 … Ethernet48
// Ethernet1/[1-4] → Ethernet1/1 … Ethernet1/4
// Ethernet[1-2]/[1-2] → cartesian product
// Ethernet[1,3,5] and Ethernet[01-03] (zero-padded) are also supported.

export const MAX_INTERFACE_RANGE = 512

export class InterfaceNameError extends Error {
  constructor(message) {
    super(message)
    this.name = 'InterfaceNameError'
  }
}

export function expandInterfaceNames(pattern) {
  const input = (pattern ?? '').trim()
  if (!input) {
    throw new InterfaceNameError('Name is required')
  }
  const names = expandAll(input)
  if (names.length > MAX_INTERFACE_RANGE) {
    throw new InterfaceNameError(`Range expands to more than ${MAX_INTERFACE_RANGE} interfaces`)
  }
  return [...new Set(names)]
}

export function parseInterfaceNamePattern(pattern, { expandRanges = true } = {}) {
  const input = (pattern ?? '').trim()
  if (!input) return { names: [], error: 'Name is required' }
  if (!expandRanges) return { names: [input], error: '' }
  try {
    return { names: expandInterfaceNames(input), error: '' }
  } catch (err) {
    return { names: [], error: err.message || 'Invalid interface name' }
  }
}

function expandAll(s) {
  const open = (s.match(/\[/g) || []).length
  const close = (s.match(/\]/g) || []).length
  if (open !== close) {
    throw new InterfaceNameError('Unmatched brackets in interface name')
  }
  const m = /\[([^\]]*)\]/.exec(s)
  if (!m) {
    return [s]
  }
  const values = expandBracketContents(m[1])
  const out = []
  for (const v of values) {
    const next = s.slice(0, m.index) + v + s.slice(m.index + m[0].length)
    out.push(...expandAll(next))
    if (out.length > MAX_INTERFACE_RANGE) {
      throw new InterfaceNameError(`Range expands to more than ${MAX_INTERFACE_RANGE} interfaces`)
    }
  }
  return out
}

function expandBracketContents(inner) {
  const tokens = inner
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
  if (!tokens.length) {
    throw new InterfaceNameError('Empty [] in interface name')
  }
  const out = []
  for (const tok of tokens) {
    const range = /^(\d+)-(\d+)$/.exec(tok)
    if (range) {
      const startStr = range[1]
      const endStr = range[2]
      const start = Number(startStr)
      const end = Number(endStr)
      if (start > end) {
        throw new InterfaceNameError(`Range ${tok} is reversed`)
      }
      const pad = /^0\d/.test(startStr) ? startStr.length : 0
      for (let n = start; n <= end; n++) {
        out.push(pad ? String(n).padStart(pad, '0') : String(n))
      }
      continue
    }
    if (/^\d+$/.test(tok)) {
      out.push(tok)
      continue
    }
    throw new InterfaceNameError(`Invalid range "${tok}"`)
  }
  return out
}
