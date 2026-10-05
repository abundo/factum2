package web

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// captureMaxSessions is how many mirrors factum will hold open at once.
// Each one is an SSH session plus a monitor session on a device.
const captureMaxSessions = 4

// wiregasmFiles are what the capture worker loads from /wiregasm/.
var wiregasmFiles = []string{"wiregasm.js", "wiregasm.wasm.gz", "wiregasm.data.gz"}

type captureStartRequest struct {
	DeviceID    uint   `json:"device_id"`
	InterfaceID uint   `json:"interface_id"`
	Filter      string `json:"filter"`
	MaxPackets  int    `json:"max_packets"`
	MaxSeconds  int    `json:"max_seconds"`
	Snaplen     int    `json:"snaplen"`
}

// captureSlots keeps one capture per device and a process-wide cap.
type captureSlots struct {
	mu      sync.Mutex
	n       int
	devices map[uint]struct{}
}

var captureSlotsLive = captureSlots{devices: map[uint]struct{}{}}

func (s *captureSlots) acquire(id uint) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[id]; ok {
		return nil, fmt.Errorf("a capture is already running on this device")
	}
	if s.n >= captureMaxSessions {
		return nil, fmt.Errorf("%d captures are already running", captureMaxSessions)
	}
	s.devices[id] = struct{}{}
	s.n++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.devices, id)
		if s.n > 0 {
			s.n--
		}
	}, nil
}

// ApiCapturePlatforms lists the platforms whose driver implements port
// mirroring. The packet-capture page filters the device picker with this.
func (ctrl *Controller) ApiCapturePlatforms(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"platforms": drivers.CapturePlatforms()})
}

// ApiCapture mirrors one interface and streams the pcap to the browser.
// The driver sets the mirror up and tears it down; this handler only
// checks the request and copies bytes. Closing the request (Stop in the
// GUI) cancels ctx, which stops tcpdump, and the deferred teardown removes
// the monitor session.
func (ctrl *Controller) ApiCapture(c *echo.Context) error {
	var body captureStartRequest
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid request"})
	}
	if body.DeviceID == 0 || body.InterfaceID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "device and interface are required"})
	}
	if err := drivers.ValidateCaptureFilter(strings.TrimSpace(body.Filter)); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{body.DeviceID})
	if err != nil || len(devices) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	device := devices[0]
	if !drivers.CaptureSupported(device.Platform) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "packet capture is not supported for platform " + device.Platform})
	}
	var ifaceName string
	for _, iface := range device.Interfaces {
		if iface.ID == body.InterfaceID {
			ifaceName = iface.Name
			break
		}
	}
	if ifaceName == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "interface not found on this device"})
	}
	req, err := drivers.NormalizeCaptureRequest(drivers.PortMirrorRequest{
		Interface:  ifaceName,
		Filter:     body.Filter,
		Snaplen:    body.Snaplen,
		MaxPackets: body.MaxPackets,
		MaxSeconds: body.MaxSeconds,
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	creds, err := ctrl.deviceSyncCredentials(device.Name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	drv, err := ctrl.newDriverForDevice(&device, creds, settings, sessionUserLabel(c))
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	mirror, ok := drv.(drivers.PortMirror)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "packet capture is not supported for platform " + device.Platform})
	}

	release, err := captureSlotsLive.acquire(device.ID)
	if err != nil {
		return c.JSON(http.StatusTooManyRequests, map[string]any{"error": err.Error()})
	}
	defer release()

	// The time limit covers the whole capture, including setup. Disconnect
	// cancels the request context and that cancels this one too.
	capCtx, capCancel := context.WithTimeout(c.Request().Context(), time.Duration(req.MaxSeconds)*time.Second)
	defer capCancel()

	session, err := mirror.SetupPortMirror(capCtx, req)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	defer func() {
		// The request context is cancelled by the time we get here when the
		// browser hung up. Cleanup needs its own deadline.
		tctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if terr := mirror.TeardownPortMirror(tctx, session); terr != nil {
			slog.Warn("capture teardown", "device", device.Name, "interface", ifaceName, "error", terr)
		}
	}()

	buf := make([]byte, 32<<10)
	n, rerr := io.ReadAtLeast(session.Packets, buf, 24)
	if rerr != nil || !pcapMagic(buf[:n]) {
		msg := strings.TrimSpace(session.Diagnostic())
		if msg == "" {
			msg = "capture did not return a pcap stream"
		}
		return c.JSON(http.StatusBadGateway, map[string]any{"error": msg})
	}

	who := sessionUserLabel(c)
	slog.Info("capture started", "user", who, "device", device.Name, "interface", ifaceName, "filter", req.Filter)
	w := c.Response()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	var sent int64
	for {
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			_ = rc.Flush()
			sent += int64(n)
		}
		if rerr != nil {
			break
		}
		n, rerr = session.Packets.Read(buf)
	}
	slog.Info("capture ended", "user", who, "device", device.Name, "interface", ifaceName, "bytes", sent)
	return nil
}

func pcapMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	le := binary.LittleEndian.Uint32(b[:4])
	be := binary.BigEndian.Uint32(b[:4])
	for _, m := range []uint32{le, be} {
		switch m {
		case 0xa1b2c3d4, 0xa1b23c4d: // classic pcap, nanosecond pcap
			return true
		}
	}
	return false
}

// handleWiregasm serves the Wiregasm build (Wireshark in WebAssembly).
// It is installed beside factum (make wiregasm), not embedded.
func (ctrl *Controller) handleWiregasm(c *echo.Context) error {
	name := c.Param("name")
	if !slices.Contains(wiregasmFiles, name) {
		return c.String(http.StatusNotFound, "not found")
	}
	dir := ctrl.WiregasmDir
	if dir == "" {
		dir = resolveWiregasmDir("")
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return c.String(http.StatusNotFound, "Wiregasm is not installed in "+dir+" (run make wiregasm)")
	}
	if name != "wiregasm.js" {
		c.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(c.Response(), c.Request(), path)
	return nil
}

// resolveWiregasmDir picks the directory Wiregasm was installed into.
// configured wins. Otherwise the first directory that exists, of the
// system path and the repo's web/static/wiregasm (the compose lab
// bind-mounts that tree).
func resolveWiregasmDir(configured string) string {
	if d := strings.TrimSpace(configured); d != "" {
		return d
	}
	candidates := []string{
		"/usr/share/factum2/wiregasm",
		filepath.Join(staticDir, "wiregasm"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return filepath.Join(staticDir, "wiregasm")
}
