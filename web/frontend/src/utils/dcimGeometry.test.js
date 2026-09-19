import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  footprintsOverlap,
  footprintInRoom,
  offsetTicksToYFromTop,
  pointerToWorld,
  rotatedFootprint,
  snapOffsetToWholeU,
  snapToGrid,
  ticksToU,
  uToTicks,
  yFromTopToOffsetTicks,
} from './dcimGeometry.js'

test('ticks convert to U', () => {
  assert.equal(uToTicks(2), 4)
  assert.equal(ticksToU(4), 2)
  assert.equal(uToTicks(0.5), 1)
})

test('snap to grid', () => {
  assert.equal(snapToGrid(750, 600), 600)
  assert.equal(snapToGrid(900, 600), 1200)
})

test('rotated footprints and overlap', () => {
  const a = rotatedFootprint(0, 0, 600, 1000, 0)
  assert.equal(a.maxX, 600)
  assert.equal(a.maxY, 1000)
  const b = rotatedFootprint(0, 0, 600, 1000, 90)
  assert.equal(b.maxX, 1000)
  assert.equal(b.maxY, 600)
  const c = rotatedFootprint(600, 0, 600, 1000, 0)
  assert.equal(footprintsOverlap(a, c), false)
  const d = rotatedFootprint(100, 0, 600, 1000, 0)
  assert.equal(footprintsOverlap(a, d), true)
  assert.equal(footprintInRoom(a, 20000, 15000), true)
  assert.equal(footprintInRoom(rotatedFootprint(19900, 0, 600, 1000, 0), 20000, 15000), false)
})

test('zoomed pointer conversion uses the inverted stage transform', () => {
  const stage = {
    getAbsoluteTransform() {
      return {
        copy() {
          return {
            invert() {},
            point({ x, y }) {
              return { x: (x - 50) / 2, y: (y - 10) / 2 }
            },
          }
        },
      }
    },
  }
  assert.deepEqual(pointerToWorld(stage, { x: 150, y: 50 }), { x: 50, y: 20 })
})

test('elevation y maps to ticks from the rack bottom', () => {
  const rackTicks = 84
  const px = 10
  assert.equal(offsetTicksToYFromTop(0, 4, rackTicks, px), 800)
  assert.equal(yFromTopToOffsetTicks(800, rackTicks, px), 4)
  assert.equal(snapOffsetToWholeU(5), 6)
  assert.equal(snapOffsetToWholeU(7), 8)
})
