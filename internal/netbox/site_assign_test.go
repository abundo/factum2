package netbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type fakeSiteNetbox struct {
	sitesByName map[string]netboxtool.NetboxNamedRef
	sitesBySlug map[string]netboxtool.NetboxNamedRef
	sites       map[uint]netboxtool.NetboxSiteREST
	nextID      uint
	created     []map[string]any
	patchedSite map[uint]map[string]any
	patchedDev  map[uint]map[string]any
	patchedVM   map[uint]map[string]any
}

func newFakeSiteNetbox() *fakeSiteNetbox {
	return &fakeSiteNetbox{
		sitesByName: map[string]netboxtool.NetboxNamedRef{},
		sitesBySlug: map[string]netboxtool.NetboxNamedRef{},
		sites:       map[uint]netboxtool.NetboxSiteREST{},
		nextID:      10,
		patchedSite: map[uint]map[string]any{},
		patchedDev:  map[uint]map[string]any{},
		patchedVM:   map[uint]map[string]any{},
	}
}

func (f *fakeSiteNetbox) addSite(id uint, name, slug string, lat, lng *float64) {
	ref := netboxtool.NetboxNamedRef{ID: id, Name: name, Slug: slug}
	f.sitesByName[name] = ref
	f.sitesBySlug[slug] = ref
	f.sites[id] = netboxtool.NetboxSiteREST{ID: id, Name: name, Slug: slug, Latitude: lat, Longitude: lng}
}

func (f *fakeSiteNetbox) client(t *testing.T) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func (f *fakeSiteNetbox) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/dcim/sites/":
		if name := r.URL.Query().Get("name"); name != "" {
			if ref, ok := f.sitesByName[name]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []netboxtool.NetboxNamedRef{ref}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			return
		}
		if slug := r.URL.Query().Get("slug"); slug != "" {
			if ref, ok := f.sitesBySlug[slug]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []netboxtool.NetboxNamedRef{ref}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/dcim/sites/"):
		id := pathID(r.URL.Path)
		site, ok := f.sites[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(site)
	case r.Method == http.MethodPost && r.URL.Path == "/api/dcim/sites/":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.created = append(f.created, body)
		id := f.nextID
		f.nextID++
		name, _ := body["name"].(string)
		slug, _ := body["slug"].(string)
		lat := floatFrom(body["latitude"])
		lng := floatFrom(body["longitude"])
		f.addSite(id, name, slug, lat, lng)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.sites[id])
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/sites/"):
		id := pathID(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.patchedSite[id] = body
		site := f.sites[id]
		if v := floatFrom(body["latitude"]); v != nil {
			site.Latitude = v
		}
		if v := floatFrom(body["longitude"]); v != nil {
			site.Longitude = v
		}
		f.sites[id] = site
		_ = json.NewEncoder(w).Encode(site)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/devices/"):
		id := pathID(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.patchedDev[id] = body
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/virtualization/virtual-machines/"):
		id := pathID(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.patchedVM[id] = body
		_ = json.NewEncoder(w).Encode(body)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
	}
}

func pathID(p string) uint {
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	var id uint
	fmtSscanf(parts[len(parts)-1], &id)
	return id
}

func fmtSscanf(s string, id *uint) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*id = uint(n)
}

func floatFrom(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	default:
		return nil
	}
}

func ptr[T any](v T) *T { return &v }

func TestAssignDeviceLocation_CreateSite(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	fake := newFakeSiteNetbox()
	got, err := AssignDeviceLocation(db, fake.client(t), dev, AssignLocationInput{
		SiteName:  "Stockholm",
		Latitude:  ptr(59.3),
		Longitude: ptr(18.0),
	})
	if err != nil {
		t.Fatalf("AssignDeviceLocation: %v", err)
	}
	if len(fake.created) != 1 {
		t.Fatalf("created sites = %d, want 1", len(fake.created))
	}
	if fake.patchedDev[42]["site"] != float64(10) {
		t.Errorf("device site patch = %v, want 10", fake.patchedDev[42]["site"])
	}
	if got.Site.Name != "Stockholm" || got.Site.NetboxID != 10 {
		t.Errorf("site = %+v", got.Site)
	}
	if got.Device.Site != "Stockholm" || got.Device.SiteID != 10 {
		t.Errorf("device site = %q / %d", got.Device.Site, got.Device.SiteID)
	}
	if got.Device.Latitude == nil || *got.Device.Latitude != 59.3 {
		t.Errorf("device lat = %v", got.Device.Latitude)
	}
}

func TestAssignDeviceLocation_PhysicalAddress(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	fake := newFakeSiteNetbox()
	addr := "1 Drottninggatan\n111 51 Stockholm\nSweden"
	if _, err := AssignDeviceLocation(db, fake.client(t), dev, AssignLocationInput{
		SiteName:        "Stockholm",
		Latitude:        ptr(59.3),
		Longitude:       ptr(18.0),
		PhysicalAddress: addr,
	}); err != nil {
		t.Fatal(err)
	}
	if fake.created[0]["physical_address"] != addr {
		t.Errorf("created physical_address = %v", fake.created[0]["physical_address"])
	}

	fake.addSite(4, "STO", "sto", nil, nil)
	if _, err := AssignDeviceLocation(db, fake.client(t), dev, AssignLocationInput{
		SiteName:        "STO",
		Latitude:        ptr(59.4),
		Longitude:       ptr(18.1),
		PhysicalAddress: addr,
	}); err != nil {
		t.Fatal(err)
	}
	if fake.patchedSite[4]["physical_address"] != addr {
		t.Errorf("patched physical_address = %v", fake.patchedSite[4])
	}
}

func TestAssignDeviceLocation_ExistingSite(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	fake := newFakeSiteNetbox()
	fake.addSite(4, "STO", "sto", ptr(59.3), ptr(18.0))
	got, err := AssignDeviceLocation(db, fake.client(t), dev, AssignLocationInput{SiteName: "STO"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.created) != 0 {
		t.Errorf("created = %d, want 0", len(fake.created))
	}
	if fake.patchedDev[42]["site"] != float64(4) {
		t.Errorf("device site patch = %v", fake.patchedDev[42])
	}
	if got.Site.NetboxID != 4 || got.Device.SiteID != 4 {
		t.Errorf("site/device ids = %d / %d", got.Site.NetboxID, got.Device.SiteID)
	}
}

func TestAssignDeviceLocation_SetCoordsOnExistingSite(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, Site: "STO", SiteID: 4, CfSource: "netbox"}
	sib := models.Device{Name: "sw1", NetboxID: 43, Site: "STO", SiteID: 4, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&sib).Error; err != nil {
		t.Fatal(err)
	}
	fake := newFakeSiteNetbox()
	fake.addSite(4, "STO", "sto", nil, nil)
	got, err := AssignDeviceLocation(db, fake.client(t), dev, AssignLocationInput{
		SiteName:  "STO",
		Latitude:  ptr(59.4),
		Longitude: ptr(18.1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.patchedSite[4]["latitude"] != 59.4 {
		t.Errorf("site patch = %v", fake.patchedSite[4])
	}
	if got.Site.Latitude != 59.4 {
		t.Errorf("local site lat = %v", got.Site.Latitude)
	}
	var sibling models.Device
	if err := db.First(&sibling, sib.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sibling.Latitude == nil || *sibling.Latitude != 59.4 {
		t.Errorf("sibling lat = %v, want inherited 59.4", sibling.Latitude)
	}
}

func TestAssignDeviceLocation_RejectsVM(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "vm1", NetboxID: 7, VM: true, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	_, err := AssignDeviceLocation(db, newFakeSiteNetbox().client(t), dev, AssignLocationInput{
		SiteName: "STO", Latitude: ptr(59.3), Longitude: ptr(18.0),
	})
	if !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("err = %v, want ErrInvalidLocation", err)
	}
}

func TestAssignDeviceLocation_RejectsDefault(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	_, err := AssignDeviceLocation(db, newFakeSiteNetbox().client(t), dev, AssignLocationInput{SiteName: "Default"})
	if !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("err = %v, want ErrInvalidLocation", err)
	}
}

func TestAssignDeviceLocation_CreateRequiresCoords(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	_, err := AssignDeviceLocation(db, newFakeSiteNetbox().client(t), dev, AssignLocationInput{SiteName: "STO"})
	if !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("err = %v, want ErrInvalidLocation", err)
	}
}

func TestAssignDeviceLocation_NotInNetbox(t *testing.T) {
	db := newImportTestDB(t)
	dev := models.Device{Name: "manual"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	_, err := AssignDeviceLocation(db, newFakeSiteNetbox().client(t), dev, AssignLocationInput{
		SiteName: "STO", Latitude: ptr(1.0), Longitude: ptr(2.0),
	})
	if !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("err = %v, want ErrInvalidLocation", err)
	}
}

func TestSiteSlug(t *testing.T) {
	if got := siteSlug("Stockholm DC"); got != "stockholm-dc" {
		t.Errorf("siteSlug = %q", got)
	}
	if got := siteSlug("***"); got != "site" {
		t.Errorf("empty slug = %q", got)
	}
}

func TestUniqueSiteSlug_Disambiguates(t *testing.T) {
	fake := newFakeSiteNetbox()
	fake.addSite(1, "Other", "stockholm", nil, nil)
	slug, err := uniqueSiteSlug(fake.client(t), "Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	if slug != "stockholm-2" {
		t.Errorf("slug = %q, want stockholm-2", slug)
	}
}
