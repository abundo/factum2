package web

import (
	"testing"

	"github.com/abundo/factum2/models"
)

func TestDeviceSyncCredentials(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "defuser", Password: "defpass",
	}).Error; err != nil {
		t.Fatalf("create default: %v", err)
	}
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "lab-sw1", Username: "labuser", Password: "labpass",
	}).Error; err != nil {
		t.Fatalf("create override: %v", err)
	}

	got, err := ctrl.deviceSyncCredentials("lab-sw1")
	if err != nil {
		t.Fatalf("exact: %v", err)
	}
	if got.Username != "labuser" || got.Password != "labpass" {
		t.Errorf("exact = %+v, want labuser/labpass", got)
	}

	got, err = ctrl.deviceSyncCredentials("prod-sw1")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if got.Username != "defuser" || got.Password != "defpass" {
		t.Errorf("fallback = %+v, want defuser/defpass", got)
	}

	if err := db.Where("name = ?", "default").Delete(&models.DeviceSyncAuth{}).Error; err != nil {
		t.Fatalf("delete default: %v", err)
	}
	if _, err := ctrl.deviceSyncCredentials("prod-sw1"); err == nil {
		t.Fatal("missing default: expected error")
	}
}

func TestDeviceSyncCredentialsIncomplete(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "onlyuser",
	}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ctrl.deviceSyncCredentials("any"); err == nil {
		t.Fatal("incomplete: expected error")
	}
}

func TestServiceCommitComment(t *testing.T) {
	c, _ := jsonRequest(t, "POST", "/api/service/1/push", nil, nil, nil)
	if got := serviceCommitComment(c, "CN00042", "push"); got != "factum push CN00042" {
		t.Errorf("no user = %q", got)
	}

	c.Set("user", models.User{Username: "alice", Name: "Alice Andersson"})
	if got := serviceCommitComment(c, "CN00042", "push"); got != "factum push CN00042 by Alice Andersson" {
		t.Errorf("named user = %q", got)
	}

	c.Set("user", models.User{Username: "bob"})
	if got := serviceCommitComment(c, "CN00042", "remove"); got != "factum remove CN00042 by bob" {
		t.Errorf("username only = %q", got)
	}

	c.Set("user", models.User{Username: "carol", Name: "  "})
	if got := serviceCommitComment(c, "", "push"); got != "factum push service by carol" {
		t.Errorf("blank id/name = %q", got)
	}
}
