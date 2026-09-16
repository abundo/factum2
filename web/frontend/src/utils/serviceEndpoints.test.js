import assert from 'node:assert/strict'
import { test } from 'node:test'
import { applySchemaDefaults, schemaMissingRequired } from './serviceEndpoints.js'

test('applySchemaDefaults fills empty keys and keeps explicit values', () => {
  const schema = [
    { name: 'mtu', type: 'int', default: 1500 },
    { name: 'control_word', type: 'bool', default: false },
    { name: 'vlan', type: 'vlan' },
  ]
  const got = applySchemaDefaults(schema, { mtu: 9000 })
  assert.equal(got.mtu, 9000)
  assert.equal(got.control_word, false)
  assert.equal(got.vlan, undefined)
})

test('schemaMissingRequired treats a default as satisfying required', () => {
  const schema = [{ name: 'mtu', type: 'int', required: true, default: 1500 }]
  assert.equal(schemaMissingRequired(schema, {}), false)
  assert.equal(schemaMissingRequired(schema, { mtu: 1280 }), false)
  const noDefault = [{ name: 'mtu', type: 'int', required: true }]
  assert.equal(schemaMissingRequired(noDefault, {}), true)
})
