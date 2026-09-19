package storage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIHandlerCRUD(t *testing.T) {
	t.Parallel()
	repo, err := NewRepo(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewAPIHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/mkdir", bytes.NewReader([]byte(`{"path":"/eos"}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mkdir %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/files?path=/eos/a.bin", bytes.NewReader([]byte("hello")))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/files?path=/eos", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	var ents []Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &ents); err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name != "a.bin" {
		t.Fatalf("ents %+v", ents)
	}

	req = httptest.NewRequest(http.MethodPost, "/move", bytes.NewReader([]byte(`{"from":"/eos/a.bin","to":"/eos/b.bin"}`)))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/files?path=/eos/b.bin", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
}
