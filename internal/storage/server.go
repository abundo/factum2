package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// MaxChunk is the largest PUT body the unix API accepts (hub frames stay under 32MiB).
const MaxChunk = 8 << 20

func NewAPIHandler(repo *Repo) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /files", func(w http.ResponseWriter, r *http.Request) {
		ents, err := repo.List(r.URL.Query().Get("path"))
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ents)
	})
	mux.HandleFunc("GET /files/content", func(w http.ResponseWriter, r *http.Request) {
		f, ent, err := repo.Open(r.URL.Query().Get("path"))
		if err != nil {
			writeRepoError(w, err)
			return
		}
		defer f.Close()
		offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Storage-Size", strconv.FormatInt(ent.Size, 10))
		w.Header().Set("X-Storage-Name", ent.Name)
		if limit > 0 {
			_, _ = io.Copy(w, io.LimitReader(f, limit))
			return
		}
		_, _ = io.Copy(w, f)
	})
	mux.HandleFunc("PUT /files", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		body := io.LimitReader(r.Body, MaxChunk+1)
		data, err := io.ReadAll(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(data) > MaxChunk {
			http.Error(w, "chunk too large", http.StatusRequestEntityTooLarge)
			return
		}
		n, err := repo.WriteAt(path, offset, bytes.NewReader(data))
		if err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"written": n})
	})
	mux.HandleFunc("DELETE /files", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.Remove(r.URL.Query().Get("path")); err != nil {
			writeRepoError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /mkdir", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := repo.Mkdir(body.Path); err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"path": body.Path})
	})
	mux.HandleFunc("POST /move", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := repo.Move(body.From, body.To); err != nil {
			writeRepoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"from": body.From, "to": body.To})
	})
	return mux
}

// NewDeviceHTTP serves repository files for device pull (no auth).
func NewDeviceHTTP(repo *Repo) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /files/", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/files")
		f, ent, err := repo.Open(rel)
		if err != nil {
			writeRepoError(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(ent.Size, 10))
		w.Header().Set("Content-Disposition", `attachment; filename="`+ent.Name+`"`)
		http.ServeContent(w, r, ent.Name, ent.ModTime, f)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRepoError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrExists):
		status = http.StatusConflict
	case errors.Is(err, ErrNotEmpty):
		status = http.StatusConflict
	case errors.Is(err, ErrIsDir), errors.Is(err, ErrNotDir), errors.Is(err, ErrInvalidPath):
		status = http.StatusBadRequest
	default:
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
