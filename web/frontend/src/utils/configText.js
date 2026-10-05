// Line diff for the running-config editor. Marks are the lines in current
// that are not part of the longest common subsequence with original.

function linesOf(text) {
  if (text == null || text === '') return []
  const parts = String(text).replace(/\r\n/g, '\n').split('\n')
  if (parts.length && parts[parts.length - 1] === '') parts.pop()
  return parts
}

function lcsPairs(a, b) {
  const n = a.length
  const m = b.length
  const dp = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      if (a[i] === b[j]) dp[i][j] = dp[i + 1][j + 1] + 1
      else dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const pairs = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      pairs.push([i, j])
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) i++
    else j++
  }
  return pairs
}

export function normalizeConfigText(text) {
  const lines = linesOf(text).map((line) => line.trimEnd())
  return lines.length ? lines.join('\n') + '\n' : ''
}

// 1-based CodeMirror line numbers that were added or replaced.
export function changedLineNumbers(original, current) {
  const a = linesOf(original)
  const b = linesOf(current)
  const matched = new Set(lcsPairs(a, b).map((p) => p[1]))
  const out = []
  for (let j = 0; j < b.length; j++) {
    if (!matched.has(j)) out.push(j + 1)
  }
  return out
}

// Lines present in original that the edit dropped, ignoring a replacement
// of one line by another. A changed line is marked in the editor instead.
export function removedLineCount(original, current) {
  const a = linesOf(original)
  const b = linesOf(current)
  const shared = lcsPairs(a, b).length
  const removed = a.length - shared
  const added = b.length - shared
  return Math.max(0, removed - added)
}
