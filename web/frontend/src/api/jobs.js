import http from './http'

export function getSyncTargets() {
  return http.get('/sync/targets').then((res) => res.data)
}

export function triggerSync(target) {
  return http.post(`/sync/${target}`).then((res) => res.data)
}

export function triggerSyncAll() {
  return http.post('/sync/all').then((res) => res.data)
}

export function getWorkerStatus() {
  return http.get('/worker/status').then((res) => res.data)
}

export function getJobs() {
  return http.get('/jobs').then((res) => res.data)
}

export function getJobTaskEvents(jobId, taskId) {
  return http.get(`/jobs/${jobId}/tasks/${taskId}/events`).then((res) => res.data)
}
