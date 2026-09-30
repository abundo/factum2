import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatOxidizedAge } from './oxidized.js'

const now = new Date(2026, 8, 29, 15, 0, 0)

test('same calendar day is 0 days', () => {
  assert.equal(formatOxidizedAge(new Date(2026, 8, 29, 1, 0, 0), now), '0 days')
})

test('age under a year is whole calendar days', () => {
  assert.equal(formatOxidizedAge(new Date(2026, 8, 28, 23, 0, 0), now), '1 day')
  assert.equal(formatOxidizedAge(new Date(2026, 8, 17, 8, 0, 0), now), '12 days')
  assert.equal(formatOxidizedAge(new Date(2025, 8, 30, 8, 0, 0), now), '364 days')
})

test('one year or more is years and leftover days', () => {
  assert.equal(formatOxidizedAge(new Date(2025, 8, 29, 15, 0, 0), now), '1 year 0 days')
  assert.equal(formatOxidizedAge(new Date(2025, 8, 17, 8, 0, 0), now), '1 year 12 days')
  assert.equal(formatOxidizedAge(new Date(2024, 8, 29, 0, 0, 0), now), '2 years 0 days')
  assert.equal(formatOxidizedAge(new Date(2024, 8, 28, 0, 0, 0), now), '2 years 1 day')
})

test('Feb 29 anniversary does not count a year early', () => {
  const later = new Date(2025, 1, 28, 12, 0, 0)
  assert.equal(formatOxidizedAge(new Date(2024, 1, 29, 12, 0, 0), later), '365 days')
  assert.equal(formatOxidizedAge(new Date(2024, 1, 29, 12, 0, 0), new Date(2025, 2, 1, 12, 0, 0)), '1 year 0 days')
})

test('missing or invalid timestamps', () => {
  assert.equal(formatOxidizedAge(''), '—')
  assert.equal(formatOxidizedAge(null), '—')
  assert.equal(formatOxidizedAge('never'), 'never')
  assert.equal(formatOxidizedAge('not-a-date'), '—')
})
