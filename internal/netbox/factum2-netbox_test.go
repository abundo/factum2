package netbox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

func TestSyncDevicePreservesOpticalKindWithoutCF(t *testing.T) {
	db := newImportTestDB(t)
	id, _ := seedDeviceWithIfaces(t, db, "roadm-a", 55, nil)
	if err := db.Model(&models.Device{}).Where("id = ?", id).Update("optical_kind", models.OpticalKindROADM).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := syncDevice(db, &netboxtool.NBDevice{
		NetboxID: 55, Name: "roadm-a", Role: "router", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	var d models.Device
	if err := db.First(&d, id).Error; err != nil {
		t.Fatal(err)
	}
	if d.OpticalKind != models.OpticalKindROADM {
		t.Errorf("optical_kind = %q, want preserved %q", d.OpticalKind, models.OpticalKindROADM)
	}
}

func TestUpsertOpticalPortRole_CreatesAndPreservesFreq(t *testing.T) {
	db := newImportTestDB(t)
	_, ifaceIDs := seedDeviceWithIfaces(t, db, "roadm1", 1, []models.Interface{
		{NetboxID: 10, Name: "LINE-1"},
	})
	id := ifaceIDs["LINE-1"]

	if err := upsertOpticalPortRole(db, id, models.PortROADMDegree); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.OpticalPort{}).Where("interface_id = ?", id).Update("freq_hz", uint64(193100000000000)).Error; err != nil {
		t.Fatal(err)
	}
	if err := upsertOpticalPortRole(db, id, models.PortROADMAddDrop); err != nil {
		t.Fatal(err)
	}
	var port models.OpticalPort
	if err := db.Where("interface_id = ?", id).First(&port).Error; err != nil {
		t.Fatal(err)
	}
	if port.Role != models.PortROADMAddDrop {
		t.Errorf("role = %q, want %s", port.Role, models.PortROADMAddDrop)
	}
	if port.FreqHz != 193100000000000 {
		t.Errorf("freq_hz = %d, want preserved", port.FreqHz)
	}
}

func TestDeleteDeviceByNetboxID_RemovesDeviceAndChildren(t *testing.T) {
	db := newImportTestDB(t)

	deviceID, ifaceIDs := seedDeviceWithIfaces(t, db, "rtr1", 42, []models.Interface{
		{NetboxID: 100, Name: "eth0"},
		{NetboxID: 101, Name: "eth1"},
	})
	eth0ID := ifaceIDs["eth0"]
	eth1ID := ifaceIDs["eth1"]

	if err := db.Create(&models.Address{InterfaceID: eth0ID, NetboxID: 200, Address: "10.0.0.1/24"}).Error; err != nil {
		t.Fatalf("create address: %v", err)
	}
	devID := deviceID
	if err := db.Create(&models.Tag{DeviceID: &devID, NetboxID: 300, Name: "core"}).Error; err != nil {
		t.Fatalf("create device tag: %v", err)
	}
	if err := db.Create(&models.Tag{InterfaceID: &eth0ID, NetboxID: 301, Name: "uplink"}).Error; err != nil {
		t.Fatalf("create iface tag: %v", err)
	}
	if err := db.Create(&models.Connection{
		NetboxID:     400,
		DeviceAID:    deviceID,
		InterfaceAID: eth0ID,
		DeviceBID:    deviceID,
		InterfaceBID: eth1ID,
		Label:        "loop",
	}).Error; err != nil {
		t.Fatalf("create connection: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 42, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	if !gone[models.Device](t, db, deviceID) {
		t.Error("device row still present")
	}
	if !gone[models.Interface](t, db, eth0ID) || !gone[models.Interface](t, db, eth1ID) {
		t.Error("interface rows still present")
	}
	var addresses int64
	if err := db.Model(&models.Address{}).Count(&addresses).Error; err != nil {
		t.Fatalf("count addresses: %v", err)
	}
	if addresses != 0 {
		t.Errorf("addresses remaining = %d, want 0", addresses)
	}
	var tags int64
	if err := db.Model(&models.Tag{}).Count(&tags).Error; err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tags != 0 {
		t.Errorf("tags remaining = %d, want 0", tags)
	}
	var conns int64
	if err := db.Model(&models.Connection{}).Count(&conns).Error; err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if conns != 0 {
		t.Errorf("connections remaining = %d, want 0", conns)
	}
}

func TestDeleteDeviceByNetboxID_NoopIfMissing(t *testing.T) {
	db := newImportTestDB(t)

	n, err := DeleteDeviceByNetboxID(db, 99, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
}

func TestDeleteDeviceByNetboxID_LeavesNonNetboxSource(t *testing.T) {
	db := newImportTestDB(t)

	d := models.Device{Name: "manual", NetboxID: 42, CfSource: "manual"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 42, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	if gone[models.Device](t, db, d.ID) {
		t.Error("non-netbox device was deleted")
	}
}

func TestDeleteDeviceByNetboxID_DoesNotCrossVMBoundary(t *testing.T) {
	db := newImportTestDB(t)

	phys := models.Device{Name: "rtr1", NetboxID: 7, VM: false, CfSource: "netbox"}
	vm := models.Device{Name: "vm1", NetboxID: 7, VM: true, CfSource: "netbox"}
	if err := db.Create(&phys).Error; err != nil {
		t.Fatalf("create physical: %v", err)
	}
	if err := db.Create(&vm).Error; err != nil {
		t.Fatalf("create vm: %v", err)
	}

	n, err := DeleteDeviceByNetboxID(db, 7, false)
	if err != nil {
		t.Fatalf("DeleteDeviceByNetboxID: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}
	if !gone[models.Device](t, db, phys.ID) {
		t.Error("physical device still present")
	}
	if gone[models.Device](t, db, vm.ID) {
		t.Error("VM with the same netbox_id was deleted")
	}
}

func gone[T any](t *testing.T, db *gorm.DB, id uint) bool {
	t.Helper()
	var row T
	err := db.First(&row, id).Error
	if err == nil {
		return false
	}
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("lookup %T id=%d: %v", row, id, err)
	}
	return true
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Acme AB", "acme-ab"},
		{"Acme-AB", "acme-ab"},
		{"  Foo   Bar  ", "foo-bar"},
		{"", "tenant"},
		{"!!!", "tenant"},
		{strings.Repeat("a", 120), strings.Repeat("a", 100)},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUniqueTenantSlug(t *testing.T) {
	taken := map[string]*netboxtool.NBTenant{
		"acme-ab": {NetboxID: 1},
	}
	if got := uniqueTenantSlug("acme-ab", "7", taken); got != "acme-ab-7" {
		t.Errorf("got %q, want acme-ab-7", got)
	}
	taken["acme-ab-7"] = &netboxtool.NBTenant{NetboxID: 2}
	if got := uniqueTenantSlug("acme-ab", "7", taken); got != "acme-ab-7-2" {
		t.Errorf("got %q, want acme-ab-7-2", got)
	}
	if got := uniqueTenantSlug("other", "7", taken); got != "other" {
		t.Errorf("got %q, want other", got)
	}
}

func TestTenantClaimable(t *testing.T) {
	live := map[string]struct{}{"1": {}, "2": {}}
	cases := []struct {
		name     string
		t        *netboxtool.NBTenant
		sourceID string
		live     map[string]struct{}
		want     bool
	}{
		{"unclaimed", &netboxtool.NBTenant{Name: "Acme"}, "1", live, true},
		{"other source", &netboxtool.NBTenant{CfSource: "lime", CfSourceID: "99"}, "1", live, true},
		{"already ours", &netboxtool.NBTenant{CfSource: "factum", CfSourceID: "1"}, "1", live, true},
		{"other live customer", &netboxtool.NBTenant{CfSource: "factum", CfSourceID: "2"}, "1", live, false},
		{"orphaned claim", &netboxtool.NBTenant{CfSource: "factum", CfSourceID: "99"}, "1", live, true},
		{"no live set, other claim", &netboxtool.NBTenant{CfSource: "factum", CfSourceID: "2"}, "1", nil, false},
	}
	for _, c := range cases {
		if got := tenantClaimable(c.t, c.sourceID, c.live); got != c.want {
			t.Errorf("%s: claimable = %v, want %v", c.name, got, c.want)
		}
	}
}

type fakeTenantAPI struct {
	tenants       []*netboxtool.NBTenant
	createErr     error
	createOnceErr error
	creates       int
	updates       int
}

func (f *fakeTenantAPI) GetTenants() ([]*netboxtool.NBTenant, error) {
	out := make([]*netboxtool.NBTenant, len(f.tenants))
	copy(out, f.tenants)
	return out, nil
}

func (f *fakeTenantAPI) GetTenant(source, sourceID string) (*netboxtool.NBTenant, error) {
	for _, t := range f.tenants {
		if t.CfSource == source && t.CfSourceID == sourceID {
			return t, nil
		}
	}
	return nil, nil
}

func (f *fakeTenantAPI) CreateTenant(name, slug string, changes map[string]any) (*netboxtool.NetboxTenantREST, error) {
	if f.createOnceErr != nil {
		err := f.createOnceErr
		f.createOnceErr = nil
		return nil, err
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	for _, t := range f.tenants {
		if strings.EqualFold(t.Name, name) || t.Slug == slug {
			return nil, fmt.Errorf("netbox POST /api/tenancy/tenants/ failed: 400 Bad Request: {\"__all__\":[\"Constraint “tenancy_tenant_unique_name” is violated.\",\"Constraint “tenancy_tenant_unique_slug” is violated.\"]}")
		}
	}
	cf := tenantFieldsFromChanges(changes)
	t := &netboxtool.NBTenant{
		NetboxID:   uint(len(f.tenants) + 1),
		Name:       name,
		Slug:       slug,
		CfSource:   cf.source,
		CfSourceID: cf.sourceID,
	}
	f.tenants = append(f.tenants, t)
	f.creates++
	return &netboxtool.NetboxTenantREST{ID: t.NetboxID, Name: name, Slug: slug}, nil
}

func (f *fakeTenantAPI) UpdateTenant(tenantID uint, changes map[string]any) error {
	f.updates++
	for _, t := range f.tenants {
		if t.NetboxID != tenantID {
			continue
		}
		if name, ok := changes["name"].(string); ok {
			t.Name = name
		}
		cf := tenantFieldsFromChanges(changes)
		if cf.source != "" {
			t.CfSource = cf.source
		}
		if cf.sourceID != "" {
			t.CfSourceID = cf.sourceID
		}
		return nil
	}
	return fmt.Errorf("tenant %d not found", tenantID)
}

type tenantCF struct {
	source, sourceID string
}

func tenantFieldsFromChanges(changes map[string]any) tenantCF {
	var cf tenantCF
	raw, ok := changes["custom_fields"].(map[string]any)
	if !ok {
		return cf
	}
	if s, ok := raw["source"].(string); ok {
		cf.source = s
	}
	if s, ok := raw["source_id"].(string); ok {
		cf.sourceID = s
	}
	return cf
}

type captureReporter struct {
	msgs []string
}

func (c *captureReporter) Emit(level jobevent.Level, format string, args ...any) {
	c.msgs = append(c.msgs, fmt.Sprintf("%s: %s", level, fmt.Sprintf(format, args...)))
}

func (c *captureReporter) EmitErr(err error) {
	if err != nil {
		c.Emit(jobevent.Error, "%s", err)
	}
}

func seedCustomer(t *testing.T, db *gorm.DB, name string) models.Customer {
	t.Helper()
	c := models.Customer{Name: name, Source: "factum"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("create customer %q: %v", name, err)
	}
	return c
}

func TestSyncCustomersToNetbox_CreatesMissing(t *testing.T) {
	db := newImportTestDB(t)
	c := seedCustomer(t, db, "Acme")
	nb := &fakeTenantAPI{}
	rep := &captureReporter{}
	if err := syncCustomersToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1", nb.creates)
	}
	if len(nb.tenants) != 1 || nb.tenants[0].Name != "Acme" || nb.tenants[0].CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("tenant = %+v", nb.tenants)
	}
}

func TestSyncCustomersToNetbox_AdoptsUnclaimedSameName(t *testing.T) {
	db := newImportTestDB(t)
	c := seedCustomer(t, db, "Acme")
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme", Slug: "acme"},
	}}
	if err := syncCustomersToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 {
		t.Fatalf("creates = %d, want 0 (should adopt, not POST)", nb.creates)
	}
	if nb.updates != 1 {
		t.Fatalf("updates = %d, want 1", nb.updates)
	}
	got := nb.tenants[0]
	if got.CfSource != "factum" || got.CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("adopted tenant CFs = %s/%s, want factum/%d", got.CfSource, got.CfSourceID, c.ID)
	}
}

func TestSyncCustomersToNetbox_SkipsNameClaimedByOtherCustomer(t *testing.T) {
	db := newImportTestDB(t)
	owner := seedCustomer(t, db, "Acme")
	dup := seedCustomer(t, db, "Acme")
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme", Slug: "acme", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", owner.ID)},
	}}
	rep := &captureReporter{}
	if err := syncCustomersToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 {
		t.Fatalf("creates = %d, want 0", nb.creates)
	}
	if nb.updates != 0 {
		t.Fatalf("updates = %d, want 0 (owner already matched, dup skipped)", nb.updates)
	}
	want := fmt.Sprintf("id=%d", dup.ID)
	var skipped bool
	for _, m := range rep.msgs {
		if strings.Contains(m, "skipping customer") && strings.Contains(m, want) {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("expected skip warning for %s, got %v", want, rep.msgs)
	}
}

func TestSyncCustomersToNetbox_AdoptsOrphanedClaim(t *testing.T) {
	db := newImportTestDB(t)
	c := seedCustomer(t, db, "Acme")
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme", Slug: "acme", CfSource: "factum", CfSourceID: "999"},
	}}
	if err := syncCustomersToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 1 {
		t.Fatalf("creates=%d updates=%d, want adopt of orphaned source_id", nb.creates, nb.updates)
	}
	if nb.tenants[0].CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("source_id = %s, want %d", nb.tenants[0].CfSourceID, c.ID)
	}
}

func TestSyncCustomersToNetbox_UpdatesRenamedCustomer(t *testing.T) {
	db := newImportTestDB(t)
	c := seedCustomer(t, db, "Acme New")
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme Old", Slug: "acme-old", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", c.ID)},
	}}
	if err := syncCustomersToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 1 {
		t.Fatalf("creates=%d updates=%d, want 0/1", nb.creates, nb.updates)
	}
	if nb.tenants[0].Name != "Acme New" {
		t.Fatalf("name = %q, want Acme New", nb.tenants[0].Name)
	}
}

func TestSyncCustomersToNetbox_UniquifiesSlugCollision(t *testing.T) {
	db := newImportTestDB(t)
	c := seedCustomer(t, db, "Acme-AB")
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 1, Name: "Acme AB", Slug: "acme-ab", CfSource: "factum", CfSourceID: "999"},
	}}
	if err := syncCustomersToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1", nb.creates)
	}
	wantSlug := "acme-ab-" + fmt.Sprintf("%d", c.ID)
	if nb.tenants[1].Slug != wantSlug {
		t.Fatalf("slug = %q, want %q", nb.tenants[1].Slug, wantSlug)
	}
}

func TestSyncCustomersToNetbox_ConflictDoesNotAbort(t *testing.T) {
	db := newImportTestDB(t)
	_ = seedCustomer(t, db, "Alpha")
	second := seedCustomer(t, db, "Beta")
	nb := &fakeTenantAPI{
		createOnceErr: fmt.Errorf("netbox POST /api/tenancy/tenants/ failed: 400 Bad Request: Constraint “tenancy_tenant_unique_name” is violated."),
	}
	rep := &captureReporter{}
	if err := syncCustomersToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1 (Beta should still be created after Alpha's 400)", nb.creates)
	}
	if len(nb.tenants) != 1 || nb.tenants[0].Name != "Beta" || nb.tenants[0].CfSourceID != fmt.Sprintf("%d", second.ID) {
		t.Fatalf("tenants = %+v", nb.tenants)
	}
	var warned bool
	for _, m := range rep.msgs {
		if strings.Contains(m, "skipping customer") && strings.Contains(m, "Alpha") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected skip warning for Alpha, got %v", rep.msgs)
	}
}

func TestFindOrCreateTenant_ReturnsExisting(t *testing.T) {
	existing := &netboxtool.NBTenant{NetboxID: 9, Name: "Acme", Slug: "acme", CfSource: "factum", CfSourceID: "5"}
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{existing}}
	got, err := findOrCreateTenant(nb, models.Customer{FactumModel: models.FactumModel{ID: 5}, Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.NetboxID != 9 || nb.creates != 0 || nb.updates != 0 {
		t.Fatalf("got %+v creates=%d updates=%d", got, nb.creates, nb.updates)
	}
}

func TestFindOrCreateTenant_AdoptsByName(t *testing.T) {
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme", Slug: "acme"},
	}}
	got, err := findOrCreateTenant(nb, models.Customer{FactumModel: models.FactumModel{ID: 5}, Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 1 {
		t.Fatalf("creates=%d updates=%d, want adopt", nb.creates, nb.updates)
	}
	if got.NetboxID != 9 || got.CfSourceID != "5" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindOrCreateTenant_CreatesWhenMissing(t *testing.T) {
	nb := &fakeTenantAPI{}
	got, err := findOrCreateTenant(nb, models.Customer{FactumModel: models.FactumModel{ID: 5}, Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 || got.Name != "Acme" || got.CfSourceID != "5" {
		t.Fatalf("got %+v creates=%d", got, nb.creates)
	}
}

func TestFindOrCreateTenant_NameTaken(t *testing.T) {
	nb := &fakeTenantAPI{tenants: []*netboxtool.NBTenant{
		{NetboxID: 9, Name: "Acme", Slug: "acme", CfSource: "factum", CfSourceID: "1"},
	}}
	_, err := findOrCreateTenant(nb, models.Customer{FactumModel: models.FactumModel{ID: 5}, Name: "Acme"})
	if err == nil {
		t.Fatal("expected name-taken error")
	}
	if !isTenantConflict(err) {
		t.Fatalf("err = %v, want tenant conflict", err)
	}
}
