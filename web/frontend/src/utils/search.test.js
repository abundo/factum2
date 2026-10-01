import assert from 'node:assert/strict'
import { test } from 'node:test'
import { matchesWords, searchWords, valuesText } from './search.js'

test('searchWords splits and lower-cases', () => {
  assert.deepEqual(searchWords('  Foo  BAR '), ['foo', 'bar'])
  assert.deepEqual(searchWords(''), [])
})

test('a row matches when its text holds every word', () => {
  const text = valuesText({ name: 'edge-1', site: 'Stockholm' }, 'up')
  const words = searchWords('stock edge')
  assert.equal(matchesWords(text, words), true)
  assert.equal(matchesWords(text, searchWords('stock missing')), false)
})
