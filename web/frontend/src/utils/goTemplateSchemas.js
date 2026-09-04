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
