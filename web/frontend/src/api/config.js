import http from './http'

export function listScopes() {
  return http.get('/config/scopes').then((res) => res.data ?? [])
}
export function getScopeTree() {
  return http.get('/config/scopes/tree').then((res) => res.data ?? [])
}
export function createScope(payload) {
  return http.post('/config/scopes', payload).then((res) => res.data)
}
export function updateScope(id, payload) {
  return http.put(`/config/scopes/${id}`, payload).then((res) => res.data)
}
export function deleteScope(id) {
  return http.delete(`/config/scopes/${id}`)
}
export function moveScope(id, payload) {
  return http.post(`/config/scopes/${id}/move`, payload).then((res) => res.data)
}
export function detachScope(id) {
  return http.post(`/config/scopes/${id}/detach`)
}
export function listFeatures(scopeId) {
  return http.get(`/config/scopes/${scopeId}/features`).then((res) => res.data ?? [])
}
export function createFeature(scopeId, payload) {
  return http.post(`/config/scopes/${scopeId}/features`, payload).then((res) => res.data)
}
export function updateFeature(id, payload) {
  return http.put(`/config/features/${id}`, payload).then((res) => res.data)
}
export function deleteFeature(id) {
  return http.delete(`/config/features/${id}`)
}

export function listVariables() {
  return http.get('/config/variables').then((res) => res.data ?? [])
}
export function createVariable(payload) {
  return http.post('/config/variables', payload).then((res) => res.data)
}
export function updateVariable(id, payload) {
  return http.put(`/config/variables/${id}`, payload).then((res) => res.data)
}
export function deleteVariable(id) {
  return http.delete(`/config/variables/${id}`)
}

export function listAssignments(scopeId) {
  return http
    .get('/config/assignments', { params: scopeId ? { scope_id: scopeId } : {} })
    .then((res) => res.data ?? [])
}
export function upsertAssignment(payload) {
  return http.put('/config/assignments', payload).then((res) => res.data)
}
export function deleteAssignment(id) {
  return http.delete(`/config/assignments/${id}`)
}

export function resolveInterface(interfaceId) {
  return http
    .get('/config/resolve', { params: { interface_id: interfaceId } })
    .then((res) => res.data ?? [])
}
export function getMatrix(scopeId, variable) {
  return http
    .get('/config/matrix', { params: { scope_id: scopeId, variable } })
    .then((res) => res.data ?? [])
}

export function listServiceTypes() {
  return http.get('/config/service-types').then((res) => res.data ?? [])
}
export function createServiceType(payload) {
  return http.post('/config/service-types', payload).then((res) => res.data)
}
export function updateServiceType(id, payload) {
  return http.put(`/config/service-types/${id}`, payload).then((res) => res.data)
}
export function deleteServiceType(id) {
  return http.delete(`/config/service-types/${id}`)
}

export function listMacros() {
  return http.get('/config/macros').then((res) => res.data ?? [])
}
export function createMacro(payload) {
  return http.post('/config/macros', payload).then((res) => res.data)
}
export function updateMacro(id, payload) {
  return http.put(`/config/macros/${id}`, payload).then((res) => res.data)
}
export function deleteMacro(id) {
  return http.delete(`/config/macros/${id}`)
}

export function renderConfig(payload) {
  return http.post('/config/render', payload).then((res) => res.data)
}
