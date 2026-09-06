package netbox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

type fakeContactAPI struct {
	contacts      []*NBContact
	tenants       []*netboxtool.NBTenant
	role          *NBContactRole
	assignments   []*NBContactAssignment
	createErr     error
	createOnceErr error
	assignErr     error
	creates       int
	updates       int
	assignCreates int
	roleEnsures   int
}

func (f *fakeContactAPI) GetContacts() ([]*NBContact, error) {
	out := make([]*NBContact, len(f.contacts))
	copy(out, f.contacts)
	return out, nil
}

func (f *fakeContactAPI) CreateContact(name string, extra map[string]any) (*NBContact, error) {
	if f.createOnceErr != nil {
		err := f.createOnceErr
		f.createOnceErr = nil
		return nil, err
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	cf := tenantFieldsFromChanges(extra)
	c := &NBContact{
		NetboxID:   uint(len(f.contacts) + 1),
		Name:       name,
		CfSource:   cf.source,
		CfSourceID: cf.sourceID,
	}
	if email, ok := extra["email"].(string); ok {
		c.Email = email
	}
	if phone, ok := extra["phone"].(string); ok {
		c.Phone = phone
	}
	f.contacts = append(f.contacts, c)
	f.creates++
	return c, nil
}

func (f *fakeContactAPI) UpdateContact(contactID uint, changes map[string]any) error {
	f.updates++
	for _, c := range f.contacts {
		if c.NetboxID != contactID {
			continue
		}
		if name, ok := changes["name"].(string); ok {
			c.Name = name
		}
		if email, ok := changes["email"].(string); ok {
			c.Email = email
		}
		if phone, ok := changes["phone"].(string); ok {
			c.Phone = phone
		}
		cf := tenantFieldsFromChanges(changes)
		if cf.source != "" {
			c.CfSource = cf.source
		}
		if cf.sourceID != "" {
			c.CfSourceID = cf.sourceID
		}
		return nil
	}
	return fmt.Errorf("contact %d not found", contactID)
}

func (f *fakeContactAPI) EnsureContactRole(name, slug string) (*NBContactRole, error) {
	f.roleEnsures++
	if f.role == nil {
		f.role = &NBContactRole{NetboxID: 1, Name: name, Slug: slug}
	}
	return f.role, nil
}

func (f *fakeContactAPI) GetContactAssignments(roleID uint) ([]*NBContactAssignment, error) {
	var out []*NBContactAssignment
	for _, a := range f.assignments {
		if roleID == 0 || a.RoleID == roleID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeContactAPI) CreateContactAssignment(objectType string, objectID, contactID, roleID uint) error {
	if f.assignErr != nil {
		return f.assignErr
	}
	f.assignments = append(f.assignments, &NBContactAssignment{
		NetboxID:   uint(len(f.assignments) + 1),
		ObjectType: objectType,
		ObjectID:   objectID,
		ContactID:  contactID,
		RoleID:     roleID,
	})
	f.assignCreates++
	return nil
}

func (f *fakeContactAPI) GetTenants() ([]*netboxtool.NBTenant, error) {
	out := make([]*netboxtool.NBTenant, len(f.tenants))
	copy(out, f.tenants)
	return out, nil
}

func seedContact(t *testing.T, db *gorm.DB, name, email, phone string) models.Contact {
	t.Helper()
	c := models.Contact{Name: name, Email: email, Phone: phone, Source: "factum"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("create contact %q: %v", name, err)
	}
	return c
}

func TestSyncContactsToNetbox_CreatesMissing(t *testing.T) {
	db := newImportTestDB(t)
	c := seedContact(t, db, "Ada Lovelace", "ada@example.com", "1")
	nb := &fakeContactAPI{}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1", nb.creates)
	}
	got := nb.contacts[0]
	if got.Name != "Ada Lovelace" || got.Email != "ada@example.com" || got.Phone != "1" {
		t.Fatalf("contact = %+v", got)
	}
	if got.CfSource != "factum" || got.CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("CFs = %s/%s, want factum/%d", got.CfSource, got.CfSourceID, c.ID)
	}
}

func TestSyncContactsToNetbox_AdoptsUnclaimedSameEmail(t *testing.T) {
	db := newImportTestDB(t)
	c := seedContact(t, db, "Ada Lovelace", "ada@example.com", "2")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada", Email: "ada@example.com", Phone: "1"},
	}}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 {
		t.Fatalf("creates = %d, want 0 (should adopt, not POST)", nb.creates)
	}
	if nb.updates != 1 {
		t.Fatalf("updates = %d, want 1", nb.updates)
	}
	got := nb.contacts[0]
	if got.CfSource != "factum" || got.CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("adopted CFs = %s/%s, want factum/%d", got.CfSource, got.CfSourceID, c.ID)
	}
	if got.Name != "Ada Lovelace" || got.Phone != "2" {
		t.Fatalf("adopted fields = %+v", got)
	}
}

func TestSyncContactsToNetbox_DoesNotAdoptByName(t *testing.T) {
	db := newImportTestDB(t)
	_ = seedContact(t, db, "Ada Lovelace", "", "")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada Lovelace"},
	}}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1 (name is not a match key)", nb.creates)
	}
	if nb.updates != 0 {
		t.Fatalf("updates = %d, want 0", nb.updates)
	}
}

func TestSyncContactsToNetbox_SkipsEmailClaimedByOtherContact(t *testing.T) {
	db := newImportTestDB(t)
	owner := seedContact(t, db, "Ada", "ada@example.com", "")
	dup := seedContact(t, db, "Ada Other", "ada@example.com", "")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada", Email: "ada@example.com", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", owner.ID)},
	}}
	rep := &captureReporter{}
	if err := syncContactsToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1 (dup should POST its own contact)", nb.creates)
	}
	if nb.contacts[1].CfSourceID != fmt.Sprintf("%d", dup.ID) {
		t.Fatalf("new contact source_id = %s, want %d", nb.contacts[1].CfSourceID, dup.ID)
	}
	if nb.contacts[0].CfSourceID != fmt.Sprintf("%d", owner.ID) {
		t.Fatalf("owner source_id stolen: %s", nb.contacts[0].CfSourceID)
	}
}

func TestSyncContactsToNetbox_AdoptsOrphanedClaim(t *testing.T) {
	db := newImportTestDB(t)
	c := seedContact(t, db, "Ada", "ada@example.com", "")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada", Email: "ada@example.com", CfSource: "factum", CfSourceID: "999"},
	}}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 1 {
		t.Fatalf("creates=%d updates=%d, want adopt of orphaned source_id", nb.creates, nb.updates)
	}
	if nb.contacts[0].CfSourceID != fmt.Sprintf("%d", c.ID) {
		t.Fatalf("source_id = %s, want %d", nb.contacts[0].CfSourceID, c.ID)
	}
}

func TestSyncContactsToNetbox_UpdatesChangedFields(t *testing.T) {
	db := newImportTestDB(t)
	c := seedContact(t, db, "Ada Lovelace", "new@example.com", "2")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada", Email: "old@example.com", Phone: "1", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", c.ID)},
	}}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 1 {
		t.Fatalf("creates=%d updates=%d, want 0/1", nb.creates, nb.updates)
	}
	got := nb.contacts[0]
	if got.Name != "Ada Lovelace" || got.Email != "new@example.com" || got.Phone != "2" {
		t.Fatalf("updated = %+v", got)
	}
}

func TestSyncContactsToNetbox_UnchangedSkipsUpdate(t *testing.T) {
	db := newImportTestDB(t)
	c := seedContact(t, db, "Ada", "ada@example.com", "1")
	nb := &fakeContactAPI{contacts: []*NBContact{
		{NetboxID: 9, Name: "Ada", Email: "ada@example.com", Phone: "1", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", c.ID)},
	}}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 || nb.updates != 0 {
		t.Fatalf("creates=%d updates=%d, want 0/0", nb.creates, nb.updates)
	}
}

func TestSyncContactsToNetbox_SkipsEmptyName(t *testing.T) {
	db := newImportTestDB(t)
	c := models.Contact{Name: " ", Email: "x@example.com", Source: "factum"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	nb := &fakeContactAPI{}
	rep := &captureReporter{}
	if err := syncContactsToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 0 {
		t.Fatalf("creates = %d, want 0", nb.creates)
	}
	var warned bool
	for _, m := range rep.msgs {
		if strings.Contains(m, "empty name") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected empty-name warning, got %v", rep.msgs)
	}
}

func TestSyncContactsToNetbox_ConflictDoesNotAbort(t *testing.T) {
	db := newImportTestDB(t)
	_ = seedContact(t, db, "Bad", "not-an-email", "")
	second := seedContact(t, db, "Good", "good@example.com", "")
	nb := &fakeContactAPI{
		createOnceErr: fmt.Errorf("netbox POST /api/tenancy/contacts/ failed: 400 Bad Request: Enter a valid email address."),
	}
	rep := &captureReporter{}
	if err := syncContactsToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1 (Good should still be created after Bad's 400)", nb.creates)
	}
	if len(nb.contacts) != 1 || nb.contacts[0].Name != "Good" || nb.contacts[0].CfSourceID != fmt.Sprintf("%d", second.ID) {
		t.Fatalf("contacts = %+v", nb.contacts)
	}
	var warned bool
	for _, m := range rep.msgs {
		if strings.Contains(m, "skipping contact") && strings.Contains(m, "Bad") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected skip warning for Bad, got %v", rep.msgs)
	}
}

func TestSyncContactsToNetbox_AssignsToTenant(t *testing.T) {
	db := newImportTestDB(t)
	cust := seedCustomer(t, db, "Acme")
	ct := seedContact(t, db, "Ada", "ada@example.com", "")
	if err := db.Create(&models.CustomerContact{CustomerID: cust.ID, ContactID: ct.ID}).Error; err != nil {
		t.Fatal(err)
	}
	nb := &fakeContactAPI{
		tenants: []*netboxtool.NBTenant{
			{NetboxID: 50, Name: "Acme", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", cust.ID)},
		},
	}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.creates != 1 {
		t.Fatalf("creates = %d, want 1", nb.creates)
	}
	if nb.roleEnsures != 1 {
		t.Fatalf("roleEnsures = %d, want 1", nb.roleEnsures)
	}
	if nb.assignCreates != 1 {
		t.Fatalf("assignCreates = %d, want 1", nb.assignCreates)
	}
	got := nb.assignments[0]
	if got.ObjectType != tenantObjectType || got.ObjectID != 50 || got.ContactID != nb.contacts[0].NetboxID {
		t.Fatalf("assignment = %+v", got)
	}
}

func TestSyncContactsToNetbox_DoesNotDuplicateAssignment(t *testing.T) {
	db := newImportTestDB(t)
	cust := seedCustomer(t, db, "Acme")
	ct := seedContact(t, db, "Ada", "ada@example.com", "")
	if err := db.Create(&models.CustomerContact{CustomerID: cust.ID, ContactID: ct.ID}).Error; err != nil {
		t.Fatal(err)
	}
	nb := &fakeContactAPI{
		contacts: []*NBContact{
			{NetboxID: 9, Name: "Ada", Email: "ada@example.com", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", ct.ID)},
		},
		tenants: []*netboxtool.NBTenant{
			{NetboxID: 50, Name: "Acme", CfSource: "factum", CfSourceID: fmt.Sprintf("%d", cust.ID)},
		},
		role: &NBContactRole{NetboxID: 3, Name: factumContactRoleName, Slug: factumContactRoleSlug},
		assignments: []*NBContactAssignment{
			{NetboxID: 1, ObjectType: tenantObjectType, ObjectID: 50, ContactID: 9, RoleID: 3},
		},
	}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.assignCreates != 0 {
		t.Fatalf("assignCreates = %d, want 0", nb.assignCreates)
	}
}

func TestSyncContactsToNetbox_SkipsAssignmentWithoutTenant(t *testing.T) {
	db := newImportTestDB(t)
	cust := seedCustomer(t, db, "Acme")
	ct := seedContact(t, db, "Ada", "ada@example.com", "")
	if err := db.Create(&models.CustomerContact{CustomerID: cust.ID, ContactID: ct.ID}).Error; err != nil {
		t.Fatal(err)
	}
	nb := &fakeContactAPI{}
	rep := &captureReporter{}
	if err := syncContactsToNetbox(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if nb.assignCreates != 0 {
		t.Fatalf("assignCreates = %d, want 0", nb.assignCreates)
	}
	var logged bool
	for _, m := range rep.msgs {
		if strings.Contains(m, "assignment sync") && strings.Contains(m, "1 skipped") {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("expected skipped assignment summary, got %v", rep.msgs)
	}
}

func TestSyncContactsToNetbox_NoLinksSkipsRoleLookup(t *testing.T) {
	db := newImportTestDB(t)
	_ = seedContact(t, db, "Ada", "ada@example.com", "")
	nb := &fakeContactAPI{}
	if err := syncContactsToNetbox(db, nb, &captureReporter{}); err != nil {
		t.Fatal(err)
	}
	if nb.roleEnsures != 0 {
		t.Fatalf("roleEnsures = %d, want 0 when there are no customer links", nb.roleEnsures)
	}
}

func TestCustomFieldSpecs_SourceIncludesContact(t *testing.T) {
	for _, spec := range customFieldSpecs(&models.Settings{}) {
		if spec.name != "source" && spec.name != "source_id" {
			continue
		}
		if !contains(spec.objectTypes, "tenancy.tenant") || !contains(spec.objectTypes, "tenancy.contact") {
			t.Errorf("%s objectTypes = %v, want tenant and contact", spec.name, spec.objectTypes)
		}
	}
}

func TestCustomFieldString(t *testing.T) {
	fields := map[string]any{"source": "factum", "source_id": float64(12)}
	if got := customFieldString(fields, "source"); got != "factum" {
		t.Errorf("source = %q", got)
	}
	if got := customFieldString(fields, "source_id"); got != "12" {
		t.Errorf("source_id = %q, want 12", got)
	}
}
