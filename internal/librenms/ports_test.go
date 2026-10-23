package librenms

import (
	"database/sql"
	"testing"

	_ "github.com/glebarez/sqlite"
)

func TestPortsGetNullAlias(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE ports (
		port_id integer,
		device_id integer,
		ifName text,
		ifAlias text,
		ifDescr text,
		"ignore" integer
	)`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.Exec(`INSERT INTO ports (port_id, device_id, ifName, ifAlias, ifDescr, "ignore") VALUES
		(10, 7, 'Gi0/1', NULL, 'GigabitEthernet0/1', 0),
		(11, 7, NULL, NULL, NULL, 1),
		(12, 7, 'Gi0/2', 'uplink', 'GigabitEthernet0/2', 0),
		(13, 8, 'other', NULL, 'other', 0)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	client := &LibrenmsClient{DB: db}
	ports, err := client.PortsGet(7)
	if err != nil {
		t.Fatalf("PortsGet: %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("got %d ports, want 3", len(ports))
	}

	if ports[0].PortID != 10 || ports[0].IfName != "Gi0/1" || ports[0].IfAlias != "" || ports[0].IfDescr != "GigabitEthernet0/1" || ports[0].Ignore != 0 {
		t.Fatalf("null alias row: %+v", ports[0])
	}
	if ports[1].PortID != 11 || ports[1].IfName != "" || ports[1].IfAlias != "" || ports[1].IfDescr != "" || ports[1].Ignore != 1 {
		t.Fatalf("all-null row: %+v", ports[1])
	}
	if ports[2].PortID != 12 || ports[2].IfAlias != "uplink" || ports[2].IfName != "Gi0/2" {
		t.Fatalf("filled row: %+v", ports[2])
	}
}