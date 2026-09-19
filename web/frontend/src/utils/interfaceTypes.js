export const fallbackInterfaceTypeItems = [
  { label: '1000BASE-T (1GE)', value: '1000base-t' },
  { label: 'SFP (1GE)', value: '1000base-x-sfp' },
  { label: '10GBASE-T (10GE)', value: '10gbase-t' },
  { label: 'SFP+ (10GE)', value: '10gbase-x-sfpp' },
  { label: 'SFP28 (25GE)', value: '25gbase-x-sfp28' },
  { label: 'QSFP+ (40GE)', value: '40gbase-x-qsfpp' },
  { label: 'QSFP28 (100GE)', value: '100gbase-x-qsfp28' },
  { label: 'LAG', value: 'lag' },
  { label: 'Virtual', value: 'virtual' },
  { label: 'Other', value: 'other' },
]

export function toSelectItems(rows) {
  if (!rows?.length) return fallbackInterfaceTypeItems
  return rows.map((r) => ({
    label: r.label || r.value,
    value: r.value,
  }))
}

export function defaultInterfaceType(rows) {
  const items = toSelectItems(rows)
  return items.find((i) => i.value === '1000base-t')?.value ?? items[0]?.value ?? 'other'
}

export function itemsWithCurrent(rows, current) {
  const items = toSelectItems(rows).slice()
  if (current && !items.some((i) => i.value === current)) {
    items.unshift({ label: current, value: current })
  }
  return items
}
