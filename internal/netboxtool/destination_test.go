package netboxtool

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDestination(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    map[string]string
		wantErr bool
	}{
		{name: "empty", in: "", want: map[string]string{}},
		{name: "nil", in: nil, want: map[string]string{}},
		{name: "tokens", in: "zabbix:7 librenms:42", want: map[string]string{"librenms": "42", "zabbix": "7"}},
		{name: "tokens with space after colon", in: "librenms: 42 zabbix: 7", want: map[string]string{"librenms": "42", "zabbix": "7"}},
		{name: "json", in: `{"zabbix":"7","librenms":42}`, want: map[string]string{"librenms": "42", "zabbix": "7"}},
		{name: "map", in: map[string]any{"LibreNMS": "42", "zabbix": float64(7)}, want: map[string]string{"librenms": "42", "zabbix": "7"}},
		{name: "garbage", in: "not a destination", wantErr: true},
		{name: "json array", in: `["librenms:42"]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDestination(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrDestinationUnreadable) {
					t.Fatalf("err = %v, want ErrDestinationUnreadable", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatDestination_SortsSystems(t *testing.T) {
	got := FormatDestination(map[string]string{"zabbix": "7", "librenms": "42"})
	assert.Equal(t, "librenms:42 zabbix:7", got)
	assert.Equal(t, "", FormatDestination(nil))
}

func TestMergeDestination_KeepsOtherSystems(t *testing.T) {
	got, err := MergeDestination("zabbix:7 librenms:1", DestinationLibrenms, "42")
	require.NoError(t, err)
	assert.Equal(t, "librenms:42 zabbix:7", got)

	got, err = MergeDestination(`{"zabbix":"7"}`, DestinationLibrenms, "42")
	require.NoError(t, err)
	assert.Equal(t, "librenms:42 zabbix:7", got)

	_, err = MergeDestination("somewhere else", DestinationLibrenms, "42")
	if !errors.Is(err, ErrDestinationUnreadable) {
		t.Fatalf("err = %v, want ErrDestinationUnreadable", err)
	}
}

func TestLibrenmsDeviceID(t *testing.T) {
	assert.Equal(t, uint(42), LibrenmsDeviceID(map[string]any{
		"destination": "librenms:42 zabbix:7",
		"librenms_id": float64(99),
	}))
	assert.Equal(t, uint(15), LibrenmsDeviceID(map[string]any{
		"destination":        "zabbix:7",
		"librenms_device_id": float64(15),
	}))
	assert.Equal(t, uint(8), LibrenmsDeviceID(map[string]any{
		"librenms_id": float64(8),
	}))
	assert.Equal(t, uint(0), LibrenmsDeviceID(map[string]any{
		"destination": "librenms:nope",
		"librenms_id": float64(8),
	}))
	assert.Equal(t, uint(0), LibrenmsDeviceID(nil))
}

func TestParseDevices_LibrenmsIDFromDestination(t *testing.T) {
	nb := &NetboxClient{}
	devices, err := nb.parseDevices([]JSONDevice{
		{
			ID:   1,
			Name: "edge1",
			CF: NetboxCustomFields{All: map[string]any{
				"destination": "librenms:42 zabbix:7",
				"librenms_id": float64(99),
			}},
		},
		{
			ID:   2,
			Name: "edge2",
			CF: NetboxCustomFields{All: map[string]any{
				"librenms_device_id": "15",
			}},
		},
	}, false)
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, uint(42), devices[0].LibrenmsID)
	assert.Equal(t, uint(15), devices[1].LibrenmsID)
}

func TestSetDestinationID_MergesAndPatches(t *testing.T) {
	var patched map[string]any
	nb := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, "/api/dcim/devices/5/", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":5,"custom_fields":{"destination":"zabbix:7","monitor_librenms":true}}`))
		case http.MethodPatch:
			assert.Equal(t, "/api/dcim/devices/5/", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&patched))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("method %s", r.Method)
		}
	})
	require.NoError(t, nb.SetDestinationID(false, 5, DestinationLibrenms, "42"))
	cf, ok := patched["custom_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "librenms:42 zabbix:7", cf["destination"])
	assert.Len(t, cf, 1)
}

func TestSetDestinationID_SkipsUnchangedAndUnreadable(t *testing.T) {
	nb := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/virtualization/virtual-machines/9/", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":9,"custom_fields":{"destination":"librenms:42 zabbix:7"}}`))
	})
	require.NoError(t, nb.SetDestinationID(true, 9, DestinationLibrenms, "42"))

	nb = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("must not write an unreadable destination, method %s", r.Method)
		}
		_, _ = io.WriteString(w, `{"id":3,"custom_fields":{"destination":"see ticket 9"}}`)
	})
	err := nb.SetDestinationID(false, 3, DestinationLibrenms, "42")
	if !errors.Is(err, ErrDestinationUnreadable) {
		t.Fatalf("err = %v, want ErrDestinationUnreadable", err)
	}
}
