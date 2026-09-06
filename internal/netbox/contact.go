package netbox

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

// factumContactRole is the Netbox contact role used for CustomerContact →
// tenancy.tenant assignments. Assignments with any other role are left
// alone; this sync never deletes contacts or assignments.
const (
	factumContactRoleName = "Factum"
	factumContactRoleSlug = "factum"
	tenantObjectType      = "tenancy.tenant"
)

// NBContact is a tenancy.Contact row as this package needs it.
type NBContact struct {
	NetboxID   uint
	Name       string
	Email      string
	Phone      string
	CfSource   string
	CfSourceID string
}

// NBContactRole is a tenancy.ContactRole row.
type NBContactRole struct {
	NetboxID uint   `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
}

// NBContactAssignment is a tenancy.ContactAssignment row, flattened to ids.
type NBContactAssignment struct {
	NetboxID   uint
	ObjectType string
	ObjectID   uint
	ContactID  uint
	RoleID     uint
}

// contactAPI is the Netbox surface contact→Netbox sync needs, narrowed so
// tests can substitute a fake without a live Netbox. contactClient wraps
// *netboxtool.NetboxClient and implements it.
type contactAPI interface {
	GetContacts() ([]*NBContact, error)
	CreateContact(name string, extra map[string]any) (*NBContact, error)
	UpdateContact(contactID uint, changes map[string]any) error
	EnsureContactRole(name, slug string) (*NBContactRole, error)
	GetContactAssignments(roleID uint) ([]*NBContactAssignment, error)
	CreateContactAssignment(objectType string, objectID, contactID, roleID uint) error
	GetTenants() ([]*netboxtool.NBTenant, error)
}

type contactClient struct {
	*netboxtool.NetboxClient
}

type restContact struct {
	ID           uint           `json:"id"`
	Name         string         `json:"name"`
	Email        string         `json:"email"`
	Phone        string         `json:"phone"`
	CustomFields map[string]any `json:"custom_fields"`
}

func (r restContact) toNBContact() *NBContact {
	return &NBContact{
		NetboxID:   r.ID,
		Name:       r.Name,
		Email:      r.Email,
		Phone:      r.Phone,
		CfSource:   customFieldString(r.CustomFields, "source"),
		CfSourceID: customFieldString(r.CustomFields, "source_id"),
	}
}

func customFieldString(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	switch v := fields[key].(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func (c *contactClient) GetContacts() ([]*NBContact, error) {
	rows, err := restListAll[restContact](c, "/api/tenancy/contacts/")
	if err != nil {
		return nil, err
	}
	out := make([]*NBContact, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toNBContact())
	}
	return out, nil
}

func (c *contactClient) CreateContact(name string, extra map[string]any) (*NBContact, error) {
	payload := map[string]any{"name": name}
	for k, v := range extra {
		payload[k] = v
	}
	var created restContact
	if err := c.RestPost("/api/tenancy/contacts/", payload, &created); err != nil {
		return nil, err
	}
	return created.toNBContact(), nil
}

func (c *contactClient) UpdateContact(contactID uint, changes map[string]any) error {
	return c.RestPatch("/api/tenancy/contacts/"+strconv.FormatUint(uint64(contactID), 10)+"/", changes, nil)
}

func (c *contactClient) EnsureContactRole(name, slug string) (*NBContactRole, error) {
	path := "/api/tenancy/contact-roles/?slug=" + url.QueryEscape(slug)
	roles, err := restListAll[NBContactRole](c, path)
	if err != nil {
		return nil, err
	}
	if len(roles) > 0 {
		r := roles[0]
		return &r, nil
	}
	var created NBContactRole
	if err := c.RestPost("/api/tenancy/contact-roles/", map[string]any{
		"name": name,
		"slug": slug,
	}, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

type restContactAssignment struct {
	ID         uint   `json:"id"`
	ObjectType string `json:"object_type"`
	ObjectID   uint   `json:"object_id"`
	Contact    struct {
		ID uint `json:"id"`
	} `json:"contact"`
	Role struct {
		ID uint `json:"id"`
	} `json:"role"`
}

func (c *contactClient) GetContactAssignments(roleID uint) ([]*NBContactAssignment, error) {
	path := "/api/tenancy/contact-assignments/"
	if roleID > 0 {
		path += "?role_id=" + strconv.FormatUint(uint64(roleID), 10)
	}
	rows, err := restListAll[restContactAssignment](c, path)
	if err != nil {
		return nil, err
	}
	out := make([]*NBContactAssignment, 0, len(rows))
	for _, r := range rows {
		out = append(out, &NBContactAssignment{
			NetboxID:   r.ID,
			ObjectType: r.ObjectType,
			ObjectID:   r.ObjectID,
			ContactID:  r.Contact.ID,
			RoleID:     r.Role.ID,
		})
	}
	return out, nil
}

func (c *contactClient) CreateContactAssignment(objectType string, objectID, contactID, roleID uint) error {
	return c.RestPost("/api/tenancy/contact-assignments/", map[string]any{
		"object_type": objectType,
		"object_id":   objectID,
		"contact":     contactID,
		"role":        roleID,
		"priority":    "primary",
	}, nil)
}

type contactSyncAction int

const (
	contactUnchanged contactSyncAction = iota
	contactCreated
	contactUpdated
)

type contactIndex struct {
	bySourceID map[string]*NBContact
	byEmail    map[string][]*NBContact
	liveIDs    map[string]struct{}
}

func newContactIndex(contacts []*NBContact, liveIDs map[string]struct{}) *contactIndex {
	idx := &contactIndex{
		bySourceID: make(map[string]*NBContact, len(contacts)),
		byEmail:    make(map[string][]*NBContact),
		liveIDs:    liveIDs,
	}
	for _, c := range contacts {
		idx.add(c)
	}
	return idx
}

func (idx *contactIndex) add(c *NBContact) {
	if c.CfSource == "factum" && c.CfSourceID != "" {
		idx.bySourceID[c.CfSourceID] = c
	}
	if email := strings.ToLower(strings.TrimSpace(c.Email)); email != "" {
		idx.byEmail[email] = append(idx.byEmail[email], c)
	}
}

func (idx *contactIndex) claim(c *NBContact, sourceID string) {
	if c.CfSourceID != "" && c.CfSourceID != sourceID {
		delete(idx.bySourceID, c.CfSourceID)
	}
	c.CfSource = "factum"
	c.CfSourceID = sourceID
	idx.bySourceID[sourceID] = c
}

func contactClaimable(c *NBContact, sourceID string, liveIDs map[string]struct{}) bool {
	if c.CfSource != "factum" || c.CfSourceID == "" || c.CfSourceID == sourceID {
		return true
	}
	if liveIDs == nil {
		return false
	}
	_, live := liveIDs[c.CfSourceID]
	return !live
}

func contactCustomFields(sourceID string) map[string]any {
	return map[string]any{
		"source":    "factum",
		"source_id": sourceID,
	}
}

func isSkippableContactErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "400 Bad Request") ||
		strings.Contains(s, "already exists") ||
		strings.Contains(s, "Enter a valid email")
}

func contactFieldsEqual(nb *NBContact, name, email, phone string) bool {
	return nb.Name == name && nb.Email == email && nb.Phone == phone
}

func adoptByEmail(idx *contactIndex, email, sourceID string) *NBContact {
	if email == "" {
		return nil
	}
	var claimable []*NBContact
	for _, c := range idx.byEmail[email] {
		if contactClaimable(c, sourceID, idx.liveIDs) {
			claimable = append(claimable, c)
		}
	}
	if len(claimable) == 1 {
		return claimable[0]
	}
	return nil
}

// ensureContact creates or updates the Netbox contact for factum contact
// against idx. Matching order: custom-field source_id, then a unique
// claimable email, then create. Name is not used as a match key — Netbox
// does not unique-constrain contact names.
func ensureContact(nb contactAPI, contact models.Contact, idx *contactIndex) (*NBContact, contactSyncAction, error) {
	sourceID := strconv.FormatUint(uint64(contact.ID), 10)
	name := strings.TrimSpace(contact.Name)
	email := strings.TrimSpace(contact.Email)
	phone := strings.TrimSpace(contact.Phone)
	customFields := contactCustomFields(sourceID)

	if existing, ok := idx.bySourceID[sourceID]; ok {
		if contactFieldsEqual(existing, name, email, phone) {
			return existing, contactUnchanged, nil
		}
		if err := nb.UpdateContact(existing.NetboxID, map[string]any{
			"name":          name,
			"email":         email,
			"phone":         phone,
			"custom_fields": customFields,
		}); err != nil {
			return nil, contactUnchanged, err
		}
		existing.Name = name
		existing.Email = email
		existing.Phone = phone
		return existing, contactUpdated, nil
	}

	if adopted := adoptByEmail(idx, strings.ToLower(email), sourceID); adopted != nil {
		if err := nb.UpdateContact(adopted.NetboxID, map[string]any{
			"name":          name,
			"email":         email,
			"phone":         phone,
			"custom_fields": customFields,
		}); err != nil {
			return nil, contactUnchanged, err
		}
		adopted.Name = name
		adopted.Email = email
		adopted.Phone = phone
		idx.claim(adopted, sourceID)
		return adopted, contactUpdated, nil
	}

	created, err := nb.CreateContact(name, map[string]any{
		"email":         email,
		"phone":         phone,
		"custom_fields": customFields,
	})
	if err != nil {
		return nil, contactUnchanged, err
	}
	created.CfSource = "factum"
	created.CfSourceID = sourceID
	idx.add(created)
	return created, contactCreated, nil
}

// syncContactsToNetbox creates/updates a Netbox contact for every factum
// contact, matched via custom fields source="factum" and
// source_id=<contact.ID> (factum's own primary key, the same keying as
// customer→tenant sync). Contacts that already exist in Netbox under the
// same email but without those custom fields are adopted rather than
// POSTed. A name-only match is never used. Contacts are never deleted
// here: a contact removed from factum leaves its Netbox row untouched.
//
// CustomerContact join rows become tenancy.ContactAssignment rows on the
// matching tenant (object_type tenancy.tenant) using a dedicated "Factum"
// contact role. Assignments with any other role are left alone, and
// stale factum-role assignments are not deleted.
func syncContactsToNetbox(db *gorm.DB, nb contactAPI, reporter jobevent.Reporter) error {
	var contacts []models.Contact
	if err := db.Order("id").Find(&contacts).Error; err != nil {
		return err
	}

	nbContacts, err := nb.GetContacts()
	if err != nil {
		return err
	}
	liveIDs := make(map[string]struct{}, len(contacts))
	for _, c := range contacts {
		liveIDs[strconv.FormatUint(uint64(c.ID), 10)] = struct{}{}
	}
	idx := newContactIndex(nbContacts, liveIDs)

	factumToNetbox := make(map[uint]uint, len(contacts))
	var countNew, countUpdated, countSkipped int
	for _, contact := range contacts {
		if strings.TrimSpace(contact.Name) == "" {
			reporter.Emit(jobevent.Warning, "Netbox contact sync: skipping contact id=%d: empty name", contact.ID)
			countSkipped++
			continue
		}
		got, action, err := ensureContact(nb, contact, idx)
		if err != nil {
			if isSkippableContactErr(err) {
				reporter.Emit(jobevent.Warning, "Netbox contact sync: skipping contact %q (id=%d): %v", contact.Name, contact.ID, err)
				countSkipped++
				continue
			}
			return err
		}
		factumToNetbox[contact.ID] = got.NetboxID
		switch action {
		case contactCreated:
			countNew++
		case contactUpdated:
			countUpdated++
		}
	}
	reporter.Emit(jobevent.Info, "Netbox contact sync: %d new, %d updated, %d skipped", countNew, countUpdated, countSkipped)

	return syncContactAssignments(db, nb, factumToNetbox, reporter)
}

func syncContactAssignments(db *gorm.DB, nb contactAPI, factumToNetbox map[uint]uint, reporter jobevent.Reporter) error {
	var links []models.CustomerContact
	if err := db.Find(&links).Error; err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}

	role, err := nb.EnsureContactRole(factumContactRoleName, factumContactRoleSlug)
	if err != nil {
		return err
	}

	tenants, err := nb.GetTenants()
	if err != nil {
		return err
	}
	tenantByCustomer := make(map[uint]uint, len(tenants))
	for _, t := range tenants {
		if t.CfSource != "factum" || t.CfSourceID == "" {
			continue
		}
		id, convErr := strconv.ParseUint(t.CfSourceID, 10, 64)
		if convErr != nil || id == 0 {
			continue
		}
		tenantByCustomer[uint(id)] = t.NetboxID
	}

	existing, err := nb.GetContactAssignments(role.NetboxID)
	if err != nil {
		return err
	}
	have := make(map[string]struct{}, len(existing))
	for _, a := range existing {
		if a.ObjectType != tenantObjectType {
			continue
		}
		have[assignmentKey(a.ObjectID, a.ContactID, a.RoleID)] = struct{}{}
	}

	var created, skipped int
	for _, link := range links {
		contactID, ok := factumToNetbox[link.ContactID]
		if !ok {
			skipped++
			continue
		}
		tenantID, ok := tenantByCustomer[link.CustomerID]
		if !ok {
			skipped++
			continue
		}
		key := assignmentKey(tenantID, contactID, role.NetboxID)
		if _, exists := have[key]; exists {
			continue
		}
		if err := nb.CreateContactAssignment(tenantObjectType, tenantID, contactID, role.NetboxID); err != nil {
			if isSkippableContactErr(err) {
				reporter.Emit(jobevent.Warning, "Netbox contact assignment: skipping customer %d contact %d: %v", link.CustomerID, link.ContactID, err)
				skipped++
				continue
			}
			return err
		}
		have[key] = struct{}{}
		created++
	}
	reporter.Emit(jobevent.Info, "Netbox contact assignment sync: %d new, %d skipped", created, skipped)
	return nil
}

func assignmentKey(objectID, contactID, roleID uint) string {
	return fmt.Sprintf("%d:%d:%d", objectID, contactID, roleID)
}
