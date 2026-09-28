package worker

import (
	"context"
	"fmt"
	"net/http"

	"github.com/abundo/factum2/internal/radius"
)

// runRadius serves switch and router login when this host's command
// allowlist contains "radius". The UDP listener uses the last config it
// fetched; a hub outage does not stop authentication.
func (w *Worker) runRadius(ctx context.Context) error {
	if w.cfg == nil {
		return nil
	}
	if _, ok := w.cfg.Commands[radius.Role]; !ok {
		return nil
	}
	return radius.Run(ctx, radius.Options{
		StatePath: radius.StatePath(w.cfg.RadiusState),
		Fetch: func(ctx context.Context) ([]byte, error) {
			status, body, err := w.DoHubRequest(ctx, http.MethodGet, "/api/radius-config", nil)
			if err != nil {
				return nil, err
			}
			if status != http.StatusOK {
				return nil, fmt.Errorf("radius-config status %d", status)
			}
			return body, nil
		},
		Report: func(ctx context.Context, body []byte) error {
			status, resp, err := w.DoHubRequest(ctx, http.MethodPost, "/api/radius-events", body)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("radius-events status %d %s", status, resp)
			}
			return nil
		},
	})
}
