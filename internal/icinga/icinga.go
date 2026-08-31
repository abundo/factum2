package icinga

//
// Library to manage Icinga
//

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"net/http"
	"os/exec"
	"strings"

	"github.com/abundo/factum2/internal/util"
)

// HostState is the parsed, factum-relevant subset of an Icinga Host object
// (icinga's own https://icinga.com/docs/icinga2/latest/doc/12-icinga2-api/
// "results[].attrs" shape, with the "vars.factum_*" custom vars set by
// Settings.IcingaHostTemplate - see internal/icinga/factum2-icinga.go).
type HostState struct {
	Name                 string
	Acknowledgement      int
	State                int
	Address              string
	Address6             string
	LastHardState        int
	LastHardStateChanged time.Time

	Notes string

	FactumComments     string
	FactumLocation     string
	FactumManufacturer string
	FactumModel        string
	FactumPlatform     string
	FactumRole         string
	FactumSiteName     string
}

// icingaHostResult mirrors the raw JSON one entry of
// GET/POST .../v1/objects/hosts's "results" array - unexported, only used to
// unmarshal before converting to the flat HostState above.
type icingaHostResult struct {
	Name  string `json:"name"`
	Attrs struct {
		State                int     `json:"state"`
		Address              string  `json:"address"`
		Address6             string  `json:"address6"`
		LastHardState        int     `json:"last_hard_state"`
		LastHardStateChanged float64 `json:"last_hard_state_change"`
		Acknowledgement      int     `json:"acknowledgement"`
		Notes                string  `json:"notes"`
		Vars                 struct {
			FactumComments     string `json:"factum_comments"`
			FactumLocation     string `json:"factum_location"`
			FactumManufacturer string `json:"factum_manufacturer"`
			FactumModel        string `json:"factum_model"`
			FactumPlatform     string `json:"factum_platform"`
			FactumRole         string `json:"factum_role"`
			FactumSiteName     string `json:"factum_site_name"`
		} `json:"vars"`
	} `json:"attrs"`
}

func (r icingaHostResult) toHostState() HostState {
	a := r.Attrs
	return HostState{
		Name:                 r.Name,
		Acknowledgement:      a.Acknowledgement,
		State:                a.State,
		Address:              a.Address,
		Address6:             a.Address6,
		LastHardState:        a.LastHardState,
		LastHardStateChanged: time.Unix(int64(a.LastHardStateChanged), 0),
		Notes:                a.Notes,
		FactumComments:       a.Vars.FactumComments,
		FactumLocation:       a.Vars.FactumLocation,
		FactumManufacturer:   a.Vars.FactumManufacturer,
		FactumModel:          a.Vars.FactumModel,
		FactumPlatform:       a.Vars.FactumPlatform,
		FactumRole:           a.Vars.FactumRole,
		FactumSiteName:       a.Vars.FactumSiteName,
	}
}

type HostStateResult struct {
	Error   float32     `json:"error"`
	Status  string      `json:"status"`
	Results []HostState `json:"results"`
}

// ServiceState is the parsed, factum-relevant subset of an Icinga Service
// object.
type ServiceState struct {
	HostName             string
	Name                 string
	Acknowledgement      int
	State                int
	LastHardState        int
	LastHardStateChanged time.Time
	Output               string
	Notes                string
}

// icingaServiceResult mirrors the raw JSON one entry of
// GET/POST .../v1/objects/services's "results" array.
type icingaServiceResult struct {
	Name  string `json:"name"`
	Attrs struct {
		HostName             string  `json:"host_name"`
		State                int     `json:"state"`
		LastHardState        int     `json:"last_hard_state"`
		LastHardStateChanged float64 `json:"last_hard_state_change"`
		Acknowledgement      int     `json:"acknowledgement"`
		Notes                string  `json:"notes"`
		LastCheckResult      struct {
			Output string `json:"output"`
		} `json:"last_check_result"`
	} `json:"attrs"`
}

func (r icingaServiceResult) toServiceState() ServiceState {
	a := r.Attrs
	return ServiceState{
		HostName:             a.HostName,
		Name:                 r.Name,
		Acknowledgement:      a.Acknowledgement,
		State:                a.State,
		LastHardState:        a.LastHardState,
		LastHardStateChanged: time.Unix(int64(a.LastHardStateChanged), 0),
		Output:               a.LastCheckResult.Output,
		Notes:                a.Notes,
	}
}

type ServiceStateResult struct {
	Error   float32        `json:"error"`
	Status  string         `json:"status"`
	Results []ServiceState `json:"results"`
}

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type icingaClient struct {
	c util.ConfigIcinga
}

func NewIcingaClient(c util.ConfigIcinga) *icingaClient {
	client := new(icingaClient)
	client.c = c
	return client
}

/*
//
// Helper function to get attributes
//
func get(attr, keys string, default string) {

}
    def get(self, attr, keys, default=""):
        try:
            for key in keys.split("."):
                attr = attr[key]
            return attr
        except (KeyError, TypeError):
            print("didnt find %s" % keys)
            return default
*/

// var stateToString map[int]string

var stateToString = map[int]string{
	0: "OK",
	1: "WARNING",
	2: "CRITICAL",
	3: "UNKNOWN",
}

func state_to_str(state int) string {
	value, ok := stateToString[state]
	if !ok {
		return "undefined state"
	}
	return value
}

func (icinga *icingaClient) Reload() ([]byte, error) {
	cmd := exec.Command("systemctl", "reload", "icinga2.service")
	output, err := cmd.CombinedOutput()
	return output, err
}

// Quote special characters, so Icinga does not barf on the config
// https://icinga.com/docs/icinga2/latest/doc/17-language-reference/#string-literals-escape-sequences
func quote(s string) string {
	s = strings.Replace(s, "\\", "\\\\", -1)
	s = strings.Replace(s, "\"", "\\\"", -1)
	s = strings.Replace(s, "\t", "\\t", -1)
	s = strings.Replace(s, "\t", "\\t", -1)
	s = strings.Replace(s, "\n", "\\n", -1)
	return s
}

// icingaAPIClient is shared by GetHostsDown/GetServicesDown - Icinga's API
// is normally reached over its self-signed cert, hence InsecureSkipVerify.
func icingaAPIClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
}

// postFilter POSTs an Icinga object-query filter (with the GET method
// override Icinga's API requires for a filtered query with a body) and
// returns the raw response body.
func (icinga *icingaClient) postFilter(url, filter string) ([]byte, error) {
	data := map[string]any{"filter": filter}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-HTTP-Method-Override", "GET")
	req.SetBasicAuth(icinga.c.Username, icinga.c.Password)

	resp, err := icingaAPIClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icinga API request to %s: %s: %s", url, resp.Status, string(body))
	}
	return body, nil
}

// GetHostsDown returns all hosts currently down and not acknowledged.
func (icinga *icingaClient) GetHostsDown() (*HostStateResult, error) {
	body, err := icinga.postFilter(icinga.c.URL+"/v1/objects/hosts", "host.state!=0 && host.acknowledgement==0")
	if err != nil {
		return nil, err
	}

	var raw struct {
		Results []icingaHostResult `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling icinga hosts response: %w", err)
	}

	result := &HostStateResult{Status: "ok", Results: make([]HostState, 0, len(raw.Results))}
	for _, r := range raw.Results {
		result.Results = append(result.Results, r.toHostState())
	}
	sort.Slice(result.Results, func(i, j int) bool {
		return result.Results[i].LastHardStateChanged.After(result.Results[j].LastHardStateChanged)
	})
	return result, nil
}

// GetServicesDown returns all services currently in a non-OK state and not
// acknowledged.
func (icinga *icingaClient) GetServicesDown() (*ServiceStateResult, error) {
	body, err := icinga.postFilter(icinga.c.URL+"/v1/objects/services", "service.state > 0 && service.state < 3 && service.acknowledgement==0")
	if err != nil {
		return nil, err
	}

	var raw struct {
		Results []icingaServiceResult `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling icinga services response: %w", err)
	}

	result := &ServiceStateResult{Status: "ok", Results: make([]ServiceState, 0, len(raw.Results))}
	for _, r := range raw.Results {
		result.Results = append(result.Results, r.toServiceState())
	}
	sort.Slice(result.Results, func(i, j int) bool {
		return result.Results[i].LastHardStateChanged.After(result.Results[j].LastHardStateChanged)
	})
	return result, nil
}

func (icinga *icingaClient) GetEvents() (*HostStateResult, error) {
	return nil, nil
}

/*
   def get_events(self):
       """
       Get all events
       returns iterator
       """
       headers = {
           "Accept": "application/json",
       }
       postdata = {
           # "attrs": [ "name", "state", "acknowledgement"]
           # "filter": "service.state!=0 && service.acknowledgement==0"
       }
       url = self.config.api.url + "/v1/events?queue=abtools_icinga&types=CheckResult"
       r = requests.post(
           url,
           headers=headers,
           auth=(self.config.api.username, self.config.api.password),
           data=json.dumps(postdata),
           verify=False,
           stream=True,
       )
       if r.status_code != 200:
           raise IcingaException("Cannot fetch data from Icinga API, status_code %s" % r.status_code)

       return r


*/
