package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// 1×1 PNG (89 PNG ... IEND).
var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

var webpHeader = append([]byte("RIFF"), append([]byte{0x0c, 0x00, 0x00, 0x00}, []byte("WEBPVP8 ")...)...)

func binaryRequest(t *testing.T, method, path string, body []byte, contentType string, paramNames, paramValues []string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if contentType != "" {
		req.Header.Set(echo.HeaderContentType, contentType)
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	pathValues := make(echo.PathValues, 0, len(paramNames))
	for i, name := range paramNames {
		pathValues = append(pathValues, echo.PathValue{Name: name, Value: paramValues[i]})
	}
	c.SetPathValues(pathValues)
	return c, rec
}

func typeIDParams(id uint) ([]string, []string) {
	return []string{"id"}, []string{strconv.FormatUint(uint64(id), 10)}
}

func typeCTParams(typeID, ctID uint) ([]string, []string) {
	return []string{"id", "ctid"}, []string{
		strconv.FormatUint(uint64(typeID), 10),
		strconv.FormatUint(uint64(ctID), 10),
	}
}

func TestApiConfigServiceTypeCreateDuplicateName(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	createTestELINEType(t, db)
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name": "ELINE",
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiConfigServiceTypeConnectionTypesReplace(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name":       "ELINE",
		"interfaces": map[string]any{"min": 2, "max": 2, "unique": true},
		"connection_types": []map[string]any{
			{"name": "nni"},
			{"name": "uni"},
		},
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.ConnectionTypes) != 2 {
		t.Fatalf("connection_types = %+v", created.ConnectionTypes)
	}
	nniID, uniID := created.ConnectionTypes[0].ID, created.ConnectionTypes[1].ID
	if nniID == 0 || uniID == 0 {
		t.Fatal("expected ids on created connection types")
	}

	pn, pv := typeCTParams(created.ID, nniID)
	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image", png1x1, "image/png", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("image put status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Swap names; image on nni id must survive.
	pn, pv = typeIDParams(created.ID)
	c, rec = jsonRequest(t, http.MethodPut, "/api/config/service-types/x", map[string]any{
		"name":       "ELINE",
		"interfaces": map[string]any{"min": 2, "max": 2, "unique": true},
		"connection_types": []map[string]any{
			{"id": uniID, "name": "nni"},
			{"id": nniID, "name": "uni"},
		},
	}, pn, pv)
	if err := ctrl.ApiConfigServiceTypeUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("swap status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var swapped models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &swapped); err != nil {
		t.Fatal(err)
	}
	if len(swapped.ConnectionTypes) != 2 {
		t.Fatalf("after swap: %+v", swapped.ConnectionTypes)
	}
	var nniAfter, uniAfter models.ServiceConnectionTypeDTO
	for _, ct := range swapped.ConnectionTypes {
		switch ct.Name {
		case "nni":
			nniAfter = ct
		case "uni":
			uniAfter = ct
		}
	}
	if nniAfter.ID != uniID || uniAfter.ID != nniID {
		t.Fatalf("swap ids: nni=%d uni=%d, want nni=%d uni=%d", nniAfter.ID, uniAfter.ID, uniID, nniID)
	}
	if !uniAfter.HasImage {
		t.Fatal("image not preserved on id after name swap")
	}
	if nniAfter.HasImage {
		t.Fatal("image leaked onto the other connection type")
	}

	pn, pv = typeCTParams(created.ID, nniID)
	c, rec = binaryRequest(t, http.MethodGet, "/api/config/service-types/x/connection-types/y/image", nil, "", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImageGet(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("image get status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("nosniff = %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if !bytes.Equal(rec.Body.Bytes(), png1x1) {
		t.Fatal("image bytes mismatch")
	}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	var catalog, cliFolder, typeFolder models.ConfigScope
	if err := db.Where("parent_id = ? AND name = ?", root.ID, models.ConfigCatalogName).First(&catalog).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("parent_id = ? AND name = ?", catalog.ID, models.ConfigCatalogCLIName).First(&cliFolder).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("parent_id = ? AND name = ?", cliFolder.ID, "ELINE").First(&typeFolder).Error; err != nil {
		t.Fatalf("catalog CLI folder missing: %v", err)
	}
}

func TestApiConfigServiceTypeConnectionTypeInUse(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name": "ELINE",
		"connection_types": []map[string]any{
			{"name": "nni"},
			{"name": "spare"},
		},
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	nniID := created.ConnectionTypes[0].ID
	spareID := created.ConnectionTypes[1].ID
	svc := models.Service{ServiceID: "CN00001", ServiceType: "ELINE", ConnectionTypeID: &nniID}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}

	pn, pv := typeIDParams(created.ID)
	c, rec = jsonRequest(t, http.MethodPut, "/api/config/service-types/x", map[string]any{
		"name": "ELINE",
		"connection_types": []map[string]any{
			{"id": spareID, "name": "spare"},
		},
	}, pn, pv)
	if err := ctrl.ApiConfigServiceTypeUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/service-types/x", map[string]any{
		"name": "ELINE",
		"connection_types": []map[string]any{
			{"id": nniID, "name": "nni"},
		},
	}, pn, pv)
	if err := ctrl.ApiConfigServiceTypeUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("omit unused status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var n int64
	if err := db.Model(&models.ServiceConnectionType{}).Where("id = ?", spareID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("omitted unused connection type not deleted")
	}
}

func TestApiConfigServiceTypeConnectionTypeDuplicateNames(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name": "X",
		"connection_types": []map[string]any{
			{"name": "nni"},
			{"name": "nni"},
		},
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiConfigServiceTypeConnectionTypeForeignID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	a := createTestELINEType(t, db)
	other := models.ServiceType{Name: "ELAN"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	ct := models.ServiceConnectionType{ServiceTypeID: other.ID, Name: "nni"}
	if err := db.Create(&ct).Error; err != nil {
		t.Fatal(err)
	}
	pn, pv := typeIDParams(a.ID)
	c, rec := jsonRequest(t, http.MethodPut, "/api/config/service-types/x", map[string]any{
		"name": "ELINE",
		"connection_types": []map[string]any{
			{"id": ct.ID, "name": "stolen"},
		},
	}, pn, pv)
	if err := ctrl.ApiConfigServiceTypeUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiConfigConnectionTypeImageValidation(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name":             "ELINE",
		"connection_types": []map[string]any{{"name": "nni"}},
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	var created models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	ctID := created.ConnectionTypes[0].ID
	pn, pv := typeCTParams(created.ID, ctID)

	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("svg status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image",
		png1x1, "image/webp", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image",
		webpHeader, "image/webp", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("webp status = %d, body=%s", rec.Code, rec.Body.String())
	}

	tooBig := bytes.Repeat([]byte("x"), maxConnectionTypeImage+1)
	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image",
		tooBig, "image/png", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d, want 413, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image",
		[]byte{}, "image/png", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, body=%s", rec.Code, rec.Body.String())
	}
	c, rec = binaryRequest(t, http.MethodGet, "/api/config/service-types/x/connection-types/y/image", nil, "", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImageGet(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cleared get status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiConfigServiceTypeListOmitsImage(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name":             "ELINE",
		"connection_types": []map[string]any{{"name": "nni"}},
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	var created models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	pn, pv := typeCTParams(created.ID, created.ConnectionTypes[0].ID)
	c, rec = binaryRequest(t, http.MethodPut, "/api/config/service-types/x/connection-types/y/image", png1x1, "image/png", pn, pv)
	if err := ctrl.ApiConfigConnectionTypeImagePut(c); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/service-types", nil, nil, nil)
	if err := ctrl.ApiConfigServiceTypeList(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"image"`) && strings.Contains(rec.Body.String(), "iVBORw") {
		t.Fatal("list leaked image bytes")
	}
	var listed []models.ServiceTypeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || len(listed[0].ConnectionTypes) != 1 {
		t.Fatalf("listed = %+v", listed)
	}
	ct := listed[0].ConnectionTypes[0]
	if !ct.HasImage || ct.ImageURL == "" || ct.ContentType != "image/png" {
		t.Fatalf("list connection type = %+v", ct)
	}
	loaded, err := cfgmgmt.ListServiceTypes(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[0].ConnectionTypes) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	row := loaded[0].ConnectionTypes[0]
	if len(row.Image) != 0 {
		t.Fatalf("list preloaded %d image bytes", len(row.Image))
	}
	if !row.HasImage {
		t.Fatal("list HasImage = false after omitting image column")
	}
	one, err := cfgmgmt.LoadServiceType(db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.ConnectionTypes) != 1 || len(one.ConnectionTypes[0].Image) != 0 || !one.ConnectionTypes[0].HasImage {
		t.Fatalf("get type loaded image=%d has=%v", len(one.ConnectionTypes[0].Image), one.ConnectionTypes[0].HasImage)
	}
}
