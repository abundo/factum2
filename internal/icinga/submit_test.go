package icinga

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/abundo/factum2/internal/util"
)

func TestAPISubmitReusesHTTP1Connection(t *testing.T) {
	var requests atomic.Int32
	var conns atomic.Int32
	var proto atomic.Value
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		proto.Store(r.Proto)
		io.Copy(io.Discard, r.Body)
		if r.URL.Path != "/v1/actions/process-check-result" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"results":[{"code":200,"status":"ok"}]}`)
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := NewIcingaClient(util.ConfigIcinga{URL: srv.URL, Username: "root", Password: "icinga"})
	submit := &apiSubmitter{icinga: client}
	results := []checkResult{
		{Host: "r1.lab.example", Service: "iface Ethernet1", Status: statusOK, Output: "up", Perf: "in_bps=1"},
		{Host: "r1.lab.example", Service: "iface Ethernet2", Status: statusCrit, Output: "down"},
		{Host: "r1.lab.example", Service: "eline 102", Status: statusOK, Output: "up"},
	}
	if err := submit.Submit(t.Context(), results); err != nil {
		t.Fatal(err)
	}
	if err := submit.Submit(t.Context(), results[:1]); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("requests = %d, want 4", got)
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
	if got, _ := proto.Load().(string); got != "HTTP/1.1" {
		t.Fatalf("protocol = %q, want HTTP/1.1", got)
	}
}
