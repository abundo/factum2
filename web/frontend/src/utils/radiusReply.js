// Accept attributes are stored as one line per attribute, the same text the
// RADIUS worker encodes. A leading # drops the line. A # after the value is
// a comment and is not sent. This module is the grid on the Service tab.

function splitTrailingComment(line) {
  let inQuote = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (ch === '"') inQuote = !inQuote
    if (!inQuote && ch === '#' && (i === 0 || /\s/.test(line[i - 1]))) {
      return {
        code: line.slice(0, i).trim(),
        comment: line.slice(i + 1).trim(),
      }
    }
  }
  return { code: line.trim(), comment: '' }
}

function parseAttr(code) {
  const match = code.match(/^(.+?)\s*(:=|\+=|=)\s*(.*)$/)
  if (!match) return null
  const name = match[1].trim()
  let value = match[3].trim()
  if (!name || !value) return null
  let quoted = false
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
    quoted = true
    value = value.slice(1, -1)
  }
  if (!value) return null
  return { name, op: match[2], value, quoted }
}

function commentText(parts) {
  return parts
    .map((part) => part.trim())
    .filter(Boolean)
    .join('\n')
}

export function parseReply(text) {
  const rows = []
  let pending = []
  const pushComment = () => {
    const comment = commentText(pending)
    pending = []
    if (!comment) return
    rows.push({ enabled: false, name: '', op: '=', value: '', quoted: false, comment })
  }

  for (const raw of String(text ?? '').split('\n')) {
    const line = raw.trim()
    if (!line) {
      pushComment()
      continue
    }
    const commented = line.startsWith('#')
    const body = commented ? line.replace(/^#\s?/, '') : line
    const { code, comment } = splitTrailingComment(body)
    const attr = parseAttr(code)
    if (!attr) {
      pending.push(body)
      continue
    }
    const notes = [...pending]
    if (comment) notes.push(comment)
    pending = []
    rows.push({
      enabled: !commented,
      name: attr.name,
      op: attr.op,
      value: attr.value,
      quoted: attr.quoted,
      comment: commentText(notes),
    })
  }
  pushComment()
  return rows
}

function formatValue(row) {
  const value = row.value ?? ''
  if (row.quoted || /[\s"#]/.test(value)) {
    return `"${value}"`
  }
  return value
}

function commentLines(comment) {
  return String(comment ?? '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
}

export function serializeReply(rows) {
  const lines = []
  for (const row of rows ?? []) {
    const name = (row.name ?? '').trim()
    const value = row.value ?? ''
    const notes = commentLines(row.comment)
    if (!name && !value.trim() && notes.length === 0) continue
    if (!name) {
      for (const note of notes) lines.push(`# ${note}`)
      if (value.trim()) lines.push(`= ${formatValue(row)}`)
      lines.push('')
      continue
    }
    let line = `${name} ${row.op || '='} ${formatValue(row)}`
    if (!row.enabled) line = `# ${line}`
    if (notes.length) line += ` # ${notes.join(' ')}`
    lines.push(line)
  }
  return lines.join('\n').replace(/\n+$/, '')
}
