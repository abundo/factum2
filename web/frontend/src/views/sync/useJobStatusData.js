import { onMounted, onUnmounted, ref } from 'vue'
import { getJobs, getWorkerStatus } from '@/api/jobs'

const REFRESH_INTERVAL_MS = 5000
const AUTO_REFRESH_DURATION_MS = 60 * 60 * 1000

// Shared by the Status and Workers tabs so both tables use one poll.
const nodes = ref([])
const loading = ref(true)
const loadError = ref(false)
const jobs = ref([])
const jobsLoading = ref(true)
const jobsLoadError = ref(false)
const autoRefresh = ref(false)

let refreshTimer = null
let stopTimer = null
let refreshInFlight = false
let users = 0

function loadStatus({ showLoading = false } = {}) {
  if (showLoading) {
    loading.value = true
    loadError.value = false
  }
  return getWorkerStatus()
    .then((data) => {
      nodes.value = data ?? []
    })
    .catch(() => {
      if (showLoading) {
        loadError.value = true
      }
    })
    .finally(() => {
      if (showLoading) {
        loading.value = false
      }
    })
}

function loadJobs({ showLoading = false } = {}) {
  if (showLoading) {
    jobsLoading.value = true
    jobsLoadError.value = false
  }
  return getJobs()
    .then((data) => {
      jobs.value = data ?? []
    })
    .catch(() => {
      if (showLoading) {
        jobsLoadError.value = true
      }
    })
    .finally(() => {
      if (showLoading) {
        jobsLoading.value = false
      }
    })
}

function loadAll({ showLoading = false } = {}) {
  if (refreshInFlight) {
    return Promise.resolve()
  }
  refreshInFlight = true
  return Promise.all([loadStatus({ showLoading }), loadJobs({ showLoading })]).finally(() => {
    refreshInFlight = false
  })
}

function clearRefreshTimers() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (stopTimer) {
    clearTimeout(stopTimer)
    stopTimer = null
  }
}

function stopAutoRefresh() {
  autoRefresh.value = false
  clearRefreshTimers()
}

function startAutoRefresh({ refreshNow = false } = {}) {
  clearRefreshTimers()
  autoRefresh.value = true
  if (refreshNow) {
    loadAll()
  }
  refreshTimer = setInterval(() => {
    loadAll()
  }, REFRESH_INTERVAL_MS)
  stopTimer = setTimeout(stopAutoRefresh, AUTO_REFRESH_DURATION_MS)
}

function setAutoRefresh(on) {
  if (on) {
    startAutoRefresh({ refreshNow: true })
  } else {
    stopAutoRefresh()
  }
}

export function useJobStatusData() {
  onMounted(() => {
    users += 1
    if (users === 1) {
      loadAll({ showLoading: true })
      startAutoRefresh()
    }
  })

  onUnmounted(() => {
    users -= 1
    if (users <= 0) {
      users = 0
      stopAutoRefresh()
    }
  })

  return {
    nodes,
    loading,
    loadError,
    jobs,
    jobsLoading,
    jobsLoadError,
    autoRefresh,
    setAutoRefresh,
  }
}
