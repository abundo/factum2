import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseZoneFile } from './zoneFile.js'

test('skips SOA and apex NS, keeps the rest', () => {
  const parsed = parseZoneFile(
    `
$TTL 3600
$ORIGIN example.com.
@       IN SOA  ns1.example.com. hostmaster.example.com. (
                        2024010101 ; serial
                        3600       ; refresh
                        1800       ; retry
                        1209600    ; expire
                        3600 )     ; minimum
        IN NS   ns1.example.com.
www     IN A    192.0.2.2
`,
    'example.com',
  )
  assert.equal(parsed.skippedSoa, 1)
  assert.equal(parsed.skippedApexNs, 1)
  assert.equal(parsed.unsupported.length, 0)
  assert.deepEqual(
    parsed.records.map(({ name, type, value }) => ({ name, type, value })),
    [{ name: 'www', type: 'A', value: '192.0.2.2' }],
  )
})

test('unsupported types are listed with line and owner', () => {
  const parsed = parseZoneFile(
    `
www A 192.0.2.1
@ CAA 0 issue "letsencrypt.org"
mail NAPTR 10 10 "u" "E2U+sip" "!^.*$!sip:info@example.com!" .
`,
    'example.com',
  )
  assert.equal(parsed.records.length, 1)
  assert.equal(parsed.skippedUnknown, 2)
  assert.deepEqual(
    parsed.unsupported.map(({ line, type, name, message }) => ({
      line,
      type,
      name,
      message,
    })),
    [
      {
        line: 3,
        type: 'CAA',
        name: '@',
        message: 'Line 3: unsupported type CAA for @',
      },
      {
        line: 4,
        type: 'NAPTR',
        name: 'mail',
        message: 'Line 4: unsupported type NAPTR for mail',
      },
    ],
  )
})

test('unsupported $ directives are listed', () => {
  const parsed = parseZoneFile(
    `
$GENERATE 1-2 host$ A 192.0.2.$
www A 192.0.2.1
`,
    'example.com',
  )
  assert.equal(parsed.records.length, 1)
  assert.equal(parsed.unsupported.length, 1)
  assert.equal(parsed.unsupported[0].type, '$GENERATE')
  assert.match(parsed.unsupported[0].message, /Line 2: unsupported type \$GENERATE/)
})

test('parse errors still throw', () => {
  assert.throws(() => parseZoneFile('$INCLUDE other.zone\nwww A 192.0.2.1\n', 'example.com'), {
    message: 'Line 1: $INCLUDE is not supported',
  })
  assert.throws(() => parseZoneFile('\n', 'example.com'), /No DNS records in the file/)
})
