package becs

// ---------------------------------------------------------------------------
//
// Library to work with BECS using the JSON-RPC API.
// Object names are from BECS: a device is an element-attach (element).
//
// ---------------------------------------------------------------------------

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

const (
	jsonrpcVersion = "2.0"
	treeClassmask  = "element-attach,interface,resource-inet"
	elementClass   = "element-attach"
	elementType    = "ibos"
)

// Object is one BECS object (element-attach, interface, resource-inet,
// or a folder fetched while walking parents).
type Object struct {
	OID         int         `json:"oid"`
	ParentOID   int         `json:"parentoid"`
	Class       string      `json:"class"`
	Name        string      `json:"name"`
	ElementType string      `json:"elementtype"`
	Role        string      `json:"role"`
	Flags       string      `json:"flags"`
	Parameters  []Parameter `json:"parameters"`
	Opaque      []Opaque    `json:"opaque"`
	Resource    *Resource   `json:"resource"`
	ChildrenOID []int       `json:"-"`
}

type Parameter struct {
	Name   string           `json:"name"`
	Values []ParameterValue `json:"values"`
}

type ParameterValue struct {
	Value string `json:"value"`
}

type Opaque struct {
	Name   string        `json:"name"`
	Values []OpaqueValue `json:"values"`
}

type OpaqueValue struct {
	Value string `json:"value"`
}

type Resource struct {
	RCParentOID int    `json:"rcparentoid"`
	Address     string `json:"address"`
	PrefixLen   int    `json:"prefixlen"`
}

// Device is a BECS element-attach mapped to the Netbox device shape.
type Device struct {
	OID              int
	Name             string // FQDN when a default domain is configured
	ShortName        string
	Manufacturer     string
	Model            string
	Role             string
	Platform         string
	Enabled          bool
	AlarmTimeperiod  string
	AlarmDestination string
	ConnectionMethod string
	Parents          []string
	Interfaces       map[string]*Interface
	InterfacesOID    map[int]*Interface
}

type Interface struct {
	OID     int
	Name    string
	Role    string
	Enabled bool
	Prefix4 []Prefix
}

type Prefix struct {
	Address string // CIDR
	OID     int
}

// Client talks to the BECS JSON-RPC API. Objects from the last LoadTree
// (plus any ancestors fetched while walking parents) live in memory —
// there is no on-disk cache; the API is fast enough without one.
type Client struct {
	URL       string
	Username  string
	Password  string
	SessionID string

	http    *http.Client
	objects map[int]*Object
	nextID  int
}

func NewClient(url, username, password string) *Client {
	return &Client{
		URL:      url,
		Username: username,
		Password: password,
		http:     &http.Client{Timeout: 120 * time.Second},
		objects:  make(map[int]*Object),
		nextID:   1,
	}
}

func NewClientFromSettings(s *models.Settings) (*Client, error) {
	if s.BecsEapiURL == "" {
		return nil, fmt.Errorf("becs: eapi url is not configured")
	}
	if s.BecsEapiUser == "" {
		return nil, fmt.Errorf("becs: eapi user is not configured")
	}
	return NewClient(s.BecsEapiURL, s.BecsEapiUser, s.BecsEapiPass), nil
}

type jsonrpcRequest struct {
	Method  string `json:"method"`
	Params  any    `json:"params"`
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonrpcError   `json:"error"`
}

type objectsResult struct {
	Objects []Object `json:"objects"`
	Total   int      `json:"total"`
}

type loginResult struct {
	SessionID string `json:"sessionid"`
}

func (c *Client) Call(method string, params map[string]any) (json.RawMessage, error) {
	if method != "sessionLogin" && c.SessionID == "" {
		if err := c.Login(); err != nil {
			return nil, err
		}
	}
	if c.SessionID != "" {
		if params == nil {
			params = map[string]any{}
		}
		params["_header"] = map[string]string{"sessionid": c.SessionID}
	}

	c.nextID++
	payload, err := json.Marshal(jsonrpcRequest{
		Method:  method,
		Params:  params,
		JSONRPC: jsonrpcVersion,
		ID:      c.nextID,
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(c.URL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("becs %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("becs %s: read: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("becs %s: %s: %s", method, resp.Status, body)
	}

	var rpc jsonrpcResponse
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil, fmt.Errorf("becs %s: decode: %w", method, err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("becs %s: %s (code %d)", method, rpc.Error.Message, rpc.Error.Code)
	}
	return rpc.Result, nil
}

func (c *Client) Login() error {
	if c.SessionID != "" {
		return nil
	}
	result, err := c.Call("sessionLogin", map[string]any{
		"username": c.Username,
		"password": c.Password,
	})
	if err != nil {
		return err
	}
	var login loginResult
	if err := json.Unmarshal(result, &login); err != nil {
		return fmt.Errorf("becs login: decode: %w", err)
	}
	if login.SessionID == "" {
		return fmt.Errorf("becs login: empty sessionid")
	}
	c.SessionID = login.SessionID
	return nil
}

func (c *Client) Logout() {
	c.SessionID = ""
}

// Index replaces the in-memory object map and rebuilds parent/child links.
// Used by LoadTree and by tests that feed a fixture.
func (c *Client) Index(objs []Object) {
	c.objects = make(map[int]*Object, len(objs))
	for i := range objs {
		obj := objs[i]
		obj.ChildrenOID = nil
		c.objects[obj.OID] = &obj
	}
	for _, obj := range c.objects {
		if parent, ok := c.objects[obj.ParentOID]; ok {
			parent.ChildrenOID = append(parent.ChildrenOID, obj.OID)
		}
	}
}

func (c *Client) ObjectTreeFind(oid int, classmask string, walkdown int) ([]Object, error) {
	result, err := c.Call("objectTreeFind", map[string]any{
		"oid":       oid,
		"classmask": classmask,
		"walkdown":  walkdown,
	})
	if err != nil {
		return nil, err
	}
	var parsed objectsResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, fmt.Errorf("becs objectTreeFind: decode: %w", err)
	}
	return parsed.Objects, nil
}

// GetObject returns one object, from the in-memory map or via objectFind.
func (c *Client) GetObject(oid int) (*Object, error) {
	if oid == 0 {
		return nil, nil
	}
	if obj, ok := c.objects[oid]; ok {
		return obj, nil
	}
	result, err := c.Call("objectFind", map[string]any{
		"queries": []map[string]int{{"oid": oid}},
	})
	if err != nil {
		return nil, err
	}
	var parsed objectsResult
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, fmt.Errorf("becs objectFind: decode: %w", err)
	}
	if len(parsed.Objects) == 0 {
		return nil, nil
	}
	obj := parsed.Objects[0]
	c.objects[obj.OID] = &obj
	return &obj, nil
}

// LoadTree fetches the object tree starting at rootOID (walkdown 0 =
// unlimited, matching the Python client) and indexes it in memory.
// If the client already has objects (e.g. tests that called Index), it
// is a no-op so a later Devices/GetElement call does not hit the API.
func (c *Client) LoadTree(rootOID int) error {
	if len(c.objects) > 0 {
		return nil
	}
	if rootOID == 0 {
		rootOID = 1
	}
	objs, err := c.ObjectTreeFind(rootOID, treeClassmask, 0)
	if err != nil {
		return err
	}
	c.Index(objs)
	return nil
}

func firstOpaque(obj *Object, name string) string {
	for _, op := range obj.Opaque {
		if op.Name != name || len(op.Values) == 0 {
			continue
		}
		return op.Values[0].Value
	}
	return ""
}

func paramValue(obj *Object, name string) string {
	for _, p := range obj.Parameters {
		if p.Name == name && len(p.Values) > 0 {
			return p.Values[0].Value
		}
	}
	return ""
}

func hasFlag(flags, name string) bool {
	if flags == "" {
		return false
	}
	for _, f := range strings.Split(flags, ",") {
		if strings.TrimSpace(f) == name {
			return true
		}
	}
	return false
}

// SearchOpaque walks from oid toward the root and returns the first
// opaque value with the given name. Does not handle arrays.
func (c *Client) SearchOpaque(oid int, name string) (string, error) {
	for oid != 0 && oid != 1 {
		obj, err := c.GetObject(oid)
		if err != nil {
			return "", err
		}
		if obj == nil {
			return "", nil
		}
		if v := firstOpaque(obj, name); v != "" {
			return v, nil
		}
		oid = obj.ParentOID
	}
	return "", nil
}

// SearchParent walks from oid toward the root. A "parents" opaque wins;
// otherwise the first ancestor element-attach (skipping the starting
// object) is used.
func (c *Client) SearchParent(oid int) (string, error) {
	checkElement := false
	for oid != 0 && oid != 1 {
		obj, err := c.GetObject(oid)
		if err != nil {
			return "", err
		}
		if obj == nil {
			return "", nil
		}
		if v := firstOpaque(obj, "parents"); v != "" {
			return v, nil
		}
		if checkElement && obj.Class == elementClass {
			return obj.Name, nil
		}
		checkElement = true
		oid = obj.ParentOID
	}
	return "", nil
}

func (c *Client) prefixlen(obj *Object) (int, error) {
	if obj.Resource == nil {
		return 0, nil
	}
	prefixlen := obj.Resource.PrefixLen
	if hasFlag(obj.Flags, "useparentmask") && obj.Resource.RCParentOID != 0 {
		parent, err := c.GetObject(obj.Resource.RCParentOID)
		if err != nil {
			return 0, err
		}
		if parent != nil && parent.Resource != nil && parent.Resource.PrefixLen > 0 {
			prefixlen = parent.Resource.PrefixLen
		}
	}
	return prefixlen, nil
}

// Interfaces returns the interfaces of an element-attach, keyed by name.
func (c *Client) Interfaces(elementOID int) (map[string]*Interface, error) {
	el, err := c.GetObject(elementOID)
	if err != nil {
		return nil, err
	}
	if el == nil {
		return nil, fmt.Errorf("becs: element oid %d not found", elementOID)
	}

	out := make(map[string]*Interface)
	for _, childOID := range el.ChildrenOID {
		child, err := c.GetObject(childOID)
		if err != nil {
			return nil, err
		}
		if child == nil || child.Class != "interface" {
			continue
		}
		iface := &Interface{
			OID:     child.OID,
			Name:    child.Name,
			Role:    child.Role,
			Enabled: !hasFlag(child.Flags, "disable"),
		}
		for _, rcOID := range child.ChildrenOID {
			rc, err := c.GetObject(rcOID)
			if err != nil {
				return nil, err
			}
			if rc == nil || rc.Class != "resource-inet" || rc.Resource == nil {
				continue
			}
			if strings.Contains(rc.Resource.Address, ":") {
				continue
			}
			prefixlen, err := c.prefixlen(rc)
			if err != nil {
				return nil, err
			}
			iface.Prefix4 = append(iface.Prefix4, Prefix{
				Address: fmt.Sprintf("%s/%d", rc.Resource.Address, prefixlen),
				OID:     rc.OID,
			})
		}
		out[iface.Name] = iface
	}
	return out, nil
}

func splitParents(s, domain string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if domain != "" {
			p = util.FormatName(domain, p)
		}
		out = append(out, p)
	}
	return out
}

func deviceNames(name, domain string) (short, fqdn string) {
	name = strings.ToLower(strings.TrimSpace(name))
	short = util.ShortName(name, domain)
	if domain != "" {
		return short, util.FormatName(domain, short)
	}
	return short, short
}

func (c *Client) deviceFromElement(el *Object, domain string) (*Device, error) {
	short, fqdn := deviceNames(el.Name, domain)
	model := paramValue(el, "model")
	parents, err := c.SearchParent(el.OID)
	if err != nil {
		return nil, err
	}
	alarmDest, err := c.SearchOpaque(el.OID, "alarm_destination")
	if err != nil {
		return nil, err
	}
	alarmPeriod, err := c.SearchOpaque(el.OID, "alarm_timeperiod")
	if err != nil {
		return nil, err
	}
	ifaces, err := c.Interfaces(el.OID)
	if err != nil {
		return nil, err
	}

	conn := "ssh"
	if strings.HasPrefix(model, "ASR5") {
		conn = "telnet"
	}

	d := &Device{
		OID:              el.OID,
		Name:             fqdn,
		ShortName:        short,
		Manufacturer:     "Waystream",
		Model:            model,
		Role:             el.Role,
		Platform:         el.ElementType,
		Enabled:          !hasFlag(el.Flags, "disable"),
		AlarmTimeperiod:  alarmPeriod,
		AlarmDestination: alarmDest,
		ConnectionMethod: conn,
		Parents:          splitParents(parents, domain),
		Interfaces:       ifaces,
		InterfacesOID:    make(map[int]*Interface, len(ifaces)),
	}
	for _, iface := range ifaces {
		d.InterfacesOID[iface.OID] = iface
	}
	return d, nil
}

// Devices returns every ibos element-attach as a Device. LoadTree must
// have been called first.
func (c *Client) Devices(domain string) ([]*Device, error) {
	var out []*Device
	for _, obj := range c.objects {
		if obj.Class != elementClass || obj.ElementType != elementType {
			continue
		}
		d, err := c.deviceFromElement(obj, domain)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// GetElement loads the tree (if needed) and returns the named ibos
// element. Name is matched case-insensitively against the short name.
func (c *Client) GetElement(name string, rootOID int) (*Device, error) {
	if len(c.objects) == 0 {
		if err := c.LoadTree(rootOID); err != nil {
			return nil, err
		}
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, obj := range c.objects {
		if obj.Class != elementClass || obj.ElementType != elementType {
			continue
		}
		if strings.ToLower(obj.Name) != want {
			continue
		}
		return c.deviceFromElement(obj, "")
	}
	return nil, fmt.Errorf("becs: element %q not found", name)
}
