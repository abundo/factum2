package web

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestCreateAdminUser(t *testing.T) {
	db := newTestDB(t)

	if err := createAdminUser(db, ""); err == nil {
		t.Fatal("empty password: expected error")
	}

	if err := createAdminUser(db, "lab-admin-pass"); err != nil {
		t.Fatalf("create: %v", err)
	}

	var user models.User
	if err := db.Preload("Roles").Where("username = ?", adminUsername).First(&user).Error; err != nil {
		t.Fatalf("load admin: %v", err)
	}
	if !CheckPasswordHash("lab-admin-pass", user.PasswordHash) {
		t.Fatal("created admin password hash does not match")
	}
	got := map[string]bool{}
	for _, role := range user.Roles {
		got[role.Name] = true
	}
	if !got[adminRoleName] {
		t.Fatalf("admin roles = %v, want %q", got, adminRoleName)
	}
	for name := range standardRoles {
		var role models.Role
		if err := db.Where("name = ?", name).First(&role).Error; err != nil {
			t.Fatalf("role %q: %v", name, err)
		}
	}

	if err := createAdminUser(db, "new-lab-pass"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.Where("username = ?", adminUsername).First(&user).Error; err != nil {
		t.Fatalf("reload admin: %v", err)
	}
	if !CheckPasswordHash("new-lab-pass", user.PasswordHash) {
		t.Fatal("updated admin password hash does not match")
	}
	if CheckPasswordHash("lab-admin-pass", user.PasswordHash) {
		t.Fatal("updated admin still accepts the old password")
	}
}

func TestAdminPassword(t *testing.T) {
	got, err := adminPassword("from-flag")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-flag" {
		t.Fatalf("flag: got %q", got)
	}

	t.Setenv("FACTUM_ADMIN_PASSWORD", "from-env")
	got, err = adminPassword("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Fatalf("env: got %q", got)
	}

	got, err = adminPassword("flag-wins")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-wins" {
		t.Fatalf("flag over env: got %q", got)
	}
}
