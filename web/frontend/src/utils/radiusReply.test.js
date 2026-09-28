import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseReply, serializeReply } from './radiusReply.js'

const sample = `# Smartoptics (vendor 30826, attribute 1, string)
Smartoptics-Userrole1 = "admin"
# Service-Type = Administrative-User
Service-Type = NAS-Prompt-User
# Cisco login enable
Cisco-AVPair = "shell:priv-lvl=15"
# Arista login enable
Arista-AVPair = "shell:priv-lvl=15"
Arista-AVPair = "shell:roles=network-admin"
# Nokia SR OS. "both" is console and ftp. 4 adds netconf.
Timetra-Access = both
Timetra-Access = 4
# Timetra-Access = console
Timetra-Profile = "administrative"
Timetra-Default-Action = permit-all
MikroTik-Group = "write"
# Huawei login enable. Privilege is an integer, 0-15.
Huawei-Exec-Privilege = 3
`

test('leading notes land in the comment column and # assignments stay off', () => {
  const rows = parseReply(sample)
  assert.equal(rows[0].name, 'Smartoptics-Userrole1')
  assert.equal(rows[0].value, 'admin')
  assert.equal(rows[0].quoted, true)
  assert.equal(rows[0].enabled, true)
  assert.equal(rows[0].comment, 'Smartoptics (vendor 30826, attribute 1, string)')

  assert.equal(rows[1].name, 'Service-Type')
  assert.equal(rows[1].value, 'Administrative-User')
  assert.equal(rows[1].enabled, false)

  assert.equal(rows[2].name, 'Service-Type')
  assert.equal(rows[2].value, 'NAS-Prompt-User')
  assert.equal(rows[2].enabled, true)
  assert.equal(rows[2].comment, '')

  const huawei = rows.at(-1)
  assert.equal(huawei.name, 'Huawei-Exec-Privilege')
  assert.equal(huawei.value, '3')
  assert.equal(huawei.quoted, false)
  assert.match(huawei.comment, /Privilege is an integer/)
  assert.deepEqual(parseReply(serializeReply(rows)), rows)
})

test('trailing comments round-trip and a note row stays a note', () => {
  const rows = [
    {
      enabled: true,
      name: 'Cisco-AVPair',
      op: '=',
      value: 'shell:priv-lvl=15',
      quoted: true,
      comment: 'Cisco login enable',
    },
    {
      enabled: false,
      name: 'Service-Type',
      op: '=',
      value: 'Administrative-User',
      quoted: false,
      comment: 'kept off',
    },
    { enabled: false, name: '', op: '=', value: '', quoted: false, comment: 'operators only' },
    {
      enabled: true,
      name: '26.30826.2',
      op: '=',
      value: 'operator',
      quoted: true,
      comment: '',
    },
  ]
  const again = parseReply(serializeReply(rows))
  assert.deepEqual(again, rows)
})

test('a hash inside a quoted value stays in the value', () => {
  const rows = parseReply('Cisco-AVPair = "shell:priv-lvl=15 #keep" # real note')
  assert.equal(rows.length, 1)
  assert.equal(rows[0].value, 'shell:priv-lvl=15 #keep')
  assert.equal(rows[0].comment, 'real note')
})

test('empty text is no rows and blank rows serialize to nothing', () => {
  assert.deepEqual(parseReply(''), [])
  assert.deepEqual(parseReply(null), [])
  assert.equal(
    serializeReply([{ enabled: true, name: '  ', op: '=', value: '', quoted: false, comment: '' }]),
    '',
  )
})
