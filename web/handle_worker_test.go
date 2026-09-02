package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestApiWorkerNodeCreateAndUpdateTLSFields(t *testing.T) {
	db := newTestDB(t)
	ctrl := Controller{DB: db}

	ca := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	c, rec := jsonRequest(t, http.MethodPost, "/api/admin/worker-nodes", models.WorkerNodeDTO{
		Name:          "dns1",
		Address:       "dns1.example.com:8443",
		Token:         "secret",
		Enabled:       true,
		TLSSkipVerify: false,
		TLSCA:         ca,
	}, nil, nil)
	if err := ctrl.ApiWorkerNodeCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.WorkerNode
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.TLSCA != ca {
		t.Fatalf("created tls_ca = %q", created.TLSCA)
	}
	if created.TLSSkipVerify {
		t.Fatal("created skip-verify, want false")
	}

	id := strconv.FormatUint(uint64(created.ID), 10)
	c, rec = jsonRequest(t, http.MethodPut, "/api/admin/worker-nodes/"+id, models.WorkerNodeDTO{
		Name:          "dns1",
		Address:       "dns1.example.com:8443",
		Enabled:       true,
		TLSSkipVerify: true,
		TLSCA:         "",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiWorkerNodeUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status %d body=%s", rec.Code, rec.Body.String())
	}

	var stored models.WorkerNode
	if err := db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.TLSSkipVerify {
		t.Fatal("stored skip-verify, want true")
	}
	if stored.TLSCA != "" {
		t.Fatalf("stored tls_ca = %q, want empty", stored.TLSCA)
	}
	if stored.Token != "secret" {
		t.Fatalf("token overwritten: %q", stored.Token)
	}
}
