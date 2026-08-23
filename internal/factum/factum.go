package factum

//
// Factum
//

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"

	"github.com/abundo/netboxtool"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

type FactumClient struct {
	p *util.ConfigFactum
}

// type FactumDeviceResponse2 struct {
// 	models.Device
// }

type FactumDeviceResponse struct {
	Data []*netboxtool.NBDevice `json:"data"`
}

func NewFactumClient(config *util.ConfigFactum) *FactumClient {
	client := new(FactumClient)
	client.p = config
	return client
}

// Fetch devices from Factum API devices
func (factum *FactumClient) GetDeviceHelper(name string) ([]*models.Device, error) {
	path := "/api/device"
	if name != "" {
		// web.ApiGetDeviceByName reads the name as a single path segment, so
		// anything in it that looks like one (or like a query string) has to
		// be escaped.
		path += "/name/" + neturl.PathEscape(name)
	}
	return factum.getDevices(path)
}

func (factum *FactumClient) GetDevice(name string) ([]*models.Device, error) {
	devices, err := factum.GetDeviceHelper(name)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

func (factum *FactumClient) GetDevices() ([]*models.Device, error) {
	return factum.GetDeviceHelper("")
}

// GetDevicesWithInterfaces is GET /api/device?include=interfaces. The
// default /api/device list is shallow (no nested interfaces/addresses) so
// the device-list UI stays cheap; DNS sync needs the nested addresses to
// emit per-interface A/AAAA records.
func (factum *FactumClient) GetDevicesWithInterfaces() ([]*models.Device, error) {
	return factum.getDevices("/api/device?include=interfaces")
}

// getDevices GETs path and decodes a []*models.Device body. Shared by
// GetDeviceHelper (list or by-name) and GetDevicesWithInterfaces. Transport
// is util.FactumHTTP (unix hub socket when the probe succeeds, else HTTPS).
func (factum *FactumClient) getDevices(path string) ([]*models.Device, error) {
	var tmp []*models.Device
	if err := factum.doJSON(http.MethodGet, path, nil, &tmp); err != nil {
		return nil, err
	}
	return tmp, nil
}

// GetDeviceByName returns the one device called name. web.ApiGetDeviceByName
// answers with an array, so an unknown name is an empty list and a 200, not
// an HTTP error - hence the explicit not-found here.
func (factum *FactumClient) GetDeviceByName(name string) (*models.Device, error) {
	if name == "" {
		return nil, errors.New("no device name given")
	}
	devices, err := factum.GetDeviceHelper(name)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("unknown device %q", name)
	}
	return devices[0], nil
}

// Job mirrors the fields of models.Job (plus its embedded JobTask
// children) this client needs - trimmed down rather than importing the
// model directly, since gorm's FactumModel embed and json:"-" fields have
// no meaning for an HTTP response DTO.
type Job struct {
	ID         uint       `json:"id"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Tasks      []JobTask  `json:"tasks"`
}

// JobTask mirrors the fields of models.JobTask this client needs.
type JobTask struct {
	Target     string     `json:"target"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   int        `json:"exit_code"`
}

// doJSON does a request against the Factum API and decodes a JSON response
// into out (skipped if out is nil) - shared by getDevices and the sync /
// pending-delete methods, which need a request body and/or a non-GET method.
// Transport is util.FactumHTTP: unix hub socket when the probe succeeds
// (no bearer; the socket ACL is the auth), else HTTPS with
// Authorization: Bearer {token}. Empty body []byte{} is a present body
// (Content-Type application/json), used by TriggerSync.
func (factum *FactumClient) doJSON(method, path string, body []byte, out any) error {
	client, baseURL, viaSocket, err := util.FactumHTTP(factum.p)
	if err != nil {
		return err
	}
	url := baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return err
	}
	if !viaSocket {
		req.Header.Set("Authorization", "Bearer "+factum.p.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &errBody) == nil && errBody.Error != "" {
			return fmt.Errorf("%s %s: %s", method, url, errBody.Error)
		}
		return fmt.Errorf("%s %s: %s", method, url, resp.Status)
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// GetSyncTargets returns the sync targets currently enabled in Settings, in
// worker.SyncTargets order - GET /api/sync/targets (web.ApiSyncTargets).
func (factum *FactumClient) GetSyncTargets() ([]string, error) {
	var targets []string
	if err := factum.doJSON(http.MethodGet, "/api/sync/targets", nil, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// TriggerSync dispatches target to a connected worker node and returns the
// created job's numeric ID - POST /api/sync/:target (web.ApiSyncTrigger).
func (factum *FactumClient) TriggerSync(target string) (uint, error) {
	var result struct {
		JobID uint `json:"job_id"`
	}
	path := "/api/sync/" + neturl.PathEscape(target)
	if err := factum.doJSON(http.MethodPost, path, []byte{}, &result); err != nil {
		return 0, err
	}
	return result.JobID, nil
}

// TriggerSyncAll dispatches every enabled sync target as one job (one
// JobTask per target) and returns the created job's numeric ID - POST
// /api/sync/all (web.ApiSyncTriggerAll).
func (factum *FactumClient) TriggerSyncAll() (uint, error) {
	var result struct {
		JobID uint `json:"job_id"`
	}
	if err := factum.doJSON(http.MethodPost, "/api/sync/all", []byte{}, &result); err != nil {
		return 0, err
	}
	return result.JobID, nil
}

// GetJobs returns the most recent jobs, newest first, each with its tasks
// embedded - GET /api/jobs (web.ApiJobs).
func (factum *FactumClient) GetJobs() ([]Job, error) {
	var jobs []Job
	if err := factum.doJSON(http.MethodGet, "/api/jobs", nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}
