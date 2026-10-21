package icinga

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// EOS has no pseudowire MIB. show patch panel over eAPI is the status
// of both a local patch and an LDP pseudowire. The account is the Icinga
// eAPI user, not a device-sync password.

const eosPatchCmd = "show patch panel"

var eosHTTP = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

type eosConn struct {
	status string
	pwID   int
	name   string
}

type eosPatch struct {
	status string
	conns  []eosConn
}

type eosShowFunc func(ctx context.Context, host string) (map[string]eosPatch, error)

func eosEAPIReady(platform, user, pass string) bool {
	return elineVendor(platform) == elineVendorEOS && strings.TrimSpace(user) != "" && pass != ""
}

func eosShowPatchPanel(ctx context.Context, host, user, pass string) (map[string]eosPatch, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "runCmds",
		"id":      "1",
		"params": map[string]any{
			"version": 1,
			"cmds":    []string{eosPatchCmd},
			"format":  "json",
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/command-api", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)
	resp, err := eosHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eAPI %s", resp.Status)
	}
	var parsed struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("eAPI %s", parsed.Error.Message)
	}
	if len(parsed.Result) != 1 {
		return nil, fmt.Errorf("eAPI returned %d results", len(parsed.Result))
	}
	return parseEOSPatches(parsed.Result[0])
}

func parseEOSPatches(raw json.RawMessage) (map[string]eosPatch, error) {
	var body struct {
		Patches map[string]struct {
			Status     string `json:"status"`
			Connectors map[string]struct {
				Status string `json:"status"`
				LDP    *struct {
					Name string `json:"name"`
					PWID int    `json:"pseudowireId"`
				} `json:"ldpPseudowireConnectorInfo"`
			} `json:"connectors"`
		} `json:"patches"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	out := make(map[string]eosPatch, len(body.Patches))
	for name, p := range body.Patches {
		keys := make([]string, 0, len(p.Connectors))
		for k := range p.Connectors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		conns := make([]eosConn, 0, len(keys))
		for _, k := range keys {
			c := p.Connectors[k]
			conn := eosConn{status: c.Status}
			if c.LDP != nil {
				conn.pwID = c.LDP.PWID
				conn.name = c.LDP.Name
			}
			conns = append(conns, conn)
		}
		out[name] = eosPatch{status: p.Status, conns: conns}
	}
	return out, nil
}

func findEOSPatch(patches map[string]eosPatch, want elineTarget) (eosPatch, bool) {
	if p, ok := patches[want.ServiceID]; ok {
		return p, true
	}
	var pwHits []string
	var nameHits []string
	for name, p := range patches {
		for _, c := range p.conns {
			if want.PWID > 0 && c.pwID == want.PWID {
				pwHits = append(pwHits, name)
			}
			if c.name != "" && (c.name == want.ServiceID || strings.EqualFold(c.name, want.ServiceID)) {
				nameHits = append(nameHits, name)
			}
		}
	}
	if len(nameHits) > 0 {
		sort.Strings(nameHits)
		return patches[nameHits[0]], true
	}
	if len(pwHits) > 0 {
		sort.Strings(pwHits)
		return patches[pwHits[0]], true
	}
	return eosPatch{}, false
}

func eosConnText(status string) (admin bool, text string) {
	switch strings.ToLower(status) {
	case "", "up":
		return false, ""
	case "admindown":
		return true, "admin down"
	case "noremoteend":
		return false, "no remote end"
	case "nolocalend":
		return false, "no local end"
	case "waitingforvlanid":
		return false, "waiting for vlan"
	case "connectorunprogrammed":
		return false, "unprogrammed"
	default:
		return false, strings.ToLower(status)
	}
}

func evalEOSPatch(t elineTarget, p eosPatch) elineReading {
	if strings.EqualFold(p.status, "up") {
		return readingFrom(t, statusOK, "up")
	}
	adminOnly := len(p.conns) > 0
	reason := ""
	for _, c := range p.conns {
		admin, text := eosConnText(c.status)
		if text == "" {
			adminOnly = false
			continue
		}
		if !admin {
			adminOnly = false
			if reason == "" {
				reason = text
			}
			continue
		}
	}
	if adminOnly {
		return readingFrom(t, statusOK, "admin down")
	}
	if reason == "" {
		return readingFrom(t, statusCrit, "down")
	}
	return readingFrom(t, statusCrit, "down ("+reason+")")
}

func readingsFromEOS(wants []elineTarget, patches map[string]eosPatch) map[string]elineReading {
	out := make(map[string]elineReading, len(wants))
	for _, want := range wants {
		p, ok := findEOSPatch(patches, want)
		if !ok {
			out[want.Service] = readingFrom(want, statusUnknown, "not in patch panel")
			continue
		}
		out[want.Service] = evalEOSPatch(want, p)
	}
	return out
}
