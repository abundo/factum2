package dcim

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestConnectionGraphExternalStub(t *testing.T) {
	db := newTestDB(t)
	site := models.Site{Name: "A", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	a := models.Device{Name: "in-scope", SiteID: site.ID}
	b := models.Device{Name: "outside", SiteID: 0}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Eth1"}
	ib := models.Interface{DeviceID: b.ID, Name: "Eth2"}
	db.Create(&ia)
	db.Create(&ib)
	db.Create(&models.Connection{
		DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID, Label: "trunk",
	})
	g, err := ConnectionGraph(db, GraphQuery{SiteID: site.ID, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("edges %+v", g.Edges)
	}
	ext := 0
	for _, n := range g.Nodes {
		if n.External {
			ext++
			if n.Name != "outside" {
				t.Fatalf("stub %+v", n)
			}
		}
	}
	if ext != 1 {
		t.Fatalf("want 1 external node, got %+v", g.Nodes)
	}
}
