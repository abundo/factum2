package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/docs"
)

func TestApiDocsList(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/docs", nil, nil, nil)
	if err := ctrl.ApiDocsList(c); err != nil {
		t.Fatalf("ApiDocsList: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var pages []docs.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("empty catalog")
	}
	foundIndex, foundSBOM := false, false
	for _, p := range pages {
		switch p.Slug {
		case "index":
			foundIndex = true
		case "sbom":
			foundSBOM = true
		}
		if p.Markdown != "" {
			t.Errorf("list included markdown for %q", p.Slug)
		}
	}
	if !foundIndex {
		t.Error("list missing index")
	}
	if !foundSBOM {
		t.Error("list missing sbom")
	}
}

func TestApiDocsGet(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/docs/x", nil, []string{"slug"}, []string{"index"})
	if err := ctrl.ApiDocsGet(c); err != nil {
		t.Fatalf("ApiDocsGet: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page docs.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Slug != "index" || page.Markdown == "" {
		t.Errorf("got slug=%q markdown empty=%v", page.Slug, page.Markdown == "")
	}
}

func TestApiDocsGetInvalid(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/docs/x", nil, []string{"slug"}, []string{"../user"})
	if err := ctrl.ApiDocsGet(c); err != nil {
		t.Fatalf("ApiDocsGet: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestApiDocsGetMissing(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/docs/x", nil, []string{"slug"}, []string{"no-such-page"})
	if err := ctrl.ApiDocsGet(c); err != nil {
		t.Fatalf("ApiDocsGet: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
