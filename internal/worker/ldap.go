package worker

import (
	"context"
	"fmt"
	"net/http"

	"github.com/abundo/factum2/internal/ldapsrv"
)

// runLDAP serves the internal directory when this host's command allowlist
// contains "ldap". The listener uses the last snapshot it fetched, so a
// hub outage does not stop binds.
func (w *Worker) runLDAP(ctx context.Context) error {
	if w.cfg == nil {
		return nil
	}
	if _, ok := w.cfg.Commands[ldapsrv.Command]; !ok {
		return nil
	}
	return ldapsrv.Run(ctx, ldapsrv.Options{
		StatePath:   ldapsrv.StatePath(w.cfg.LdapState),
		TLSCert:     w.cfg.TLSCert,
		TLSKey:      w.cfg.TLSKey,
		LegoStorage: w.cfg.Paths.LegoStorage,
		Fetch: func(ctx context.Context) ([]byte, error) {
			status, body, err := w.DoHubRequest(ctx, http.MethodGet, "/api/ldap-config", nil)
			if err != nil {
				return nil, err
			}
			if status != http.StatusOK {
				return nil, fmt.Errorf("ldap-config status %d", status)
			}
			return body, nil
		},
	})
}
