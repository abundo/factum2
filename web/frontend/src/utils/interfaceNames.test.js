import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  MAX_INTERFACE_RANGE,
  compareInterfaceName,
  expandInterfaceNames,
  parseInterfaceNamePattern,
} from './interfaceNames.js'

test('plain name is unchanged', () => {
  assert.deepEqual(expandInterfaceNames('Ethernet1'), ['Ethernet1'])
  assert.deepEqual(expandInterfaceNames('  Ethernet1  '), ['Ethernet1'])
})

test('Ethernet[1-48] expands to Ethernet1 through Ethernet48', () => {
  const names = expandInterfaceNames('Ethernet[1-48]')
  assert.equal(names.length, 48)
  assert.equal(names[0], 'Ethernet1')
  assert.equal(names[47], 'Ethernet48')
  assert.equal(names[9], 'Ethernet10')
})

test('nested ranges are a cartesian product', () => {
  assert.deepEqual(expandInterfaceNames('Ethernet[1-2]/[1-3]'), [
    'Ethernet1/1',
    'Ethernet1/2',
    'Ethernet1/3',
    'Ethernet2/1',
    'Ethernet2/2',
    'Ethernet2/3',
  ])
})

test('comma lists and mixed ranges', () => {
  assert.deepEqual(expandInterfaceNames('Ethernet[1,3,5]'), ['Ethernet1', 'Ethernet3', 'Ethernet5'])
  assert.deepEqual(expandInterfaceNames('Ethernet[1-3,5]'), [
    'Ethernet1',
    'Ethernet2',
    'Ethernet3',
    'Ethernet5',
  ])
})

test('zero-padding follows the start width', () => {
  assert.deepEqual(expandInterfaceNames('Ethernet[01-03]'), [
    'Ethernet01',
    'Ethernet02',
    'Ethernet03',
  ])
})

test('duplicates from expansion are dropped', () => {
  assert.deepEqual(expandInterfaceNames('Ethernet[1,1,2]'), ['Ethernet1', 'Ethernet2'])
})

test('empty name is an error', () => {
  assert.throws(() => expandInterfaceNames(''), { message: 'Name is required' })
  assert.throws(() => expandInterfaceNames('   '), { message: 'Name is required' })
})

test('invalid patterns error', () => {
  assert.throws(() => expandInterfaceNames('Ethernet[1-48'), {
    message: 'Unmatched brackets in interface name',
  })
  assert.throws(() => expandInterfaceNames('Ethernet[]'), {
    message: 'Empty [] in interface name',
  })
  assert.throws(() => expandInterfaceNames('Ethernet[48-1]'), {
    message: 'Range 48-1 is reversed',
  })
  assert.throws(() => expandInterfaceNames('Ethernet[a-c]'), {
    message: 'Invalid range "a-c"',
  })
})

test('range larger than the cap is rejected', () => {
  assert.throws(() => expandInterfaceNames(`Ethernet[1-${MAX_INTERFACE_RANGE + 1}]`), {
    message: `Range expands to more than ${MAX_INTERFACE_RANGE} interfaces`,
  })
})

test('parseInterfaceNamePattern does not throw', () => {
  assert.deepEqual(parseInterfaceNamePattern(''), { names: [], error: 'Name is required' })
  assert.deepEqual(parseInterfaceNamePattern('Ethernet[1-2]'), {
    names: ['Ethernet1', 'Ethernet2'],
    error: '',
  })
  assert.equal(parseInterfaceNamePattern('Ethernet[x]').error, 'Invalid range "x"')
  assert.deepEqual(parseInterfaceNamePattern('Ethernet[1-2]', { expandRanges: false }), {
    names: ['Ethernet[1-2]'],
    error: '',
  })
})

test('compareInterfaceName is alphanumeric like DeviceList', () => {
  const names = [
    'Ethernet10',
    'Ethernet2',
    'Ethernet1',
    'Ethernet1/10',
    'Ethernet1/2',
    'Management1',
    'ethernet3',
  ]
  names.sort(compareInterfaceName)
  assert.deepEqual(names, [
    'Ethernet1',
    'Ethernet1/2',
    'Ethernet1/10',
    'Ethernet2',
    'ethernet3',
    'Ethernet10',
    'Management1',
  ])
})
