<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { pointerToWorld, rotatedFootprint } from '@/utils/dcimGeometry'

const props = defineProps({
  plan: { type: Object, required: true },
  editMode: { type: Boolean, default: false },
  selectedRackId: { type: Number, default: 0 },
})

const emit = defineEmits(['select', 'move', 'rotate'])

const host = ref(null)
let stage
let layer
let dragging = null

function occupancyFill(rack) {
  if (rack?.unknown_dimensions) return '#facc15'
  if (rack?.has_conflict) return '#fca5a5'
  const pct = rack?.occupancy_percent || 0
  if (pct === 0) return '#e2e8f0'
  if (pct < 50) return '#86efac'
  if (pct < 80) return '#93c5fd'
  return '#fdba74'
}

async function draw() {
  if (!host.value || !stage) return
  const Konva = (await import('konva')).default
  layer.destroyChildren()
  const plan = props.plan
  const w = plan.width_mm || 20000
  const h = plan.height_mm || 15000
  const grid = plan.grid_mm || 600

  layer.add(
    new Konva.Rect({
      x: 0,
      y: 0,
      width: w,
      height: h,
      stroke: '#334155',
      strokeWidth: 20,
      fill: '#f8fafc',
    }),
  )
  for (let x = 0; x <= w; x += grid) {
    layer.add(new Konva.Line({ points: [x, 0, x, h], stroke: '#e2e8f0', strokeWidth: 4 }))
  }
  for (let y = 0; y <= h; y += grid) {
    layer.add(new Konva.Line({ points: [0, y, w, y], stroke: '#e2e8f0', strokeWidth: 4 }))
  }

  for (const a of plan.annotations || []) {
    layer.add(
      new Konva.Rect({
        x: a.x_mm,
        y: a.y_mm,
        width: a.width_mm || 400,
        height: a.height_mm || 200,
        stroke: '#94a3b8',
        dash: [40, 20],
        listening: false,
      }),
    )
    if (a.text) {
      layer.add(
        new Konva.Text({
          x: a.x_mm + 20,
          y: a.y_mm + 20,
          text: a.text,
          fontSize: 120,
          fill: '#475569',
          listening: false,
        }),
      )
    }
  }

  for (const row of plan.racks || []) {
    const meta = row.rack || {}
    const fp = rotatedFootprint(row.x_mm, row.y_mm, meta.width_mm, meta.depth_mm, row.rotation)
    const selected = props.selectedRackId === row.rack_id
    const rect = new Konva.Rect({
      x: fp.x,
      y: fp.y,
      width: fp.width,
      height: fp.height,
      fill: occupancyFill(meta),
      stroke: selected ? '#2563eb' : '#0f172a',
      strokeWidth: selected ? 24 : 12,
      name: 'rack',
      draggable: props.editMode,
    })
    rect.setAttr('rackId', row.rack_id)
    rect.on('click tap', () => emit('select', row.rack_id))
    rect.on('dragstart', () => {
      dragging = row.rack_id
    })
    rect.on('dragend', () => {
      if (dragging !== row.rack_id) return
      emit('move', { rackId: row.rack_id, x: rect.x(), y: rect.y() })
      dragging = null
    })
    rect.on('dblclick dbltap', () => emit('rotate', row.rack_id))
    layer.add(rect)
    layer.add(
      new Konva.Text({
        x: fp.x + 20,
        y: fp.y + 20,
        width: fp.width - 40,
        text: meta.name || `rack ${row.rack_id}`,
        fontSize: 140,
        fill: '#0f172a',
        listening: false,
      }),
    )
  }
  layer.draw()
}

function fit() {
  if (!stage || !host.value || !props.plan) return
  const w = props.plan.width_mm || 20000
  const h = props.plan.height_mm || 15000
  const cw = host.value.clientWidth || 800
  const ch = host.value.clientHeight || 500
  const scale = Math.min(cw / w, ch / h) * 0.92
  stage.scale({ x: scale, y: scale })
  stage.position({ x: (cw - w * scale) / 2, y: (ch - h * scale) / 2 })
  stage.batchDraw()
}

function onWheel(ev) {
  if (!stage) return
  ev.evt.preventDefault()
  const old = stage.scaleX()
  const pointer = stage.getPointerPosition()
  const scaleBy = ev.evt.deltaY > 0 ? 0.92 : 1.08
  const next = Math.min(8, Math.max(0.02, old * scaleBy))
  const world = pointerToWorld(stage, pointer)
  stage.scale({ x: next, y: next })
  stage.position({
    x: pointer.x - world.x * next,
    y: pointer.y - world.y * next,
  })
  stage.batchDraw()
}

onMounted(async () => {
  const Konva = (await import('konva')).default
  stage = new Konva.Stage({
    container: host.value,
    width: host.value.clientWidth || 800,
    height: host.value.clientHeight || 520,
    draggable: true,
  })
  layer = new Konva.Layer()
  stage.add(layer)
  stage.on('wheel', onWheel)
  await draw()
  fit()
  window.addEventListener('resize', onResize)
})

function onResize() {
  if (!stage || !host.value) return
  stage.size({ width: host.value.clientWidth, height: host.value.clientHeight })
  fit()
}

watch(
  () => [props.plan, props.editMode, props.selectedRackId],
  () => {
    draw()
  },
  { deep: true },
)

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  stage?.destroy()
})

defineExpose({ fit })
</script>

<template>
  <div
    ref="host"
    class="w-full h-full min-h-[420px] bg-slate-100 rounded border border-default"
  ></div>
</template>
