package lime

import (
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	limetoolmodels "github.com/abundo/limetool/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := util.MigrateDatabase(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

func TestServiceFromDeliveryMapsLimeFields(t *testing.T) {
	delivery := limetoolmodels.LimeDelivery{
		ID:               77,
		ConnectionNumber: "20-73",
		Comment:          "Region Norrbotten PÄS-Sbyn",
		DeliveryPoint:    &limetoolmodels.LimeDeliverypoint{Address: "Site A"},
		DeliveryPoint2:   &limetoolmodels.LimeDeliverypoint2{Address: "Site B"},
		Product:          &limetoolmodels.LimeProduct{Name: "Svartfiber"},
		Service:          &limetoolmodels.LimeService{Name: "Hyra av fiber"},
		Agreeement: &limetoolmodels.LimeAgreement{
			Status: &limetoolmodels.LimeAgreementStatus{Key: "active", Text: "Active"},
		},
	}

	svc := serviceFromDelivery(delivery, 9, 3)
	if svc.Source != "lime" || svc.SourceID != "77" {
		t.Errorf("source = %q/%q, want lime/77", svc.Source, svc.SourceID)
	}
	if svc.CustomerID != 9 || svc.LastSync != 3 {
		t.Errorf("CustomerID/LastSync = %d/%d, want 9/3", svc.CustomerID, svc.LastSync)
	}
	if svc.ServiceID != "20-73" || svc.Comment != delivery.Comment {
		t.Errorf("ServiceID/Comment = %q/%q", svc.ServiceID, svc.Comment)
	}
	if svc.DeliveryPoint1 != "Site A" || svc.DeliveryPoint2 != "Site B" {
		t.Errorf("delivery points = %q/%q", svc.DeliveryPoint1, svc.DeliveryPoint2)
	}
	if svc.Product != "Svartfiber" || svc.Service != "Hyra av fiber" {
		t.Errorf("Product/Service = %q/%q", svc.Product, svc.Service)
	}
	if svc.AgreementStatus != "Active" {
		t.Errorf("AgreementStatus = %q, want Active", svc.AgreementStatus)
	}
}

func TestServiceFromDeliveryEmptyAgreementStatus(t *testing.T) {
	svc := serviceFromDelivery(limetoolmodels.LimeDelivery{ID: 1, ConnectionNumber: "x"}, 1, 1)
	if svc.AgreementStatus != "" {
		t.Errorf("AgreementStatus = %q, want empty when no agreement is embedded", svc.AgreementStatus)
	}
}

func TestSaveDeliveryWritesAgreementStatus(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := serviceFromDelivery(limetoolmodels.LimeDelivery{
		ID:               5,
		ConnectionNumber: "CN1",
		Agreeement: &limetoolmodels.LimeAgreement{
			Status: &limetoolmodels.LimeAgreementStatus{Text: "Active"},
		},
	}, 1, 1)
	if err := l.SaveDelivery(&row); err != nil {
		t.Fatalf("SaveDelivery: %v", err)
	}

	var stored models.Service
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.AgreementStatus != "Active" {
		t.Errorf("stored AgreementStatus = %q, want Active", stored.AgreementStatus)
	}

	again := serviceFromDelivery(limetoolmodels.LimeDelivery{
		ID:               5,
		ConnectionNumber: "CN1",
		Agreeement: &limetoolmodels.LimeAgreement{
			Status: &limetoolmodels.LimeAgreementStatus{Text: "Ended"},
		},
	}, 1, 2)
	if err := l.SaveDelivery(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if again.ID != row.ID {
		t.Errorf("ID = %d, want existing %d", again.ID, row.ID)
	}
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("reload after resync: %v", err)
	}
	if stored.AgreementStatus != "Ended" {
		t.Errorf("resync AgreementStatus = %q, want Ended", stored.AgreementStatus)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("resync zeroed CreatedAt")
	}
}

func TestSaveDeliveryPreservesFactumOwnedFields(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := serviceFromDelivery(limetoolmodels.LimeDelivery{ID: 9, ConnectionNumber: "CN1"}, 1, 1)
	if err := l.SaveDelivery(&row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&row).Updates(map[string]any{
		"service_type":                 "ELINE",
		"bandwidth_mbps":               100,
		"applied_endpoint_a_device_id": 7,
		"applied_endpoint_a_iface":     "Ethernet1",
	}).Error; err != nil {
		t.Fatalf("enrich: %v", err)
	}

	again := serviceFromDelivery(limetoolmodels.LimeDelivery{ID: 9, ConnectionNumber: "CN1", Comment: "moved"}, 1, 2)
	if err := l.SaveDelivery(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}

	var stored models.Service
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.ServiceType != "ELINE" || stored.BandwidthMbps != 100 {
		t.Errorf("factum type fields wiped: type=%q bw=%d", stored.ServiceType, stored.BandwidthMbps)
	}
	if stored.AppliedEndpointADeviceID != 7 || stored.AppliedEndpointAIface != "Ethernet1" {
		t.Errorf("applied endpoints wiped: %+v", stored)
	}
	if stored.Comment != "moved" || stored.LastSync != 2 {
		t.Errorf("lime fields not updated: comment=%q last_sync=%d", stored.Comment, stored.LastSync)
	}
}

func TestSaveCustomerPreservesCreatedAt(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := models.Customer{Source: "lime", SourceID: "3", Name: "Acme", LastSync: 1}
	if err := l.SaveCustomer(&row); err != nil {
		t.Fatalf("create: %v", err)
	}
	created := row.CreatedAt
	if created.IsZero() {
		t.Fatal("expected CreatedAt on insert")
	}

	again := models.Customer{Source: "lime", SourceID: "3", Name: "Acme AB", LastSync: 2}
	if err := l.SaveCustomer(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if again.ID != row.ID {
		t.Errorf("ID = %d, want %d", again.ID, row.ID)
	}
	var stored models.Customer
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Name != "Acme AB" || stored.LastSync != 2 {
		t.Errorf("stored = %+v", stored)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("resync zeroed CreatedAt")
	}
}

func TestContactFromPersonMapsLimeFields(t *testing.T) {
	p := limetoolmodels.LimePerson{
		ID:          1011,
		Name:        "Anders Edström",
		Email:       "anders.edstrom@norrbotten.se",
		Phone:       "0920-28 43 34",
		Mobilephone: "070",
	}
	c := contactFromPerson(p, 7)
	if c.Source != "lime" || c.SourceID != "1011" {
		t.Errorf("source = %q/%q, want lime/1011", c.Source, c.SourceID)
	}
	if c.Name != "Anders Edström" || c.Email != p.Email || c.Phone != "0920-28 43 34" {
		t.Errorf("mapped fields = %+v", c)
	}
	if c.LastSync != 7 {
		t.Errorf("LastSync = %d, want 7", c.LastSync)
	}
}

func TestSaveContactCreatesWithNotifyDefault(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := contactFromPerson(limetoolmodels.LimePerson{ID: 1, Name: "Ada", Email: "ada@example.com"}, 1)
	if err := l.SaveContact(&row); err != nil {
		t.Fatalf("SaveContact: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("expected assigned ID")
	}
	if row.NotifyMaintenance {
		t.Error("new Lime contact should default NotifyMaintenance false")
	}

	var stored models.Contact
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Source != "lime" || stored.SourceID != "1" || stored.Name != "Ada" {
		t.Errorf("stored = %+v", stored)
	}
}

func TestSaveContactPreservesNotifyMaintenance(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := contactFromPerson(limetoolmodels.LimePerson{ID: 2, Name: "Ada", Email: "old@example.com", Phone: "1"}, 1)
	if err := l.SaveContact(&row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&row).Update("notify_maintenance", true).Error; err != nil {
		t.Fatalf("operator opt-in: %v", err)
	}

	again := contactFromPerson(limetoolmodels.LimePerson{ID: 2, Name: "Ada Lovelace", Email: "new@example.com", Phone: "2"}, 2)
	if err := l.SaveContact(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if again.ID != row.ID {
		t.Errorf("ID = %d, want existing %d", again.ID, row.ID)
	}
	if !again.NotifyMaintenance {
		t.Error("resync reset NotifyMaintenance")
	}
	if again.Name != "Ada Lovelace" || again.Email != "new@example.com" || again.Phone != "2" {
		t.Errorf("lime-owned fields not updated: %+v", again)
	}
}

func TestSyncPersonLinksCustomerAndSkipsInactiveCreate(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	cust := models.Customer{Name: "Acme", Source: "lime", SourceID: "9"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	active := limetoolmodels.LimePerson{ID: 10, Name: "Active", Email: "a@example.com", Company: 9}
	if err := l.syncPerson(&cust, active, 1); err != nil {
		t.Fatalf("sync active: %v", err)
	}

	var contacts []models.Contact
	if err := db.Find(&contacts).Error; err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("got %d contacts, want 1", len(contacts))
	}
	var links int64
	if err := db.Model(&models.CustomerContact{}).Where("contact_id = ? AND customer_id = ?", contacts[0].ID, cust.ID).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 1 {
		t.Errorf("links = %d, want 1", links)
	}

	inactiveNew := limetoolmodels.LimePerson{ID: 11, Name: "Never", Inactive: true}
	if err := l.syncPerson(&cust, inactiveNew, 1); err != nil {
		t.Fatalf("sync unseen inactive: %v", err)
	}
	if err := db.Find(&contacts).Error; err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Errorf("inactive person was created: %d contacts", len(contacts))
	}
}

func TestSyncPersonInactiveUnlinksExisting(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	cust := models.Customer{Name: "Acme", Source: "lime", SourceID: "9"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	person := limetoolmodels.LimePerson{ID: 10, Name: "Was Active", Email: "a@example.com"}
	if err := l.syncPerson(&cust, person, 1); err != nil {
		t.Fatalf("sync active: %v", err)
	}

	person.Inactive = true
	if err := l.syncPerson(&cust, person, 2); err != nil {
		t.Fatalf("sync inactive: %v", err)
	}

	var contact models.Contact
	if err := db.Where("source = ? AND source_id = ?", "lime", "10").First(&contact).Error; err != nil {
		t.Fatalf("contact should remain: %v", err)
	}
	var links int64
	if err := db.Model(&models.CustomerContact{}).Where("contact_id = ?", contact.ID).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Errorf("inactive person still linked: %d", links)
	}
	if contact.Name != "Was Active" {
		t.Errorf("inactive resync should not rewrite fields, name=%q", contact.Name)
	}
}

func TestReplaceContactCustomersIsExclusive(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	a := models.Customer{Name: "A"}
	b := models.Customer{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatalf("seed B: %v", err)
	}
	contact := models.Contact{Name: "P", Source: "lime", SourceID: "1"}
	if err := db.Create(&contact).Error; err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	if err := l.replaceContactCustomers(contact.ID, a.ID); err != nil {
		t.Fatalf("link A: %v", err)
	}
	if err := l.replaceContactCustomers(contact.ID, b.ID); err != nil {
		t.Fatalf("link B: %v", err)
	}

	var links []models.CustomerContact
	if err := db.Where("contact_id = ?", contact.ID).Find(&links).Error; err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 || links[0].CustomerID != b.ID {
		t.Errorf("links = %+v, want only customer B", links)
	}
}

func TestPruneStaleDeliveriesRemovesUnseenLimeServices(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	cust := models.Customer{Name: "Acme", Source: "lime", SourceID: "1"}
	other := models.Customer{Name: "Other", Source: "lime", SourceID: "2"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed cust: %v", err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}

	keep := models.Service{Source: "lime", SourceID: "10", CustomerID: cust.ID, ServiceID: "CN1", LastSync: 5}
	gone := models.Service{Source: "lime", SourceID: "11", CustomerID: cust.ID, ServiceID: "CN2", LastSync: 4}
	factum := models.Service{Source: "factum", CustomerID: cust.ID, ServiceID: "CI00001", LastSync: 1}
	otherSvc := models.Service{Source: "lime", SourceID: "20", CustomerID: other.ID, ServiceID: "CN3", LastSync: 4}
	for _, s := range []*models.Service{&keep, &gone, &factum, &otherSvc} {
		if err := db.Create(s).Error; err != nil {
			t.Fatalf("seed service %s: %v", s.ServiceID, err)
		}
	}

	n, err := l.pruneStaleDeliveries(cust.ID, 5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}

	var ids []string
	var remaining []models.Service
	if err := db.Order("id").Find(&remaining).Error; err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range remaining {
		ids = append(ids, s.SourceID+":"+s.ServiceID)
	}
	want := []string{"10:CN1", ":CI00001", "20:CN3"}
	if len(remaining) != 3 {
		t.Fatalf("remaining = %v, want 3 (%v)", ids, want)
	}
	for i, s := range remaining {
		got := s.SourceID + ":" + s.ServiceID
		if got != want[i] {
			t.Errorf("remaining[%d] = %s, want %s", i, got, want[i])
		}
	}
}

func TestPruneRemovedPersonLinksUnlinksStaleLimeContacts(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	cust := models.Customer{Name: "Acme", Source: "lime", SourceID: "1"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatalf("seed cust: %v", err)
	}
	seen := models.Contact{Name: "Seen", Source: "lime", SourceID: "10", LastSync: 5}
	gone := models.Contact{Name: "Gone", Source: "lime", SourceID: "11", LastSync: 4}
	local := models.Contact{Name: "Local", Source: "factum", LastSync: 1}
	for _, c := range []*models.Contact{&seen, &gone, &local} {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("seed contact: %v", err)
		}
		if err := db.Create(&models.CustomerContact{ContactID: c.ID, CustomerID: cust.ID}).Error; err != nil {
			t.Fatalf("link: %v", err)
		}
	}

	n, err := l.pruneRemovedPersonLinks(cust.ID, 5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("unlinked %d, want 1", n)
	}

	var contacts []models.Contact
	if err := db.Find(&contacts).Error; err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if len(contacts) != 3 {
		t.Errorf("contact rows = %d, want 3 (rows are kept)", len(contacts))
	}

	var links []models.CustomerContact
	if err := db.Find(&links).Error; err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2", len(links))
	}
	got := map[uint]bool{}
	for _, link := range links {
		got[link.ContactID] = true
	}
	if !got[seen.ID] || !got[local.ID] || got[gone.ID] {
		t.Errorf("links = %+v, want seen+local", links)
	}
}

func TestPruneStaleCustomersRemovesUnseenLimeCompanies(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	keep := models.Customer{Name: "Keep", Source: "lime", SourceID: "1", LastSync: 5}
	gone := models.Customer{Name: "Gone", Source: "lime", SourceID: "2", LastSync: 4}
	held := models.Customer{Name: "Held", Source: "lime", SourceID: "3", LastSync: 4}
	local := models.Customer{Name: "Local", Source: "factum", LastSync: 1}
	for _, c := range []*models.Customer{&keep, &gone, &held, &local} {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("seed customer %s: %v", c.Name, err)
		}
	}
	if err := db.Create(&models.Service{Source: "lime", SourceID: "20", CustomerID: gone.ID, ServiceID: "CN-gone", LastSync: 4}).Error; err != nil {
		t.Fatalf("seed gone service: %v", err)
	}
	if err := db.Create(&models.Service{Source: "factum", CustomerID: held.ID, ServiceID: "CI00001"}).Error; err != nil {
		t.Fatalf("seed held service: %v", err)
	}

	custN, svcN, err := l.pruneStaleCustomers(5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if custN != 1 || svcN != 1 {
		t.Errorf("removed customers=%d services=%d, want 1 and 1", custN, svcN)
	}

	var names []string
	var remaining []models.Customer
	if err := db.Order("id").Find(&remaining).Error; err != nil {
		t.Fatalf("list customers: %v", err)
	}
	for _, c := range remaining {
		names = append(names, c.Name)
	}
	if len(remaining) != 3 {
		t.Fatalf("customers = %v, want Keep, Held, Local", names)
	}

	var svcs []models.Service
	if err := db.Find(&svcs).Error; err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(svcs) != 1 || svcs[0].ServiceID != "CI00001" {
		t.Errorf("services = %+v, want only the factum row on Held", svcs)
	}
}

func TestIsFullLimeSync(t *testing.T) {
	if !isFullLimeSync(nil) || !isFullLimeSync([]string{""}) || !isFullLimeSync([]string{"Acme", ""}) {
		t.Error("empty company name should be a full sync")
	}
	if isFullLimeSync([]string{"Acme"}) || isFullLimeSync([]string{"A", "B"}) {
		t.Error("named companies should not prune other customers")
	}
}

func TestLimeSourceIDUnique(t *testing.T) {
	db := newTestDB(t)
	first := models.Service{Source: "lime", SourceID: "77", ServiceID: "CN1"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	dup := models.Service{Source: "lime", SourceID: "77", ServiceID: "CN1-copy"}
	if err := db.Create(&dup).Error; err == nil {
		t.Fatal("expected unique index to reject a second lime row with the same source_id")
	}
	ok := models.Service{Source: "lime", SourceID: "78", ServiceID: "CN1"}
	if err := db.Create(&ok).Error; err != nil {
		t.Fatalf("same connection number, different lime id should be allowed: %v", err)
	}
}
