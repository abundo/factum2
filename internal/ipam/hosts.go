package ipam

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const hostPageSize = 256

// HostCell is one address in a prefix occupancy page.
type HostCell struct {
	Address   string `json:"address"`
	CIDR      string `json:"cidr"`
	Allocated bool   `json:"allocated"`
}

// HostPage is one page of hosts inside an allocated prefix.
// IPv4 prefixes longer than /24 (more than 256 addresses) are paged as
// successive /24s. IPv6 prefixes with more than 256 hosts are paged in
// 256-address chunks.
type HostPage struct {
	PrefixID   uint       `json:"prefix_id"`
	Prefix     string     `json:"prefix"`
	VRFID      uint       `json:"vrf_id"`
	VRFName    string     `json:"vrf_name"`
	Family     int        `json:"family"`
	Bits       int        `json:"bits"`
	Page       int        `json:"page"`
	PageCount  int        `json:"page_count"`
	PagePrefix string     `json:"page_prefix"`
	PageSize   int        `json:"page_size"`
	Hosts      []HostCell `json:"hosts"`
}

func PrefixHosts(db *gorm.DB, prefixID uint, page int) (*HostPage, error) {
	var row models.IpamPrefix
	if err := db.First(&row, prefixID).Error; err != nil {
		return nil, statusErr(404, "prefix not found")
	}
	p, err := ParsePrefix(row.Prefix)
	if err != nil {
		return nil, statusErr(400, err.Error())
	}
	var vrf models.IpamVRF
	if err := db.First(&vrf, row.VRFID).Error; err != nil {
		return nil, statusErr(404, "VRF not found")
	}

	bitLen := p.Addr().BitLen()
	hostBits := bitLen - p.Bits()
	pageBits := 8
	if hostBits < pageBits {
		pageBits = hostBits
	}
	pageSize := 1 << pageBits
	var pageCount int
	if hostBits <= pageBits {
		pageCount = 1
	} else {
		shift := hostBits - pageBits
		if shift > 30 {
			return nil, statusErr(400, "prefix is too large to page; pick a more specific prefix")
		}
		pageCount = 1 << shift
	}
	if page < 0 {
		page = 0
	}
	if page >= pageCount {
		page = pageCount - 1
	}

	start := addrAdd(p.Addr(), uint64(page)*uint64(pageSize))
	pagePfx := netip.PrefixFrom(start, bitLen-pageBits).Masked()

	taken := map[netip.Addr]bool{}
	if err := markAllocatedHosts(db, row, p, pagePfx, taken); err != nil {
		return nil, err
	}

	hosts := make([]HostCell, 0, pageSize)
	addr := start
	pfxBits := p.Bits()
	for i := 0; i < pageSize; i++ {
		if !p.Contains(addr) {
			break
		}
		hosts = append(hosts, HostCell{
			Address:   addr.String(),
			CIDR:      addr.String() + "/" + strconv.Itoa(pfxBits),
			Allocated: taken[addr],
		})
		addr = addrAdd(addr, 1)
	}

	vrfName := vrf.Name
	if vrf.IsDefault {
		vrfName = ""
	}
	return &HostPage{
		PrefixID:   row.ID,
		Prefix:     row.Prefix,
		VRFID:      row.VRFID,
		VRFName:    vrfName,
		Family:     familyOf(p),
		Bits:       p.Bits(),
		Page:       page,
		PageCount:  pageCount,
		PagePrefix: pagePfx.String(),
		PageSize:   pageSize,
		Hosts:      hosts,
	}, nil
}

func markAllocatedHosts(db *gorm.DB, row models.IpamPrefix, parent, pagePfx netip.Prefix, taken map[netip.Addr]bool) error {
	var addrs []models.Address
	if err := db.Where("prefix_id = ?", row.ID).Find(&addrs).Error; err != nil {
		return err
	}
	var children []models.IpamPrefix
	if err := db.Where("vrf_id = ? AND id <> ?", row.VRFID, row.ID).Find(&children).Error; err != nil {
		return err
	}
	childIDs := make([]uint, 0)
	for _, c := range children {
		cp, err := ParsePrefix(c.Prefix)
		if err != nil {
			continue
		}
		if !strictlyContains(parent, cp) {
			continue
		}
		childIDs = append(childIDs, c.ID)
		if pagePfx.Overlaps(cp) {
			markPrefixHosts(pagePfx, cp, taken)
		}
	}
	if len(childIDs) > 0 {
		var childAddrs []models.Address
		if err := db.Where("prefix_id IN ?", childIDs).Find(&childAddrs).Error; err != nil {
			return err
		}
		addrs = append(addrs, childAddrs...)
	}
	for _, a := range addrs {
		host, ok := parseHostAddr(a.Address)
		if !ok || !pagePfx.Contains(host) {
			continue
		}
		taken[host] = true
	}
	return nil
}

func markPrefixHosts(pagePfx, child netip.Prefix, taken map[netip.Addr]bool) {
	addr := child.Addr()
	if !pagePfx.Contains(addr) {
		// Child starts before this page; start at the page network.
		addr = pagePfx.Addr()
		if !child.Contains(addr) {
			return
		}
	}
	n := 0
	for n < hostPageSize && pagePfx.Contains(addr) && child.Contains(addr) {
		taken[addr] = true
		addr = addrAdd(addr, 1)
		n++
	}
}

func parseHostAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a, true
}

func addrAdd(a netip.Addr, n uint64) netip.Addr {
	if n == 0 {
		return a
	}
	b := a.AsSlice()
	carry := n
	for i := len(b) - 1; i >= 0 && carry > 0; i-- {
		sum := uint64(b[i]) + carry
		b[i] = byte(sum)
		carry = sum >> 8
	}
	out, ok := netip.AddrFromSlice(b)
	if !ok {
		return a
	}
	if a.Is4() {
		return out.Unmap()
	}
	return out
}
