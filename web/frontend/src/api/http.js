import axios from 'axios'

const http = axios.create({
  baseURL: '/api',
  withCredentials: true,
})

// On a mid-session 401 (e.g. an expired JWT), bounce to the login page. /me and
// /login are excluded: a 401 from those is an expected "not logged in yet" or
// "wrong password" result, already handled by their own callers.
http.interceptors.response.use(
  (res) => res,
  (err) => {
    const url = err.config?.url ?? ''
    if (err.response?.status === 401 && url !== '/me' && url !== '/login' && url !== '/version') {
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

export default http
