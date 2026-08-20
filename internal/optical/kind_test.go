package optical

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNormalizeOpticalKindCF(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"", ""},
		{"roadm", "roadm"},
		{"ROADM", "roadm"},
		{"transponder", "wdm_shelf"},
		{"muxponder", "wdm_shelf"},
		{"wdm_shelf", "wdm_shelf"},
		{"nope", ""},
		{map[string]any{"value": "roadm", "label": "ROADM"}, "roadm"},
		{map[string]any{"value": "transponder"}, "wdm_shelf"},
		{map[string]any{"label": "ROADM"}, ""},
		{42, ""},
	}
	for _, tc := range cases {
		if got := NormalizeOpticalKindCF(tc.in); got != tc.want {
			t.Errorf("NormalizeOpticalKindCF(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCustomFieldValue_PrefersOpticalRole(t *testing.T) {
	got := CustomFieldValue(map[string]any{
		"optical_kind": "ila",
		"optical_role": "roadm",
	}, "optical_role", "optical_kind")
	if got != "roadm" {
		t.Errorf("got %v, want roadm", got)
	}
	got = CustomFieldValue(map[string]any{"optical_kind": "ila"}, "optical_role", "optical_kind")
	if got != "ila" {
		t.Errorf("fallback got %v, want ila", got)
	}
}

func TestNormalizeOpticalPortRole(t *testing.T) {
	if got := NormalizeOpticalPortRole("roadm_degree"); got != "roadm_degree" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeOpticalPortRole(map[string]any{"value": "txp_line"}); got != "txp_line" {
		t.Errorf("selection got %q", got)
	}
	if got := NormalizeOpticalPortRole("roadm"); got != "" {
		t.Errorf("device kind must not be a port role, got %q", got)
	}
}

func TestResolveOpticalKind_CFThenMap(t *testing.T) {
	maps := map[string]string{"roadm": "roadm", "wdm chassis": "wdm_shelf"}
	if got := ResolveOpticalKind("wdm_shelf", "ROADM", maps); got != "wdm_shelf" {
		t.Errorf("CF must win: got %q", got)
	}
	if got := ResolveOpticalKind("transponder", "ROADM", maps); got != "wdm_shelf" {
		t.Errorf("CF alias: got %q", got)
	}
	if got := ResolveOpticalKind("", "ROADM", maps); got != "roadm" {
		t.Errorf("map fallback: got %q", got)
	}
	if got := ResolveOpticalKind("", "WDM Chassis", maps); got != "wdm_shelf" {
		t.Errorf("case-insensitive map: got %q", got)
	}
	if got := ResolveOpticalKind("", "Router", maps); got != "" {
		t.Errorf("unmapped: got %q", got)
	}
}

func TestReresolveAllKinds_DoesNotClobberCF(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:kind?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Device{}, &models.OpticalKindMap{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.OpticalKindMap{NetboxRoleName: "roadm", OpticalKind: "roadm"}).Error; err != nil {
		t.Fatal(err)
	}
	d := models.Device{Name: "dcp2", Role: "ROADM", OpticalKindCF: "wdm_shelf", OpticalKind: "wdm_shelf"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	if err := ReresolveAllKinds(db); err != nil {
		t.Fatal(err)
	}
	var got models.Device
	if err := db.First(&got, d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.OpticalKind != "wdm_shelf" {
		t.Errorf("OpticalKind = %q, want wdm_shelf (CF must survive mapping CRUD)", got.OpticalKind)
	}
}
