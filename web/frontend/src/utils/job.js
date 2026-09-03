export function jobTasks(job) {
  return job?.tasks ?? []
}

export function jobStatus(job) {
  if (!job?.finished_at) {
    return { label: 'Running', color: 'info' }
  }
  const failed = jobTasks(job).some((task) => task.exit_code !== 0)
  if (failed) {
    return { label: 'Failed', color: 'error' }
  }
  return { label: 'Success', color: 'success' }
}

export function jobDuration(job) {
  if (!job?.finished_at) {
    return '-'
  }
  const seconds = (new Date(job.finished_at) - new Date(job.started_at)) / 1000
  if (seconds < 1) {
    return '<1s'
  }
  return `${Math.round(seconds)}s`
}
