# internal/cfgmgmt

Configuration management: scope tree (folders, devices, parameter / CLI /
service objects), typed variables, service types, and CLI render/preview.

**How to design a service type** (catalog definition, CLI objects under
`_catalog/cli`, parameter/resource objects, instantiate in the tree):
[docs/cfgmgmt-service-design.md](../../docs/cfgmgmt-service-design.md).

**Tree architecture** (kinds, wrap policy, refs):
[docs/cfgmgmt-tree-objects.md](../../docs/cfgmgmt-tree-objects.md).

**Definition product** (no built-in types; goose 00004 wipe):
[docs/cfgmgmt-service-definitions.md](../../docs/cfgmgmt-service-definitions.md).
