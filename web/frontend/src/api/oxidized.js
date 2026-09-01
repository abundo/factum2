import http from './http'

export function getOxidizedNodes() {
  return http.get('/oxidized/nodes').then((res) => res.data)
}

export function getOxidizedConfig(nodeFull) {
  return http
    .get('/oxidized/node/config', { params: { node_full: nodeFull } })
    .then((res) => res.data)
}

export function getOxidizedVersions(nodeFull) {
  return http
    .get('/oxidized/node/versions', { params: { node_full: nodeFull } })
    .then((res) => res.data)
}

export function getOxidizedVersion(nodeFull, oid) {
  return http
    .get('/oxidized/node/version', { params: { node_full: nodeFull, oid } })
    .then((res) => res.data)
}

export function getOxidizedDiff(nodeFull, oid, oid2) {
  return http
    .get('/oxidized/node/diff', { params: { node_full: nodeFull, oid, oid2 } })
    .then((res) => res.data)
}
