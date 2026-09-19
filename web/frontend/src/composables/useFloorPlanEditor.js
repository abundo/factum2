import { computed, ref } from 'vue'
import {
  footprintInRoom,
  footprintsOverlap,
  rotatedFootprint,
  snapToGrid,
} from '@/utils/dcimGeometry'

function clonePlan(plan) {
  return JSON.parse(JSON.stringify(plan))
}

export function useFloorPlanEditor() {
  const original = ref(null)
  const draft = ref(null)
  const history = ref([])
  const future = ref([])
  const editMode = ref(false)
  const selectedRackId = ref(0)

  const dirty = computed(() => {
    if (!original.value || !draft.value) return false
    return (
      JSON.stringify(layoutPayload(original.value)) !== JSON.stringify(layoutPayload(draft.value))
    )
  })

  function layoutPayload(plan) {
    return {
      revision: plan.revision,
      width_mm: plan.width_mm,
      height_mm: plan.height_mm,
      grid_mm: plan.grid_mm,
      racks: (plan.racks || []).map((r) => ({
        rack_id: r.rack_id,
        x_mm: r.x_mm,
        y_mm: r.y_mm,
        rotation: r.rotation,
      })),
      annotations: (plan.annotations || []).map((a) => ({
        kind: a.kind,
        text: a.text,
        x_mm: a.x_mm,
        y_mm: a.y_mm,
        width_mm: a.width_mm,
        height_mm: a.height_mm,
        rotation: a.rotation,
      })),
    }
  }

  function load(plan) {
    original.value = clonePlan(plan)
    draft.value = clonePlan(plan)
    history.value = []
    future.value = []
    editMode.value = false
  }

  function pushHistory() {
    history.value.push(clonePlan(draft.value))
    if (history.value.length > 50) history.value.shift()
    future.value = []
  }

  function undo() {
    if (!history.value.length) return
    future.value.push(clonePlan(draft.value))
    draft.value = history.value.pop()
  }

  function redo() {
    if (!future.value.length) return
    history.value.push(clonePlan(draft.value))
    draft.value = future.value.pop()
  }

  function cancel() {
    if (original.value) draft.value = clonePlan(original.value)
    history.value = []
    future.value = []
    editMode.value = false
  }

  function acceptSaved(plan) {
    load(plan)
  }

  function validateRack(rackRow, x, y, rotation) {
    const plan = draft.value
    if (!plan) return { ok: false, reason: 'invalid' }
    const meta = rackRow.rack || {}
    const fp = rotatedFootprint(x, y, meta.width_mm, meta.depth_mm, rotation)
    if (!footprintInRoom(fp, plan.width_mm, plan.height_mm)) {
      return { ok: false, reason: 'out_of_bounds', footprint: fp }
    }
    for (const other of plan.racks || []) {
      if (other.rack_id === rackRow.rack_id) continue
      const ofp = rotatedFootprint(
        other.x_mm,
        other.y_mm,
        other.rack?.width_mm,
        other.rack?.depth_mm,
        other.rotation,
      )
      if (footprintsOverlap(fp, ofp)) return { ok: false, reason: 'overlap', footprint: fp }
    }
    return { ok: true, footprint: fp }
  }

  function moveRack(rackId, x, y) {
    const plan = draft.value
    if (!plan) return false
    const row = (plan.racks || []).find((r) => r.rack_id === rackId)
    if (!row) return false
    const grid = plan.grid_mm || 600
    const nx = snapToGrid(x, grid)
    const ny = snapToGrid(y, grid)
    const check = validateRack(row, nx, ny, row.rotation)
    if (!check.ok) return false
    pushHistory()
    row.x_mm = nx
    row.y_mm = ny
    return true
  }

  function rotateRack(rackId) {
    const plan = draft.value
    if (!plan) return false
    const row = (plan.racks || []).find((r) => r.rack_id === rackId)
    if (!row) return false
    const next = (Number(row.rotation) + 90) % 360
    const check = validateRack(row, row.x_mm, row.y_mm, next)
    if (!check.ok) return false
    pushHistory()
    row.rotation = next
    return true
  }

  function addRack(summary, x, y) {
    const plan = draft.value
    if (!plan) return false
    if ((plan.racks || []).some((r) => r.rack_id === summary.id)) return false
    const grid = plan.grid_mm || 600
    const nx = snapToGrid(x, grid)
    const ny = snapToGrid(y, grid)
    const row = {
      rack_id: summary.id,
      x_mm: nx,
      y_mm: ny,
      rotation: 0,
      rack: summary,
    }
    const check = validateRack(row, nx, ny, 0)
    if (!check.ok) return false
    pushHistory()
    plan.racks = [...(plan.racks || []), row]
    plan.available_racks = (plan.available_racks || []).filter((r) => r.id !== summary.id)
    selectedRackId.value = summary.id
    return true
  }

  function removeRack(rackId) {
    const plan = draft.value
    if (!plan) return
    const row = (plan.racks || []).find((r) => r.rack_id === rackId)
    if (!row) return
    pushHistory()
    plan.racks = (plan.racks || []).filter((r) => r.rack_id !== rackId)
    if (row.rack) plan.available_racks = [...(plan.available_racks || []), row.rack]
    if (selectedRackId.value === rackId) selectedRackId.value = 0
  }

  return {
    original,
    draft,
    dirty,
    editMode,
    selectedRackId,
    history,
    future,
    load,
    cancel,
    undo,
    redo,
    acceptSaved,
    layoutPayload,
    moveRack,
    rotateRack,
    addRack,
    removeRack,
    validateRack,
  }
}
