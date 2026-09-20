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
    insert: '{{ ip(.Device.PrimaryIPv4) }}',
    description: 'Strip /prefixlen from a CIDR address (PrimaryIPv4 / PrimaryIPv6)',
  },
  {
    name: 'fqdn',
    args: 'name',
    insert: '{{ fqdn(.Device.Name) }}',
    description:
      'Append Settings.DefaultDomain if name has no dot; Icinga host objects must be FQDNs',
  },
]

export const icingaHostTemplateSchema = {
  notes:
    'Rendered once per Icinga-monitored device that is enabled, has a primary IPv4, and is not on the ignore list. Output is Icinga 2 DSL (Jet template, no HTML escaping). Names are Go struct fields (PascalCase), not JSON keys.',
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
    'Rendered for each Icinga-monitored device that has no alarm destination. Output is Icinga 2 DSL lines inserted into the host object via .Options (typically vars.pe_*). Literal lines with no {{ }} still work. Jet template. Names are Go struct fields (PascalCase), not JSON keys.',
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
    'Stored as a Jet template, but Icinga sync does not currently render it (devices have no parent list to build dependencies from).',
  variables: [],
  functions: [],
}

export const icingaCertTemplateExample = `template Service "factum-cert-check" {
  // max_check_attempts = 3
  check_interval = 1d
  retry_interval = 5m
  check_command = "http"
  vars.http_ssl = false
  vars.http_certificate = "20,10"
  vars.http_sni = "true"
}

{{ range .Checks }}
object Service "HTTPS cert - {{ quote(.Domain) }}" {
  import "factum-cert-check"
  host_name = "{{ quote(.Host) }}"
  vars.http_vhost = "{{ quote(.Domain) }}"
}

{{ end }}`

export const icingaCertTemplateSchema = {
  notes:
    'Rendered once per Icinga sync. Output is Icinga 2 DSL. One Service object per concrete certificate name (wildcards are skipped). Certificates with an empty Host are skipped. IP check hosts that are not already Icinga Host objects are written as generic-host objects in the same file.',
  variables: [
    {
      name: '.Checks',
      type: '[]certCheck',
      insert: '{{ range .Checks }}{{ .Domain }}{{ end }}',
      description: 'Every concrete name on every certificate that has a Host',
    },
    {
      name: '.Host',
      type: 'string',
      insert: '{{ quote(.Host) }}',
      description: 'Inside range .Checks: IPv4, IPv6, or hostname to connect to',
    },
    {
      name: '.Domain',
      type: 'string',
      insert: '{{ quote(.Domain) }}',
      description: 'Inside range .Checks: DNS name to present as SNI / http_vhost',
    },
    {
      name: '.CertName',
      type: 'string',
      description: 'Inside range .Checks: Factum certificate name',
    },
  ],
  functions: [
    {
      name: 'quote',
      args: 's',
      insert: '{{ quote(.Domain) }}',
      description: 'Escape backslash, quote, tab, and newline for Icinga double-quoted strings',
    },
  ],
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
    args: 'sep, list',
    insert: '{{ join(",", .Interface.Addresses) }}',
    description: 'Join strings with a separator',
  },
  {
    name: 'include',
    args: '"macro-name"',
    insert: '{{ include "macro-name" }}',
    description: 'Body of a ConfigMacro. Nested at most 8 deep. Same data as the caller.',
  },
  {
    name: 'eq',
    args: 'a, b',
    insert: '{{ if eq(.X, .Y) }}{{ end }}',
    description: 'Equality via fmt.Sprint, so 1 and "1" compare equal. Prefer .X == .Y.',
  },
  {
    name: 'ne',
    args: 'a, b',
    insert: '{{ if ne(.X, .Y) }}{{ end }}',
    description: 'Inequality via fmt.Sprint. Prefer .X != .Y.',
  },
  {
    name: 'sdpid',
    args: 'neighborIP',
    insert: '{{ sdpid(.Others[0].NeighborIP) }}',
    description: 'SR OS SDP id from a neighbor IPv4 last octet. Errors on empty/non-IPv4.',
  },
  {
    name: 'macColon',
    args: 'mac',
    insert: '{{ macColon(.) }}',
    description: 'Format a MAC as aa:bb:cc:dd:ee:ff',
  },
  {
    name: 'macHyphen',
    args: 'mac',
    insert: '{{ macHyphen(.) }}',
    description: 'Format a MAC as aa-bb-cc-dd-ee-ff',
  },
  {
    name: 'macCisco',
    args: 'mac',
    insert: '{{ macCisco(.) }}',
    description: 'Format a MAC as aabb.ccdd.eeff',
  },
]

const cfgmgmtVarsNote = {
  name: '.Vars',
  type: 'map',
  insert: '{{ .Vars.name }}',
  description: 'Resolved config variables. Use .Vars.name or .Vars["name"].',
}

export const cfgmgmtPackSchema = {
  notes:
    'CLI object feature blob, rendered per endpoint. One CLI command per line (blank lines dropped). Missing struct fields error; missing map keys are empty — guard with {{ if }} or isset. Peers are .Others[0]. Same-device peers have empty NeighborIP. Teardown goes in the feature remove blob (or {{ block cleanup() }} inside add).',
  functions: cfgmgmtFunctions,
  variables: [
    { name: '.Name', type: 'string', description: 'Service.ServiceID (e.g. CN00012)' },
    {
      name: '.Description',
      type: 'string',
      description: 'Service.Comment',
    },
    {
      name: '.ServiceNumericID',
      type: 'int',
      description: 'Service.PseudowireID, else Fields["service_numeric_id"]',
    },
    {
      name: '.ConnectionType',
      type: 'string',
      description: 'Chosen connection type name (empty if none)',
    },
    {
      name: '.Fields',
      type: 'map',
      description: 'Service.Fields (per-service schema values). Access as .Fields.name.',
    },
    {
      name: '.FieldMeta',
      type: 'map',
      insert: '{{ .FieldMeta.bandwidth_mbps.Unit }}',
      description: 'Definition metadata by field name (Name, Type, Unit, Description).',
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
      name: '.Current',
      type: 'RenderEndpoint',
      description:
        'This termination (Device, Interface, LocalIface, Fields, Commercial, NeighborIP)',
    },
    {
      name: '.Current.Fields',
      type: 'map',
      insert: '{{ .Current.Fields.vlan }}',
      description: "This termination's fields after same-name service defaults are filled",
    },
    {
      name: '.Interfaces',
      type: '[]RenderEndpoint',
      insert: '{{ range .Interfaces }}{{ .LocalIface }}{{ end }}',
      description: 'All homogeneous UNIs including Current',
    },
    {
      name: '.Others',
      type: '[]RenderEndpoint',
      insert: '{{ .Others[0].NeighborIP }}',
      description:
        'Interfaces without Current. NeighborIP is the peer loopback when the peer device differs; empty for same-device peers.',
    },
  ],
}

export const cfgmgmtMacroSchema = {
  notes:
    'Inserted with {{ include "name" }} from a CLI feature. Same data as the caller (service-translation CLI objects pass GenericRenderData; baseline CLI objects pass .Name / .Device / .Vars). Nested at most 8 deep.',
  functions: cfgmgmtFunctions,
  variables: cfgmgmtPackSchema.variables,
}

export const cfgmgmtBaselineSchema = {
  notes:
    'Golden/baseline CLI object for a device. Rendered with .Name, .Device, and .Vars. Interface-parented objects also see .Interface and .LocalIface. Does not see service endpoints. One CLI command per line (blank lines dropped). .Vars is a map: {{ .Vars.mtu }}.',
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
    {
      name: '.LocalIface',
      type: 'string',
      description: 'Interface.Name when parent is an interface',
    },
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
      name: `.Vars.${variable.name}`,
      type: variable.type || '',
      insert: `{{ .Vars[${JSON.stringify(variable.name)}] }}`,
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
  for (const field of serviceType?.interfaces?.fields ?? []) {
    if (!field?.name || seenEndpointField.has(field.name)) continue
    seenEndpointField.add(field.name)
    vars.push({
      name: `.Current.Fields.${field.name}`,
      type: field.type || '',
      insert: `{{ .Current.Fields[${JSON.stringify(field.name)}] }}`,
      description: field.description || field.name,
    })
  }
  const seenMeta = new Set()
  for (const field of [
    ...(serviceType?.schema ?? []),
    ...(serviceType?.interfaces?.fields ?? []),
  ]) {
    if (!field?.name || seenMeta.has(field.name)) continue
    seenMeta.add(field.name)
    vars.push({
      name: `.FieldMeta.${field.name}`,
      type: 'FieldMeta',
      insert: `{{ .FieldMeta[${JSON.stringify(field.name)}].Unit }}`,
      description: field.unit
        ? `Definition metadata for ${field.name} (unit ${field.unit})`
        : `Definition metadata for ${field.name}`,
    })
  }
  return { ...schema, functions, variables: vars }
}
