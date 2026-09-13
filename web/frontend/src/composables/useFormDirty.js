import { computed, ref, toRaw, toValue, watch } from 'vue'

function stableStringify(value) {
  const seen = new WeakSet()
  return JSON.stringify(toRaw(value), (_, v) => {
    if (v && typeof v === 'object') {
      if (seen.has(v)) return undefined
      seen.add(v)
      if (Array.isArray(v)) return v
      return Object.keys(v)
        .sort()
        .reduce((acc, k) => {
          const val = v[k]
          if (val === undefined || val === null || val === '' || typeof val === 'function') {
            return acc
          }
          if (Array.isArray(val) && val.length === 0) return acc
          acc[k] = val
          return acc
        }, {})
    }
    return v
  })
}

// Snapshot a form when its overlay opens (and again after async load) so
// overlay/Escape dismiss can be blocked until the user hits Cancel/Close.
export function useFormDirty(source, options = {}) {
  const snapshot = ref(null)
  const current = ref('null')

  function serialize() {
    return stableStringify(toValue(source)) ?? 'null'
  }

  function markClean() {
    current.value = serialize()
    snapshot.value = current.value
  }

  watch(
    () => toValue(source),
    () => {
      current.value = serialize()
    },
    { deep: true, flush: 'sync' },
  )

  const dirty = computed(() => snapshot.value != null && current.value !== snapshot.value)

  const open = options.open
  const loading = options.loading

  if (open) {
    watch(
      () => [!!toValue(open), !!toValue(loading)],
      ([isOpen, isLoading]) => {
        if (!isOpen) {
          snapshot.value = null
          return
        }
        if (isLoading) return
        // Flush-post already runs after the parent assigned the form in the
        // same tick as opening. nextTick here would race Playwright fills
        // (and any other same-frame input) and snapshot the edited value.
        markClean()
      },
      { flush: 'post' },
    )
  }

  return { dirty, markClean }
}
