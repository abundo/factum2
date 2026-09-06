const deviceVars = [
  { name: '.Device.Name', type: 'string', description: 'Factum device name (not always an FQDN)' },
  {
    name: '.Device.PrimaryIPv4',
    type: 'string',
    description: 'Primary IPv4 as CIDR (e.g. 10.0.0.1/24). Use ip to strip the prefix.',
  },
  { name: '.Device.PrimaryIPv6', type: 'string', description: 'Primary IPv6 as CIDR, if set' },
  {
    name: '.Device.Comments',
    type: 'string',
    description: 'Already escaped for Icinga double-quoted strings',
  },
  { name: '.Device.Manufacturer', type: 'string', description: 'Hardware manufacturer' },
  { name: '.Device.ModelName', type: 'string', description: 'Device model' },
  { name: '.Device.Platform', type: 'string', description: 'NOS platform (eos, sros, ios-xr, …)' },
  { name: '.Device.Role', type: 'string', description: 'Device role' },
  { name: '.Device.Site', type: 'string', description: 'Site name' },
  { name: '.Device.Status', type: 'string', description: 'NetBox / factum status' },
  { name: '.Device.Enabled', type: 'bool', description: 'Whether the device is enabled in factum' },
  { name: '.Device.CfLocation', type: 'string', description: 'Custom field: location' },
  {
    name: '.Device.CfAlarmDestination',
    type: 'string',
    description: 'Alarm destination email, if set',
  },
  { name: '.Device.CfAlarmTimeperiod', type: 'string', description: 'Alarm timeperiod, if set' },
  {
    name: '.Device.CfBackupOxidized',
    type: 'bool',
    description: 'Whether Oxidized backup is enabled',
  },
]

const icingaDeviceFunctions = [
  {
    name: 'ip',
    args: 'cidr',
    insert: '{{ ip .Device.PrimaryIPv4 }}',
    description: 'Strip /prefixlen from a CIDR address (PrimaryIPv4 / PrimaryIPv6)',
  },
  {
    name: 'fqdn',
    args: 'name',
    insert: '{{ fqdn .Device.Name }}',
    description:
      'Append Settings.DefaultDomain if name has no dot; Icinga host objects must be FQDNs',
  },
]

export const icingaHostTemplateSchema = {
  notes:
    'Rendered once per Icinga-monitored device that is enabled, has a primary IPv4, and is not on the ignore list. Output is Icinga 2 DSL. Names are Go struct fields (PascalCase), not JSON keys.',
  variables: [
    {
      name: '.Device',
      type: 'Device',
      description: 'The factum device being written as an Icinga Host',
    },
    ...deviceVars,
    {
      name: '.Options',
      type: 'string',
      description:
        'Pre-built Icinga vars lines (alarm destination, default notification, timeperiod, oxidized). Insert as a raw block, not inside quotes.',
    },
  ],
  functions: icingaDeviceFunctions,
}

export const icingaDefaultNotificationSchema = {
  notes:
    'Rendered for each Icinga-monitored device that has no alarm destination. Output is Icinga 2 DSL lines inserted into the host object via .Options (typically vars.pe_*). Literal lines with no {{ }} still work. Names are Go struct fields (PascalCase), not JSON keys.',
  variables: [
    {
      name: '.Device',
      type: 'Device',
      description: 'The factum device that has no CfAlarmDestination',
    },
    ...deviceVars,
  ],
  functions: icingaDeviceFunctions,
}

export const icingaUserTemplateSchema = {
  notes:
    'Rendered once per alarm-destination email collected from devices. All three fields are that email address today. Output is Icinga 2 DSL.',
  variables: [
    {
      name: '.Username',
      type: 'string',
      description: 'Icinga user object name (the destination email)',
    },
    { name: '.DisplayName', type: 'string', description: 'Display name (same as the email today)' },
    { name: '.Email', type: 'string', description: 'Notification email address' },
  ],
  functions: [],
}

export const icingaDependencyTemplateSchema = {
  notes:
    'Stored as a Go text/template, but Icinga sync does not currently render it (devices have no parent list to build dependencies from).',
  variables: [],
  functions: [],
}

const cfgmgmtDeviceVars = [
  { name: '.Device.Name', type: 'string', description: 'Factum device name' },
  { name: '.Device.Platform', type: 'string', description: 'NOS platform (eos, sros, ios-xr, …)' },
  { name: '.Device.Site', type: 'string', description: 'Site name' },
  { name: '.Device.Role', type: 'string', description: 'Device role' },
  { name: '.Device.ModelName', type: 'string', description: 'Device model' },
  { name: '.Device.Manufacturer', type: 'string', description: 'Hardware manufacturer' },
  { name: '.Device.Status', type: 'string', description: 'NetBox / factum status' },
  {
    name: '.Device.PrimaryIPv4',
    type: 'string',
    description: 'Primary IPv4 as CIDR (e.g. 10.0.0.1/24)',
  },
  { name: '.Device.PrimaryIPv6', type: 'string', description: 'Primary IPv6 as CIDR, if set' },
]

const cfgmgmtFunctions = [
  {
    name: 'join',
    args: 'sep list',
    insert: '{{ join "," .Interface.Addresses }}',
    description: 'Join strings (strings.Join)',
  },
  {
    name: 'include',
    args: '"macro-name"',
    insert: '{{ include "macro-name" }}',
    description: 'Body of a ConfigMacro. Nested at most 8 deep. Same data as the caller.',
  },
  {
    name: 'eq',
    args: 'a b',
    insert: '{{ if eq .X .Y }}{{ end }}',
    description: 'Equality via fmt.Sprint, so 1 and "1" compare equal',
  },
  {
    name: 'ne',
    args: 'a b',
    insert: '{{ if ne .X .Y }}{{ end }}',
    description: 'Inequality via fmt.Sprint',
  },
]

const cfgmgmtVarsNote = {
  name: '.Vars',
  type: 'map',
  insert: '{{ index .Vars "name" }}',
  description: 'Resolved config variables. Use {{index .Vars "name"}}, not .Vars.name.',
}

export const cfgmgmtPackSchema = {
  notes:
    'CLI object feature blob, rendered per endpoint. One CLI command per line (blank lines dropped). missingkey=error — guard optional fields with {{if}}. .Vars is a map: {{index .Vars "mtu"}}. ELINE-only fields (.Remote, .PeerLocal*, .SDPID, .StaleSubinterfaces) are zero on other types. Teardown goes in the feature remove blob (or {{define "cleanup"}} inside add).',
  functions: cfgmgmtFunctions,
  variables: [
    { name: '.Name', type: 'string', description: 'Service.ServiceID (e.g. CN00012)' },
    {
      name: '.Description',
      type: 'string',
      description: 'Service.Comment (ELINE: "ID=<ServiceID> <customer>")',
    },
    {
      name: '.ServiceNumericID',
      type: 'int',
      description: 'Service.PseudowireID, else Fields["service_numeric_id"]',
    },
    {
      name: '.Fields',
      type: 'map',
      description: 'Service.Fields (per-service schema values). Access as .Fields.name.',
    },
    { name: '.Endpoint.Role', type: 'string', description: "This termination's role" },
    { name: '.Endpoint.DeviceID', type: 'uint', description: "This termination's device id" },
    {
      name: '.Endpoint.InterfaceID',
      type: 'uint',
      description: "This termination's interface id",
    },
    {
      name: '.Endpoint.Fields',
      type: 'map',
      description: "This termination's fields. Access as .Endpoint.Fields.name.",
    },
    cfgmgmtVarsNote,
    { name: '.Device', type: 'DCIMDevice', description: 'Read-only inventory for this endpoint' },
    ...cfgmgmtDeviceVars,
    { name: '.Interface', type: 'DCIMInterface', description: 'Read-only interface inventory' },
    { name: '.Interface.Name', type: 'string', description: 'Interface name' },
    { name: '.Interface.Description', type: 'string', description: 'Interface description' },
    { name: '.Interface.Enabled', type: 'bool', description: 'Whether the interface is enabled' },
    { name: '.Interface.Type', type: 'string', description: 'Interface type' },
    { name: '.LocalIface', type: 'string', description: 'Interface.Name' },
    {
      name: '.LocalVLAN',
      type: 'int',
      description: 'Endpoint.Fields["vlan"], else 0. Name the VLAN field vlan to populate this.',
    },
    { name: '.Role', type: 'string', description: 'Same as Endpoint.Role' },
    {
      name: '.PeerLocalIface',
      type: 'string',
      description: 'ELINE: other endpoint on the same device',
    },
    { name: '.PeerLocalVLAN', type: 'int', description: 'ELINE: VLAN of the same-device peer' },
    {
      name: '.Remote',
      type: '*ELINERemote',
      description: 'ELINE: other endpoint on a different device. Guard with {{if .Remote}}.',
    },
    { name: '.Remote.NeighborIP', type: 'string', description: 'ELINE: peer loopback address' },
    { name: '.Remote.PseudowireID', type: 'int', description: 'ELINE: Service.PseudowireID' },
    { name: '.Remote.MTU', type: 'int', description: 'ELINE: pseudowire MTU' },
    { name: '.Remote.ControlWord', type: 'bool', description: 'ELINE: control-word flag' },
    { name: '.Remote.DeviceName', type: 'string', description: 'ELINE: peer device name' },
    { name: '.Remote.RemoteIface', type: 'string', description: 'ELINE: peer interface name' },
    { name: '.Remote.RemoteVLAN', type: 'int', description: 'ELINE: peer VLAN' },
    {
      name: '.SDPID',
      type: 'int',
      description: 'ELINE: SR OS shared SDP id from neighbor last octet',
    },
    {
      name: '.StaleSubinterfaces',
      type: '[]ELINEStale',
      insert: '{{ range .StaleSubinterfaces }}\nno interface {{ .Iface }}.{{ .VLAN }}\n{{ end }}',
      description: 'ELINE leftover subinterfaces from a previous apply (.Iface, .VLAN)',
    },
  ],
}

export const cfgmgmtMacroSchema = {
  notes:
    'Inserted with {{include "name"}} from a CLI feature. Same data as the caller (service-translation CLI objects pass GenericRenderData; baseline CLI objects pass .Name / .Device / .Vars). Nested at most 8 deep.',
  functions: cfgmgmtFunctions,
  variables: cfgmgmtPackSchema.variables,
}

export const cfgmgmtBaselineSchema = {
  notes:
    'Golden/baseline CLI object for a device. Rendered with .Name, .Device, and .Vars. Interface-parented objects also see .Interface and .LocalIface. Does not see service endpoints. One CLI command per line (blank lines dropped). .Vars is a map: {{index .Vars "mtu"}}, not .Vars.mtu.',
  functions: cfgmgmtFunctions,
  variables: [
    { name: '.Name', type: 'string', description: 'Device name' },
    { name: '.Device', type: 'DCIMDevice', description: 'Read-only inventory for this device' },
    ...cfgmgmtDeviceVars,
    {
      name: '.Interface',
      type: 'DCIMInterface',
      description: 'Read-only interface inventory when this CLI object is under an interface',
    },
    {
      name: '.Interface.Name',
      type: 'string',
      description: 'Interface name (empty at device/folder)',
    },
    { name: '.LocalIface', type: 'string', description: 'Interface.Name when parent is an interface' },
    cfgmgmtVarsNote,
  ],
}

export function withCfgmgmtContext(
  schema,
  { macros = [], variables = [], serviceType = null } = {},
) {
  const functions = [...(schema.functions ?? [])]
  const seenMacro = new Set()
  for (const macro of macros) {
    if (!macro?.name || seenMacro.has(macro.name)) continue
    seenMacro.add(macro.name)
    functions.push({
      name: 'include',
      args: JSON.stringify(macro.name),
      insert: `{{ include ${JSON.stringify(macro.name)} }}`,
      description: `Insert ConfigMacro ${macro.name}`,
    })
  }

  const vars = [...(schema.variables ?? [])]
  for (const variable of variables) {
    if (!variable?.name) continue
    vars.push({
      name: `index .Vars ${JSON.stringify(variable.name)}`,
      type: variable.type || '',
      insert: `{{ index .Vars ${JSON.stringify(variable.name)} }}`,
      description: variable.description || `Config variable ${variable.name}`,
    })
  }
  for (const field of serviceType?.schema ?? []) {
    if (!field?.name) continue
    vars.push({
      name: `.Fields.${field.name}`,
      type: field.type || '',
      description: field.description || `Service field ${field.name}`,
    })
  }
  const seenEndpointField = new Set()
  for (const role of serviceType?.endpoint_roles ?? []) {
    for (const field of role.fields ?? []) {
      if (!field?.name || seenEndpointField.has(field.name)) continue
      seenEndpointField.add(field.name)
      vars.push({
        name: `.Endpoint.Fields.${field.name}`,
        type: field.type || '',
        description: `${role.name}: ${field.description || field.name}`,
      })
    }
  }
  return { ...schema, functions, variables: vars }
}
