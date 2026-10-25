import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  internalDropRegions,
  serverDropRegions,
  serverOrderAfterDrop,
  sortDirectoryNodes,
} from './ldapTree.js'

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

test('a user can move into another folder, not beside a sibling', () => {
  assert.deepEqual(
    internalDropRegions({
      sourceParent: 1,
      targetKind: 'ou',
      overId: 2,
      aroundParent: 0,
      intoSelf: false,
    }),
    ['over', 'before', 'after'],
  )
  assert.equal(
    internalDropRegions({
      sourceParent: 1,
      targetKind: 'user',
      overId: null,
      aroundParent: 1,
      intoSelf: false,
    }),
    false,
  )
})

test('a folder cannot be dropped into itself', () => {
  assert.equal(
    internalDropRegions({
      sourceParent: 0,
      targetKind: 'ou',
      overId: 4,
      aroundParent: 4,
      intoSelf: true,
    }),
    false,
  )
})

test('LDAP servers reorder beside servers and cannot pass the local directory', () => {
  assert.deepEqual(serverDropRegions('server'), ['before', 'after'])
  assert.equal(serverDropRegions('internal'), false)
  assert.deepEqual(serverOrderAfterDrop([1, 2, 3], 3, 'server', 1, 'before'), [3, 1, 2])
  assert.deepEqual(serverOrderAfterDrop([1, 2, 3], 1, 'server', 3, 'after'), [2, 3, 1])
  assert.equal(serverOrderAfterDrop([1, 2, 3], 3, 'internal', 0, 'before'), null)
})
