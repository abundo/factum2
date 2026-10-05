package drivers

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Packet capture limits. The browser asks; the server clamps. A filter is
// passed to tcpdump on the device, so it stays inside a charset that cannot
// break out of that command.
const (
	captureMaxFilterLen   = 200
	captureMaxSnaplen     = 65535
	captureMaxPackets     = 1_000_000
	captureDefaultPackets = 100_000
	captureMaxSeconds     = 3600
	captureDefaultSeconds = 600
)

// captureInterfaceName is an EOS-style interface name (Ethernet1,
// Ethernet1/1, Port-Channel5). It is checked again before the name is
// placed in a CLI command.
var captureInterfaceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9/._-]{0,63}$`)

// captureFilterText is a pcap-filter expression without shell
// metacharacters. "and" / "or" / "not" are words, so "|" and "&" are not
// needed. Quotes, backticks and semicolons are rejected.
var captureFilterText = regexp.MustCompile(`^[A-Za-z0-9 .:/_()\-]*$`)

// PortMirrorRequest is one capture. Interface is the device's own name
// (Ethernet1), not a Linux name. Snaplen 0 means the whole packet.
type PortMirrorRequest struct {
	Interface  string
	Filter     string
	Snaplen    int
	MaxPackets int
	MaxSeconds int
}

// PortMirrorSession is a running mirror. Packets is a pcap byte stream
// (tcpdump -w -). Teardown closes it and removes the mirror; Close on
// Packets alone does not remove the mirror.
type PortMirrorSession struct {
	Name      string
	Interface string // where the platform is reading, for logs (EOS: mirror0)
	Packets   io.ReadCloser
	diag      func() string
}

// Diagnostic is extra text from the capture command (tcpdump's stderr)
// when the stream fails before a pcap header.
func (s *PortMirrorSession) Diagnostic() string {
	if s == nil || s.diag == nil {
		return ""
	}
	return s.diag()
}

// PortMirror is the platform half of packet capture. Setup configures a
// temporary port mirror over SSH and returns a pcap stream. Teardown
// removes that mirror. It is not part of DriverClient: Arista EOS
// implements it today, and a later platform (Nokia) adds the same two
// methods without a stub on every other driver. The web handler
// type-asserts this interface and does not know the CLI.
type PortMirror interface {
	SetupPortMirror(ctx context.Context, req PortMirrorRequest) (*PortMirrorSession, error)
	TeardownPortMirror(ctx context.Context, session *PortMirrorSession) error
}

// portMirrorPlatforms is filled by registerPortMirror from each driver
// that implements PortMirror. The GUI lists only these platforms.
var portMirrorPlatforms []string

func registerPortMirror(platform string) {
	platform = strings.ToLower(platform)
	for _, have := range portMirrorPlatforms {
		if have == platform {
			panic("drivers: duplicate port-mirror registration for " + platform)
		}
	}
	portMirrorPlatforms = append(portMirrorPlatforms, platform)
}

// CapturePlatforms returns the platforms that implement PortMirror, in
// registration order.
func CapturePlatforms() []string {
	out := make([]string, len(portMirrorPlatforms))
	copy(out, portMirrorPlatforms)
	return out
}

// CaptureSupported reports whether platform (any case) implements PortMirror.
func CaptureSupported(platform string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	for _, have := range portMirrorPlatforms {
		if have == platform {
			return true
		}
	}
	return false
}

// ValidateCaptureInterface rejects names that must not be interpolated
// into a device CLI command.
func ValidateCaptureInterface(name string) error {
	if !captureInterfaceName.MatchString(name) {
		return fmt.Errorf("invalid interface name %q", name)
	}
	return nil
}

// ValidateCaptureFilter rejects a capture filter that is empty-ok but
// otherwise too long or outside the pcap-filter charset.
func ValidateCaptureFilter(filter string) error {
	if filter == "" {
		return nil
	}
	if len(filter) > captureMaxFilterLen {
		return fmt.Errorf("filter is longer than %d characters", captureMaxFilterLen)
	}
	if !captureFilterText.MatchString(filter) {
		return fmt.Errorf("filter has characters that are not allowed")
	}
	return nil
}

// NormalizeCaptureRequest trims and clamps a request. The interface name
// and filter are validated. Zero packets or seconds become the defaults.
func NormalizeCaptureRequest(req PortMirrorRequest) (PortMirrorRequest, error) {
	req.Interface = strings.TrimSpace(req.Interface)
	if err := ValidateCaptureInterface(req.Interface); err != nil {
		return PortMirrorRequest{}, err
	}
	req.Filter = strings.TrimSpace(req.Filter)
	if err := ValidateCaptureFilter(req.Filter); err != nil {
		return PortMirrorRequest{}, err
	}
	if req.MaxPackets <= 0 {
		req.MaxPackets = captureDefaultPackets
	} else if req.MaxPackets > captureMaxPackets {
		req.MaxPackets = captureMaxPackets
	}
	if req.MaxSeconds <= 0 {
		req.MaxSeconds = captureDefaultSeconds
	} else if req.MaxSeconds > captureMaxSeconds {
		req.MaxSeconds = captureMaxSeconds
	}
	if req.Snaplen < 0 {
		req.Snaplen = 0
	} else if req.Snaplen > captureMaxSnaplen {
		req.Snaplen = captureMaxSnaplen
	}
	return req, nil
}
