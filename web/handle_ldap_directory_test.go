package web

import (
	"testing"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/abundo/factum2/models"
)

func TestDirectoryWriteBlock(t *testing.T) {
	wb := ldapauth.WritebackConfig{BindDN: "cn=admin,dc=example,dc=com", Password: "secret"}
	if code, _ := directoryWriteBlock("generic", false, wb); code != 400 {
		t.Fatalf("disabled code = %d", code)
	}
	if code, _ := directoryWriteBlock("ad", true, wb); code != 403 {
		t.Fatalf("ad code = %d", code)
	}
	if code, _ := directoryWriteBlock("generic", true, ldapauth.WritebackConfig{}); code != 400 {
		t.Fatalf("missing writeback code = %d", code)
	}
	if code, msg := directoryWriteBlock("generic", true, wb); code != 0 || msg != "" {
		t.Fatalf("writable code = %d %q", code, msg)
	}
	blankType := &models.Settings{LdapEnabled: boolPtr(true)}
	status := directoryStatus(blankType, wb)
	if !status.Writable || status.ServerType != "generic" {
		t.Fatalf("blank server type status = %+v", status)
	}
}

func TestRewriteGroupDNReferences(t *testing.T) {
	db := newTestDB(t)
	role := models.Role{Name: "ops", Description: "ops"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	mapping := models.LdapRoleMapping{GroupDN: "cn=admins,ou=groups,dc=example,dc=com", RoleID: role.ID}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatal(err)
	}
	policy := models.RadiusPolicy{GroupDN: "CN=Admins,OU=Groups,DC=example,DC=com", AllDevices: true}
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	other := models.LdapRoleMapping{GroupDN: "cn=other,dc=example,dc=com", RoleID: role.ID}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}

	newDN := "cn=operators,ou=groups,dc=example,dc=com"
	if err := rewriteGroupDNReferences(db, "CN=admins,OU=groups,DC=example,DC=com", newDN); err != nil {
		t.Fatal(err)
	}
	var gotMap models.LdapRoleMapping
	if err := db.First(&gotMap, mapping.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotMap.GroupDN != newDN {
		t.Fatalf("mapping DN = %q", gotMap.GroupDN)
	}
	var gotPolicy models.RadiusPolicy
	if err := db.First(&gotPolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotPolicy.GroupDN != newDN {
		t.Fatalf("policy DN = %q", gotPolicy.GroupDN)
	}
	var still models.LdapRoleMapping
	if err := db.First(&still, other.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.GroupDN != other.GroupDN {
		t.Fatalf("other mapping changed to %q", still.GroupDN)
	}

	if err := rewriteGroupDNReferences(db, newDN, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&gotMap, mapping.ID).Error; err == nil {
		t.Fatal("mapping should have been deleted")
	}
	if err := db.First(&gotPolicy, policy.ID).Error; err == nil {
		t.Fatal("policy should have been deleted")
	}
	if err := db.First(&still, other.ID).Error; err != nil {
		t.Fatal(err)
	}
}
