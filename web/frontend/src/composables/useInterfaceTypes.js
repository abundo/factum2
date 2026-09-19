import { computed, ref } from 'vue'
import { getInterfaceTypes } from '@/api/dcim'
import {
  defaultInterfaceType,
  itemsWithCurrent,
  labelForType,
  toSelectItems,
} from '@/utils/interfaceTypes'

export function useInterfaceTypes() {
  const rows = ref([])
  const loading = ref(false)

  const items = computed(() => toSelectItems(rows.value))
  const defaultType = computed(() => defaultInterfaceType(rows.value))

  function typeItems(current) {
    return itemsWithCurrent(rows.value, current)
  }

  function typeLabel(value) {
    return labelForType(rows.value, value)
  }

  function load() {
    loading.value = true
    return getInterfaceTypes()
      .then((data) => {
        rows.value = data ?? []
      })
      .catch(() => {
        rows.value = []
      })
      .finally(() => {
        loading.value = false
      })
  }

  return { rows, items, defaultType, loading, load, typeItems, typeLabel }
}
