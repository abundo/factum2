export const TICKS_PER_U = 2

export function ticksToU(ticks) {
  return Number(ticks || 0) / TICKS_PER_U
}

export function uToTicks(u) {
  return Math.round(Number(u || 0) * TICKS_PER_U)
}

export function snapToGrid(value, grid) {
  if (!grid) return value
  return Math.round(value / grid) * grid
}

export function normalizeRotation(deg) {
  const n = ((Number(deg) || 0) % 360) + 360
  return n % 360
}

export function rotatedFootprint(x, y, width, depth, rotation) {
  const rot = normalizeRotation(rotation)
  let w = width > 0 ? width : 600
  let d = depth > 0 ? depth : 1000
  if (rot === 90 || rot === 270) {
    const t = w
    w = d
    d = t
  }
  return { x, y, width: w, height: d, maxX: x + w, maxY: y + d }
}

export function footprintsOverlap(a, b) {
  return a.x < b.maxX && b.x < a.maxX && a.y < b.maxY && b.y < a.maxY
}

export function footprintInRoom(fp, roomWidth, roomHeight) {
  return fp.x >= 0 && fp.y >= 0 && fp.maxX <= roomWidth && fp.maxY <= roomHeight
}

export function pointerToWorld(stage, pointer) {
  if (!stage || !pointer) return { x: 0, y: 0 }
  const transform = stage.getAbsoluteTransform().copy()
  transform.invert()
  return transform.point(pointer)
}

export function yFromTopToOffsetTicks(yFromTop, rackHeightTicks, pxPerTick) {
  if (!pxPerTick) return 0
  return Math.round(rackHeightTicks - yFromTop / pxPerTick)
}

export function offsetTicksToYFromTop(offsetTicks, heightTicks, rackHeightTicks, pxPerTick) {
  return (rackHeightTicks - offsetTicks - heightTicks) * pxPerTick
}

export function snapOffsetToWholeU(offsetTicks) {
  return Math.round(offsetTicks / TICKS_PER_U) * TICKS_PER_U
}
