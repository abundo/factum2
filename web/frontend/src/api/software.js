import http from './http'

export function listSoftware(path = '/') {
  return http.get('/software/files', { params: { path } }).then((res) => res.data)
}

export function mkdirSoftware(path) {
  return http.post('/software/mkdir', { path }).then((res) => res.data)
}

export function moveSoftware(from, to) {
  return http.post('/software/move', { from, to }).then((res) => res.data)
}

export function deleteSoftware(path) {
  return http.delete('/software/files', { params: { path } }).then((res) => res.data)
}

export function uploadSoftware(path, file) {
  const data = new FormData()
  data.append('file', file)
  return http
    .put('/software/files', data, {
      params: { path },
      timeout: 30 * 60 * 1000,
    })
    .then((res) => res.data)
}

export function copySoftware(payload) {
  return http.post('/software/copy', payload, { timeout: 0 }).then((res) => res.data)
}

const downloadChunk = 8 * 1024 * 1024

export async function downloadSoftware(path, name) {
  const parts = []
  let offset = 0
  let total = 0
  for (;;) {
    const res = await http.get('/software/files/content', {
      params: { path, offset, limit: downloadChunk },
      responseType: 'arraybuffer',
      timeout: 0,
    })
    const size = Number(res.headers['x-storage-size'])
    if (Number.isFinite(size) && size >= 0) total = size
    const n = res.data?.byteLength ?? 0
    if (n) parts.push(res.data)
    offset += n
    if (n === 0 || n < downloadChunk || (total > 0 && offset >= total)) break
  }
  const blob = new Blob(parts)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name || 'download'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
