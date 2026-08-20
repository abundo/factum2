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
	if !row.NotifyMaintenance {
		t.Error("new Lime contact should default NotifyMaintenance true")
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
	if err := db.Model(&row).Update("notify_maintenance", false).Error; err != nil {
		t.Fatalf("operator opt-out: %v", err)
	}

	again := contactFromPerson(limetoolmodels.LimePerson{ID: 2, Name: "Ada Lovelace", Email: "new@example.com", Phone: "2"}, 2)
	if err := l.SaveContact(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if again.ID != row.ID {
		t.Errorf("ID = %d, want existing %d", again.ID, row.ID)
	}
	if again.NotifyMaintenance {
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
