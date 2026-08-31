package librenms

//
// Library to manage Librenms
//

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
)

type LibrenmsClient struct {
	P *util.ConfigLibrenms
	// DBConfig holds LibreNMS's own MySQL credentials, read directly from
	// LibreNMS's .env file on disk (see NewFactumLibrenmsClient) - unlike P,
	// which is fetched over REST from the primary. Left nil for callers that
	// never touch the port-level methods (PortsGet/PortsUpdateIgnore).
	DBConfig *util.ConfigDB
	DB       *sql.DB
}

// Create a librenmsClient
func NewLibrenmsClient(param *util.ConfigLibrenms) *LibrenmsClient {
	client := new(LibrenmsClient)
	client.P = param

	return client
}

// ---------------------------------------------------------------------------

type StatusJSON struct {
	Status  string      `json:"status"`
	Count   StringOrNum `json:"count"`
	Message string      `json:"message"`
}

// StringOrNum unmarshals a JSON number or a JSON string into a string -
// librenms's delete_device endpoint sends "count" as a number while other
// endpoints (e.g. add_device) send it as a string, the same status-JSON
// inconsistency pattern Float32String/IntString work around elsewhere in
// this file.
type StringOrNum string

func (s *StringOrNum) UnmarshalJSON(data []byte) error {
	*s = StringOrNum(strings.Trim(string(data), `"`))
	return nil
}

// Float32String unmarshals a JSON number or a JSON string holding a number
// into a float32 - librenms's API is inconsistent about which one it sends
// for lat/lng (its own docs show lat/lng posted as strings), so a plain
// float32 field fails to unmarshal whenever a device/location happens to
// come back with a quoted value.
type Float32String float32

func (f *Float32String) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return err
	}
	*f = Float32String(v)
	return nil
}

// IntString unmarshals a JSON number, a JSON bool, or a JSON string holding
// a number into an int - librenms's API sends the same field (e.g.
// ignore/disabled) as a number on the device-list endpoint but as a real
// JSON boolean on the single-device endpoint, on top of the number-as-
// string inconsistency Float32String works around.
type IntString int

func (i *IntString) UnmarshalJSON(data []byte) error {
	switch s := string(data); s {
	case "true":
		*i = 1
	case "false", "null", `""`:
		*i = 0
	default:
		s = strings.Trim(s, `"`)
		v, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*i = IntString(v)
	}
	return nil
}

// deviceLocation accepts either a plain location-name string (as sent by
// the device-list endpoint) or the nested location object the single-
// device endpoint embeds instead, keeping just its "location" name.
type deviceLocation string

func (l *deviceLocation) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*l = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*l = deviceLocation(s)
		return nil
	}
	var obj struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*l = deviceLocation(obj.Location)
	return nil
}

// We follow librenms names on variables
type LibrenmsDevice struct {
	DeviceID                 int            `json:"device_id"`
	Hostname                 string         `json:"hostname"`
	Location                 string         `json:"location"`
	Display                  string         `json:"display"`
	IP                       string         `json:"ip"`
	Community                string         `json:"community"`
	SNMPver                  string         `json:"snmpver"`
	LocationID               int            `json:"location_id"`
	OS                       string         `json:"hpe-ilo"`
	Ignore                   int            `json:"ignore"`
	Disabled                 int            `json:"disabled"`
	DependencyParentID       int            `json:"dependency_parent_id"`
	DependencyParentHostname string         `json:"dependency_parent_hostname"`
	Lat                      *Float32String `json:"lat"`
	Lng                      *Float32String `json:"lng"`
}

// librenms's API is inconsistent about whether device_id/location_id/ignore/
// disabled/dependency_parent_id come back as JSON numbers or JSON strings
// (same issue as lat/lng) - unmarshal through IntString-typed fields and
// copy the results into the plain ints the rest of the codebase expects.
func (d *LibrenmsDevice) UnmarshalJSON(data []byte) error {
	type alias LibrenmsDevice
	aux := &struct {
		DeviceID           IntString      `json:"device_id"`
		LocationID         IntString      `json:"location_id"`
		Ignore             IntString      `json:"ignore"`
		Disabled           IntString      `json:"disabled"`
		DependencyParentID IntString      `json:"dependency_parent_id"`
		Location           deviceLocation `json:"location"`
		*alias
	}{
		alias: (*alias)(d),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	d.DeviceID = int(aux.DeviceID)
	d.LocationID = int(aux.LocationID)
	d.Ignore = int(aux.Ignore)
	d.Disabled = int(aux.Disabled)
	d.DependencyParentID = int(aux.DependencyParentID)
	d.Location = string(aux.Location)
	return nil
}

type LibrenmsPort struct {
	PortID   int    `json:"port_id"`
	DeviceID int    `json:"device_id"`
	Name     string `json:"Name"`
	IfName   string `json:"ifName"`
	IfDescr  string `json:"ifDescr"`
	IfAlias  string `json:"ifAlias"`
	Ignore   int    `json:"ignore"`
}

type LibrenmsLocation struct {
	ID               int            `json:"id"`
	Location         string         `json:"location"`
	Lat              *Float32String `json:"lat"`
	Lng              *Float32String `json:"lng"`
	FixedCoordinates int            `json:"fixed_coordinates"`
}

// See LibrenmsDevice.UnmarshalJSON - fixed_coordinates has been observed as
// a JSON bool (in the nested location object embedded by the single-device
// endpoint), so route it through IntString like the device's bool-ish
// fields.
func (l *LibrenmsLocation) UnmarshalJSON(data []byte) error {
	type alias LibrenmsLocation
	aux := &struct {
		ID               IntString `json:"id"`
		FixedCoordinates IntString `json:"fixed_coordinates"`
		*alias
	}{
		alias: (*alias)(l),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	l.ID = int(aux.ID)
	l.FixedCoordinates = int(aux.FixedCoordinates)
	return nil
}

// Helper, to setup headers etc for calling Librenms API
// If data is not nil, it is JSON encoded before posting
func (librenms *LibrenmsClient) callAPI(method string, endpoint string, data any) ([]byte, error) {
	var err error
	url := librenms.P.URL + endpoint

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}

	var jsonData []byte
	if data != nil {
		jsonData, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Token", librenms.P.Key)

	response, err := client.Do(req)
	if err != nil {
		log.Fatal("Server error:", err)
		return nil, err
	}
	defer response.Body.Close()
	respData, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal("Server error:", err)
		return nil, err
	}
	return respData, nil
}

func (librenms *LibrenmsClient) connectDB() error {
	var err error
	if librenms.DB != nil {
		return nil
	}
	if librenms.DBConfig == nil {
		return errors.New("librenms mysql credentials not configured")
	}
	c := librenms.DBConfig
	cfg := mysql.Config{
		User:                 c.User,
		Passwd:               c.Pass,
		Net:                  "tcp",
		Addr:                 c.Host + ":" + c.Port,
		DBName:               c.Database,
		AllowNativePasswords: true, // Recommended for modern MySQL versions.
		ParseTime:            true,
		//Collation:            "utf8mb4_unicode_bin",
		//Loc:                  time.Local, // Use the system's local time zone.
		Timeout:      5 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	librenms.DB, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	librenms.DB.SetMaxOpenConns(10)
	librenms.DB.SetMaxIdleConns(2)
	librenms.DB.SetConnMaxLifetime(time.Minute * 10)
	librenms.DB.SetConnMaxIdleTime(time.Minute * 3)

	err = librenms.DB.Ping()
	return err
}

// ---------------------------------------------------------------------------
//  Devices
// ---------------------------------------------------------------------------

// API JSON response
type librenmsDevices struct {
	Status  string            `json:"status"`
	Devices []*LibrenmsDevice `json:"devices"`
}

type deviceUpdateFields struct {
	Field []string `json:"field"`
	Data  []string `json:"data"`
}

// parseApiResponse unmarshals a librenms API response and turns a
// {"status":"error",...} body into a real Go error - librenms reports
// operation failures (e.g. update_device_field/rename_device rejecting the
// change) with HTTP 200/500 and a JSON body, not a transport-level error, so
// without this check callers previously saw a nil error on failure and had
// no way to know the operation didn't actually happen.
func parseApiResponse(data []byte, err error) (*string, error) {
	var status StatusJSON
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &status)
	if err != nil {
		return nil, err
	}
	if status.Status != "ok" {
		msg := status.Message
		if msg == "" {
			msg = string(data)
		}
		return &status.Status, fmt.Errorf("librenms: %s", msg)
	}
	return &status.Status, nil
}

// Get all devices
// https://docs.librenms.org/API/Devices/#list_devices
func (librenms *LibrenmsClient) DeviceHelper(name string) ([]*LibrenmsDevice, error) {
	var endpoint string
	if name != "" {
		endpoint = fmt.Sprintf("/devices/%s", name)
	} else {
		endpoint = "/devices"
	}
	respData, err := librenms.callAPI("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var tmp librenmsDevices
	err = json.Unmarshal(respData, &tmp)
	if err != nil {
		return nil, err
	}
	return tmp.Devices, nil
}

func (librenms *LibrenmsClient) DevicesGet() ([]*LibrenmsDevice, error) {
	return librenms.DeviceHelper("")
}

// Get one device, name can be hostname or device_id
// https://docs.librenms.org/API/Devices/#get_device
// curl -H 'X-Auth-Token: YOURAPITOKENHERE' https://foo.example/api/v0/devices/localhost
func (librenms *LibrenmsClient) DeviceGetByName(name string) (*LibrenmsDevice, error) {
	response, err := librenms.DeviceHelper(name)
	if err != nil {
		return nil, err
	}
	return response[0], nil
}

// Get one device, key is ID
func (librenms *LibrenmsClient) DeviceGet(deviceID int) (*LibrenmsDevice, error) {
	tmp := fmt.Sprintf("%d", deviceID)
	response, err := librenms.DeviceHelper(tmp)
	if err != nil {
		return nil, err
	}
	return response[0], nil
}

// Create device, returns the librenms device_id of the created device.
// https://docs.librenms.org/API/Devices/#add_device
func (librenms *LibrenmsClient) DeviceCreate(name string, display string, force_add bool, version string, community string) (int, error) {
	data := make(map[string]string)

	data["hostname"] = name
	if display != "" {
		data["display"] = display
	}
	if force_add {
		data["force_add"] = "1"
	} else {
		data["force_add"] = "0"
	}

	if version != "" {
		data["version"] = version
	}
	if community != "" {
		data["community"] = community
	}
	respData, err := librenms.callAPI("POST", "/devices", data)
	if err != nil {
		return 0, err
	}

	var resp struct {
		StatusJSON
		Devices []struct {
			DeviceID int `json:"device_id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, err
	}
	if resp.Status != "ok" {
		return 0, fmt.Errorf("librenms: cannot create device %s: %s", name, resp.Message)
	}
	if len(resp.Devices) == 0 {
		return 0, fmt.Errorf("librenms: create device %s: response missing device_id", name)
	}
	return resp.Devices[0].DeviceID, nil
}

// Update device fields
// https://docs.librenms.org/API/Devices/#update_device_field
func (librenms *LibrenmsClient) DeviceUpdate(deviceID int, updates map[string]string) (*string, error) {
	postData := deviceUpdateFields{}
	for field, data := range updates {
		postData.Field = append(postData.Field, field)
		postData.Data = append(postData.Data, data)
	}
	endpoint := fmt.Sprintf("/devices/%d", deviceID)
	response, err := librenms.callAPI("PATCH", endpoint, postData)
	return parseApiResponse(response, err)
}

// Rename device
// https://docs.librenms.org/API/Devices/#rename_device
func (librenms *LibrenmsClient) DeviceRename(deviceID int, new_name string) (*string, error) {
	endpoint := fmt.Sprintf("/devices/%d/rename/%s", deviceID, new_name)
	response, err := librenms.callAPI("PATCH", endpoint, nil)
	return parseApiResponse(response, err)
}

// Delete device
// https://docs.librenms.org/API/Devices/#del_device
func (librenms *LibrenmsClient) DeviceDelete(deviceID int) (*string, error) {
	endpoint := fmt.Sprintf("/devices/%d", deviceID)
	response, err := librenms.callAPI("DELETE", endpoint, nil)
	return parseApiResponse(response, err)
}

// hostnameIsIP reports whether hostname is a bare IPv4 or IPv6 address
// (the form LibreNMS sync uses as the device key / SNMP polling target).
func hostnameIsIP(hostname string) bool {
	return net.ParseIP(hostname) != nil
}

// requireIPHostnames fails if any device's hostname is not an IPv4 or IPv6
// address. Sync requires this so matching and polling stay keyed on IP;
// run NormalizeHostnames first to convert leftover name-based hostnames.
func requireIPHostnames(devices []*LibrenmsDevice, reporter jobevent.Reporter) error {
	var bad []string
	for _, device := range devices {
		if device == nil || hostnameIsIP(device.Hostname) {
			continue
		}
		reporter.Emit(jobevent.Error, "%s (id=%d): hostname is not an IPv4 or IPv6 address", device.Hostname, device.DeviceID)
		bad = append(bad, fmt.Sprintf("%s (id=%d)", device.Hostname, device.DeviceID))
	}
	if len(bad) == 0 {
		return nil
	}
	err := fmt.Errorf("sync aborted: %d LibreNMS device(s) do not have an IPv4 or IPv6 address as hostname (run factum2-librenms normalize-hostnames first): %s", len(bad), strings.Join(bad, ", "))
	reporter.EmitErr(err)
	return err
}

// NormalizeHostnames finds every device whose hostname is not an IP address
// (e.g. a legacy/manually-added device keyed on a name), preserves that name
// as the device's display name, then renames the device's hostname to its
// last-known IP address (LibrenmsDevice.IP) via the rename_device API.
// https://docs.librenms.org/API/Devices/#rename_device
// A device with no known IP yet is skipped rather than failed, since there is
// nothing to rename it to. Per-device errors are logged and skipped so one
// bad device doesn't block the rest of the run.
func (librenms *LibrenmsClient) NormalizeHostnames(reporter jobevent.Reporter) (int, error) {
	devices, err := librenms.DevicesGet()
	if err != nil {
		return 0, err
	}

	renamed := 0
	for _, device := range devices {
		if hostnameIsIP(device.Hostname) {
			continue // already keyed on an IP address
		}
		if device.IP == "" {
			reporter.Emit(jobevent.Warning, "%s: no known IP address, skipping", device.Hostname)
			continue
		}

		// librenms auto-regenerates Display from Hostname whenever no
		// display_template override is set (its own default template is
		// "{{ $hostname }}"), so Display == Hostname here even though no
		// override exists yet - comparing them can never detect "needs a
		// fixed display_template". Set it unconditionally instead: this
		// pins the display to today's (name-based) hostname, so it survives
		// the upcoming rename instead of being regenerated to the new
		// IP-based hostname.
		reporter.Emit(jobevent.Info, "%s: setting display_template to %q", device.Hostname, device.Hostname)
		if _, err := librenms.DeviceUpdate(device.DeviceID, map[string]string{"display_template": device.Hostname}); err != nil {
			reporter.Emit(jobevent.Warning, "%s: cannot set display_template: %s", device.Hostname, err)
			continue
		}

		reporter.Emit(jobevent.Info, "%s: renaming hostname to %s", device.Hostname, device.IP)
		if _, err := librenms.DeviceRename(device.DeviceID, device.IP); err != nil {
			reporter.Emit(jobevent.Warning, "%s: cannot rename to %s: %s", device.Hostname, device.IP, err)
			continue
		}
		renamed++
	}
	return renamed, nil
}

// Set location on a device
// If location does not exist, it is created
func (librenms *LibrenmsClient) DeviceSetLocation(deviceID int, locationName string, lat *float64, lng *float64) error {
	device, err := librenms.DeviceGet(deviceID)
	if err != nil {
		return err
	}
	if device.Location != locationName {
		// Incorrect location
		data := make(map[string]string)
		data["location"] = locationName
		_, err := librenms.DeviceUpdate(deviceID, data)
		if err != nil {
			return err
		}
	}

	// get device, location_id can be new/updated
	device, err = librenms.DeviceGet(deviceID)
	if err != nil {
		return err
	}

	data := make(map[string]any)
	if lat != nil {
		data["lat"] = lat
	}
	if lng != nil {
		data["lng"] = lng
	}
	_, err = librenms.LocationUpdate(device.LocationID, lat, lng)
	return err
}

// ---------------------------------------------------------------------------
//  Ports (Interfaces)
// ---------------------------------------------------------------------------

// API JSON response
type LibrenmsPorts struct {
	Status string          `json:"status"`
	Ports  []*LibrenmsPort `json:"ports"`
}

// Get all ports for a device.
// https://docs.librenms.org/API/Devices/#get_device_ports
// Uses direct database access, API is SLOW!
func (librenms *LibrenmsClient) PortsGet(deviceID int) ([]*LibrenmsPort, error) {
	var err error
	err = librenms.connectDB()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	rows, err := librenms.DB.QueryContext(ctx, "SELECT port_id, ifName, ifAlias, ifDescr, `ignore` FROM ports WHERE device_id = ?", deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ports []*LibrenmsPort
	for rows.Next() {
		var port LibrenmsPort
		err = rows.Scan(&port.PortID, &port.IfName, &port.IfAlias, &port.IfDescr, &port.Ignore)
		if err != nil {
			return nil, err
		}
		ports = append(ports, &port)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}
	return ports, nil

	// API implementation
	// endpoint := fmt.Sprintf("/ports?columns=port_id,ifName,ifAlias,ifDescr,ignore")
	// respData, err := librenms.callAPI("GET", endpoint, nil)
	// if err != nil {
	// 	return nil, err
	// }
	// var tmp LibrenmsPorts
	// err = json.Unmarshal(respData, &tmp)
	// if err != nil {
	// 	return nil, err
	// }
	// return tmp.Ports, nil
}

// Update a port details
// There is no API, database is updated directly
func (librenms *LibrenmsClient) PortsUpdateIgnore(portID int, ignore int) error {
	var err error
	// data.port_id = port_id
	// self.db.update("ports", d=data, primary_key="port_id")
	err = librenms.connectDB()
	if err != nil {
		return err
	}
	ctx := context.Background()

	result, err := librenms.DB.ExecContext(ctx, "UPDATE ports SET `ignore` = ? WHERE port_id = ?", ignore, portID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected < 1 {
		return errors.New("SQL update affected more than one row")
	}
	return nil
}

// ---------------------------------------------------------------------------
//  Locations
// ---------------------------------------------------------------------------

// API JSON response
type LibrenmsLocationsResponse struct {
	StatusJSON
	Locations []*LibrenmsLocation `json:"locations"`
}

// Get all locations
// https://docs.librenms.org/API/Locations/
// curl -H 'X-Auth-Token: YOURAPITOKENHERE' https://foo.example/api/v0/resources/locations
func (librenms *LibrenmsClient) LocationsGet() ([]*LibrenmsLocation, error) {
	var err error
	endpoint := "/resources/locations"
	respData, err := librenms.callAPI("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var tmp LibrenmsLocationsResponse
	err = json.Unmarshal(respData, &tmp)
	if err != nil {
		return nil, err
	}
	return tmp.Locations, nil
}

// API JSON response
type LibrenmsLocationResponse struct {
	StatusJSON
	Locations []*LibrenmsLocation `json:"get_location"`
}

// Get one location, using the name or id
// https://docs.librenms.org/API/Locations/#get_location
func (librenms *LibrenmsClient) LocationGet(name string) (*LibrenmsLocation, error) {
	var err error
	endpoint := fmt.Sprintf("/location/%s", name)
	respData, err := librenms.callAPI("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var tmp LibrenmsLocationResponse
	err = json.Unmarshal(respData, &tmp)
	if err != nil {
		return nil, err
	}
	return tmp.Locations[0], nil
}

// Create new location
// https://docs.librenms.org/API/Locations/#add_location
// curl -X POST -d '{"location":"Google", "lat":"37.4220041","lng":"-122.0862462"}' -H 'X-Auth-Token:YOUR-API-TOKEN' https://foo.example/api/v0/locations
func (librenms *LibrenmsClient) LocationCreate(locationName string, lat *float64, lng *float64) (*string, error) {
	post := map[string]any{
		"location": locationName,
	}
	if lat != nil {
		post["lat"] = *lat
	}
	if lng != nil {
		post["lng"] = *lng
	}
	r, err := librenms.callAPI("POST", "/locations", post)
	return parseApiResponse(r, err)
}

// Update location, location: name or id of the location to edit
// https://docs.librenms.org/API/Locations/#edit_location
// curl -X PATCH -d '{"lng":"100.0862462"}' -H 'X-Auth-Token:YOUR-API-TOKEN' https://foo.example/api/v0/locations/Google
func (librenms *LibrenmsClient) LocationUpdate(locationID int, lat *float64, lng *float64) (*string, error) {
	post := make(map[string]any)
	if lat != nil {
		post["lat"] = *lat
	}
	if lng != nil {
		post["lng"] = *lng
	}
	r, err := librenms.callAPI("PATCH", "/locations", post)
	return parseApiResponse(r, err)
}

// ---------------------------------------------------------------------------
//  Parents
// ---------------------------------------------------------------------------

// Add a parent to a device
// https://docs.librenms.org/API/Devices/#add_parents_to_host
// curl -X POST -d '{"parent_ids":"15,16,17"}' -H 'X-Auth-Token: YOURAPITOKENHERE' https://foo.example/api/v0/devices/1/parents
func (librenms *LibrenmsClient) DeviceParentCreate(deviceID int, parent string) error {
	return nil
}

// Delete a parent from a device, parent is device_id or hostname
// https://docs.librenms.org/API/Devices/#delete_parents_from_host
// curl -X DELETE -d '{"parent_ids":"15,16,17"}' -H 'X-Auth-Token: YOURAPITOKENHERE' https://foo.example/api/v0/devices/1/parents
func (librenms *LibrenmsClient) DeviceParentDelete(deviceID int, parent string) (*string, error) {
	data := make(map[string]string)
	data["parent_ids"] = parent

	endpoint := fmt.Sprintf("/devices/%d/parents", deviceID)
	response, err := librenms.callAPI("DELETE", endpoint, data)
	return parseApiResponse(response, err)
}

// Get all parents for a device
// https://docs.librenms.org/API/Devices/#list_parents_of_host
// curl -H 'X-Auth-Token: YOURAPITOKENHERE' 'http://foo.example/api/v0/devices?type=device_id&query=34'
func (librenms *LibrenmsClient) DeviceParentList(deviceID int) ([]*string, error) {
	endpoint := fmt.Sprintf("/devices?type=device_id&query=%d", deviceID)
	_ = endpoint
	return nil, nil
}

// Set parent on a device
// This is not an Librenms API call. Fetch all parents and adjust accordingly
func (librenms *LibrenmsClient) SetDeviceParent(deviceID int, new_parents []*string) error {
	var add []*string
	var remove []*string

	parents, err := librenms.DeviceParentList(deviceID)
	if err != nil {
		return err
	}

	// structure for fast lookup
	old_pmap := make(map[string]bool)
	for _, v := range parents {
		old_pmap[*v] = true
	}

	// structure for fast lookup
	new_pmap := make(map[string]bool)
	for _, v := range new_parents {
		new_pmap[*v] = true
	}

	// find what to add
	for _, k := range new_parents {
		_, ok := old_pmap[*k]
		if !ok {
			add = append(add, k)
		}
	}

	// find what to remove
	for _, k := range parents {
		_, ok := new_pmap[*k]
		if !ok {
			remove = append(remove, k)
		}
	}

	for _, p := range remove {
		librenms.DeviceParentDelete(deviceID, *p)
	}
	for _, p := range add {
		librenms.DeviceParentCreate(deviceID, *p)
	}

	//	data = AttrDict(parent_ids=",".join(parent_ids))
	//	try:
	//	    r = self.call_api(method="POST", endpoint=f"/devices/{device_id}/parents", data=data)
	//	    # todo update cache
	//	    return r
	//	except requests.exceptions.HTTPError as err:
	//	    raise LibrenmsException(f"Error setting parents on device_id {device_id}: {err}")
	return nil
}
