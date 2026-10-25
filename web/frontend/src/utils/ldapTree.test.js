import assert from 'node:assert/strict'
import { test } from 'node:test'
import { sortDirectoryNodes } from './ldapTree.js'

test('folders come first, then other entries, each by name', () => {
  const sorted = sortDirectoryNodes([
    { kind: 'user', title: 'mike' },
    { kind: 'ou', title: 'zeta' },
    { kind: 'role', title: 'Audit' },
    { kind: 'ou', title: 'Alpha' },
    { kind: 'user', title: 'Ann' },
  ])
  assert.deepEqual(
    sorted.map((n) => n.title),
    ['Alpha', 'zeta', 'Ann', 'Audit', 'mike'],
  )
})
