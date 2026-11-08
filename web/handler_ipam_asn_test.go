package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestIpamASNAndRouteTargetLists(t *testing.T) {
	ctrl := setupIPAM(t)
	if err := ctrl.DB.Create(&models.IpamASNRange{
		Name: "Private 16-bit", Slug: "private-16-bit", RIR: "RFC 6996", Start: 64512, End: 65534,
		Source: models.VRFSourceNetbox, NetboxID: 4,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ctrl.DB.Create(&models.IpamASN{
		ASN: 65000, RIR: "RFC 6996", Description: "lab", Sites: "STO",
		Source: models.VRFSourceNetbox, NetboxID: 8,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ctrl.DB.Create(&models.IpamRouteTarget{
		Name: "65000:100", Description: "cust", Source: models.VRFSourceNetbox, NetboxID: 3,
	}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/ipam/asn-ranges", nil, nil, nil)
	if err := ctrl.ApiIpamASNRangeList(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("ranges status = %d body=%s", rec.Code, rec.Body.String())
	}
	var ranges []models.IpamASNRange
	if err := json.Unmarshal(rec.Body.Bytes(), &ranges); err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges[0].Name != "Private 16-bit" || ranges[0].End != 65534 {
		t.Fatalf("ranges = %+v", ranges)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/asns", nil, nil, nil)
	if err := ctrl.ApiIpamASNList(c); err != nil {
		t.Fatal(err)
	}
	var asns []models.IpamASN
	if err := json.Unmarshal(rec.Body.Bytes(), &asns); err != nil {
		t.Fatal(err)
	}
	if len(asns) != 1 || asns[0].ASN != 65000 || asns[0].Sites != "STO" {
		t.Fatalf("asns = %+v", asns)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/route-targets", nil, nil, nil)
	if err := ctrl.ApiIpamRouteTargetList(c); err != nil {
		t.Fatal(err)
	}
	var targets []models.IpamRouteTarget
	if err := json.Unmarshal(rec.Body.Bytes(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Name != "65000:100" {
		t.Fatalf("targets = %+v", targets)
	}
}
