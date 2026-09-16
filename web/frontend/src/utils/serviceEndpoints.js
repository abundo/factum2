export function applySchemaDefaults(schema, values = {}) {
  const out = { ...values }
  for (const f of schema ?? []) {
    if (!f?.name) continue
    if (!fieldEmpty(f, out[f.name])) continue
    if (fieldEmpty(f, f.default)) continue
    out[f.name] = f.default
  }
  return out
}

export function emptyServiceEndpoint(extra = {}, schema = []) {
  return {
    role: 'interface',
    device_id: extra.device_id ?? null,
    interface_id: extra.interface_id ?? null,
    fields: applySchemaDefaults(schema, extra.fields),
    label: extra.label ?? '',
  }
}

export function isDraftEndpoint(ep) {
  return !ep?.device_id || !ep?.interface_id
}

export function reshapeEndpoints(spec, current = [], draft = null) {
  const min = spec?.min ?? 0
  const max = spec?.max ?? 0
  const schema = spec?.fields ?? []
  let next = [...(current ?? [])]
  if (!next.length || next.every(isDraftEndpoint)) {
    next = []
    for (let i = 0; i < min; i++) next.push(emptyServiceEndpoint({}, schema))
    if (draft?.device_id && draft?.interface_id) {
      const slot = emptyServiceEndpoint(draft, schema)
      if (next.length) next[0] = { ...next[0], ...slot }
      else next.push(slot)
    }
    return next
  }
  while (next.length < min) next.push(emptyServiceEndpoint({}, schema))
  if (max > 0 && next.length > max) next = next.slice(0, max)
  return next
}

export function fieldEmpty(field, v) {
  if (v === null || v === undefined || v === '') return true
  if (field?.type === 'service_id' && Number(v) === 0) return true
  return false
}

export function schemaMissingRequired(schema, values) {
  return (schema ?? []).some(
    (f) => f.required && fieldEmpty(f, values?.[f.name]) && fieldEmpty(f, f.default),
  )
}

export function endpointsReady(spec, endpoints) {
  const min = spec?.min ?? 0
  const list = endpoints ?? []
  if (list.some((ep) => !ep.device_id || !ep.interface_id)) return false
  if (list.length < min) return false
  const fields = spec?.fields ?? []
  if (fields.length) {
    for (const ep of list) {
      if (schemaMissingRequired(fields, ep.fields)) return false
    }
  }
  return true
}

export async function findServicesFolderId(listScopes) {
  const rows = await listScopes()
  const root = (rows ?? []).find((s) => s.kind === 'folder' && s.name === 'global' && !s.parent_id)
  const folder = (rows ?? []).find(
    (s) => s.kind === 'folder' && s.name === '_services' && s.parent_id === root?.id,
  )
  return folder?.id ?? null
}

export async function findServiceScope(listScopes, servicePk) {
  const rows = await listScopes()
  return (rows ?? []).find((s) => s.kind === 'service' && s.service_id === servicePk) ?? null
}

export function swallowAttachConflict(err, fallback) {
  if (err?.response?.status === 409) return fallback
  throw err
}
