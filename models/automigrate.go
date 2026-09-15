package models

import "gorm.io/gorm"

// AutoMigrateAll creates the application schema via GORM. Used only by
// in-memory SQLite tests (util.MigrateDatabase's non-postgres branch).
// Production schema is internal/dbmigrate/sql, not this list.
func AutoMigrateAll(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&User{},
		&Role{},
		&LdapRoleMapping{},
		&PasswordResetToken{},

		&Device{},
		&Interface{},
		&Address{},
		&Tag{},
		&Connection{},
		&Site{},
		&Manufacturer{},
		&DeviceType{},
		&Platform{},
		&OpticalKindMap{},
		&OpticalPort{},
		&OpticalXConnect{},
		&ServicePath{},
		&ServiceHop{},
		&MaintenanceWindow{},
		&MaintenanceResource{},
		&MaintenanceNotification{},
		&CustomerContact{},

		&Customer{},
		&Contact{},
		&Product{},

		&IpamNamespace{},
		&IpamNamespacePrefix{},
		&IpamVRF{},
		&IpamPrefix{},

		&DnsSOATemplate{},
		&DnsDNSSECPolicy{},
		&DnsTemplate{},
		&DnsTemplateNameserver{},
		&DnsZone{},
		&DnsZoneRecord{},

		&Service{},
		&Agreement{},

		&Settings{},
		&LibrenmsPendingDelete{},
		&WorkerNode{},
		&DeviceSyncAuth{},
		&Link{},
		&Job{},
		&JobTask{},
		&JobTaskEvent{},
		&JobSchedule{},

		&ConfigScope{},
		&ConfigCLIFeature{},
		&ConfigVariableDef{},
		&ConfigAssignment{},
		&ServiceType{},
		&ServiceConnectionType{},
		&ConfigMacro{},
		&ServiceEndpoint{},
	); err != nil {
		return err
	}
	// Partial unique indexes GORM tags cannot express. Postgres has these
	// in the goose baseline; sqlite tests still need them here.
	for _, s := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_lime_source_id ON customers (source, source_id) WHERE source = 'lime' AND source_id IS NOT NULL AND source_id != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_services_lime_source_id ON services (source, source_id) WHERE source = 'lime' AND source_id IS NOT NULL AND source_id != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_lime_source_id ON contacts (source, source_id) WHERE source = 'lime' AND source_id IS NOT NULL AND source_id != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_netbox_id_vm ON devices (vm, netbox_id) WHERE netbox_id != 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_manufacturer_model ON device_types (manufacturer_id, model)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_manufacturers_netbox_id ON manufacturers (netbox_id) WHERE netbox_id != 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_netbox_id ON device_types (netbox_id) WHERE netbox_id != 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_platforms_netbox_id ON platforms (netbox_id) WHERE netbox_id != 0`,
		`DROP INDEX IF EXISTS idx_sites_netbox_id`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_netbox_kind_id ON sites (netbox_kind, netbox_id) WHERE netbox_id != 0`,
	} {
		if err := db.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
