package web

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// IcingaConfigResponse is what factum2-icinga (internal/icinga's
// FetchRemoteConfig) parses - keep the JSON tags in sync with that type.
type IcingaConfigResponse struct {
	util.CommonConfig
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`

	HostsFile           string                  `json:"hosts_file"`
	UsersFile           string                  `json:"users_file"`
	CertsFile           string                  `json:"certs_file"`
	InterfacesFile      string                  `json:"interfaces_file"`
	MonitorInterfaces   bool                    `json:"monitor_interfaces"`
	SNMPCommunities     string                  `json:"snmp_communities"`
	EAPIUser            string                  `json:"eapi_user"`
	EAPIPassword        string                  `json:"eapi_password"`
	IgnoreDevices       string                  `json:"ignore_devices"`
	DefaultNotification string                  `json:"default_notification"`
	HostTemplate        string                  `json:"host_template"`
	DependencyTemplate  string                  `json:"dependency_template"`
	UserTemplate        string                  `json:"user_template"`
	CertTemplate        string                  `json:"cert_template"`
	Certificates        []util.ConfigIcingaCert `json:"certificates"`
}

// ApiIcingaConfig returns the Icinga API connection settings from the
// database-backed Settings row, so factum2-icinga - which typically runs on
// a different host than the primary - doesn't need its own copy of these
// credentials in a local YAML config file.
func (ctrl *Controller) ApiIcingaConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var list []models.Certificate
	if err := ctrl.DB.Preload("Domains", func(db *gorm.DB) *gorm.DB { return db.Order("rank") }).
		Order("name").Find(&list).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	certs := make([]util.ConfigIcingaCert, 0, len(list))
	for _, cert := range list {
		domains := make([]string, 0, len(cert.Domains))
		for _, d := range cert.Domains {
			domains = append(domains, d.Name)
		}
		certs = append(certs, util.ConfigIcingaCert{
			Name: cert.Name, Host: cert.Host, Domains: domains,
		})
	}
	return c.JSON(http.StatusOK, IcingaConfigResponse{
		CommonConfig: util.WithSMTP(util.NewCommonConfig(settings), settings),
		URL:          settings.IcingaApiURL,
		Username:     settings.IcingaApiUser,
		Password:     settings.IcingaApiPass,

		HostsFile:           settings.IcingaHostsFile,
		UsersFile:           settings.IcingaUsersFile,
		CertsFile:           settings.IcingaCertsFile,
		InterfacesFile:      settings.IcingaInterfacesFile,
		MonitorInterfaces:   settings.IcingaMonitorInterfaces != nil && *settings.IcingaMonitorInterfaces,
		SNMPCommunities:     settings.IcingaSNMPCommunities,
		EAPIUser:            settings.IcingaEAPIUser,
		EAPIPassword:        settings.IcingaEAPIPassword,
		IgnoreDevices:       settings.IcingaIgnoreDevices,
		DefaultNotification: settings.IcingaDefaultNotification,
		HostTemplate:        settings.IcingaHostTemplate,
		DependencyTemplate:  settings.IcingaDependencyTemplate,
		UserTemplate:        settings.IcingaUserTemplate,
		CertTemplate:        settings.IcingaCertTemplate,
		Certificates:        certs,
	})
}

// ApiIcingaElines lists applied ELINE endpoints so factum2-icinga can
// write passive services and poll them. One row per device and service.
func (ctrl *Controller) ApiIcingaElines(c *echo.Context) error {
	rows, err := ListIcingaElines(c.Request().Context(), ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rows)
}

// ListIcingaElines returns applied endpoints of service types whose
// sync source is eline. A service is local when every endpoint is on
// the same device. Platform prefers the catalog slug.
func ListIcingaElines(ctx context.Context, db *gorm.DB) ([]models.IcingaEline, error) {
	var types []models.ServiceType
	if err := db.WithContext(ctx).Where("sync_source = ?", models.SyncSourceELINE).Find(&types).Error; err != nil {
		return nil, err
	}
	if len(types) == 0 {
		return []models.IcingaEline{}, nil
	}
	names := make([]string, 0, len(types))
	for _, st := range types {
		if name := strings.TrimSpace(st.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []models.IcingaEline{}, nil
	}
	var services []models.Service
	if err := db.WithContext(ctx).Where("service_type IN ?", names).Find(&services).Error; err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return []models.IcingaEline{}, nil
	}
	byID := make(map[uint]models.Service, len(services))
	ids := make([]uint, 0, len(services))
	for _, svc := range services {
		byID[svc.ID] = svc
		ids = append(ids, svc.ID)
	}
	var endpoints []models.ServiceEndpoint
	if err := db.WithContext(ctx).Where("service_id IN ?", ids).Find(&endpoints).Error; err != nil {
		return nil, err
	}
	deviceSet := map[uint]map[uint]struct{}{}
	for _, ep := range endpoints {
		if ep.DeviceID == 0 {
			continue
		}
		set := deviceSet[ep.ServiceID]
		if set == nil {
			set = map[uint]struct{}{}
			deviceSet[ep.ServiceID] = set
		}
		set[ep.DeviceID] = struct{}{}
	}
	type key struct {
		device uint
		svc    uint
	}
	seen := map[key]struct{}{}
	deviceIDs := map[uint]struct{}{}
	var picked []models.ServiceEndpoint
	for _, ep := range endpoints {
		if ep.AppliedDeviceID == 0 {
			continue
		}
		k := key{ep.AppliedDeviceID, ep.ServiceID}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		deviceIDs[ep.AppliedDeviceID] = struct{}{}
		picked = append(picked, ep)
	}
	if len(picked) == 0 {
		return []models.IcingaEline{}, nil
	}
	idList := make([]uint, 0, len(deviceIDs))
	for id := range deviceIDs {
		idList = append(idList, id)
	}
	var devices []models.Device
	if err := db.WithContext(ctx).Where("id IN ?", idList).Find(&devices).Error; err != nil {
		return nil, err
	}
	byDevice := make(map[uint]models.Device, len(devices))
	platIDs := map[uint]struct{}{}
	for _, dev := range devices {
		byDevice[dev.ID] = dev
		if dev.PlatformID != 0 {
			platIDs[dev.PlatformID] = struct{}{}
		}
	}
	slugs := map[uint]string{}
	if len(platIDs) > 0 {
		pids := make([]uint, 0, len(platIDs))
		for id := range platIDs {
			pids = append(pids, id)
		}
		var plats []models.Platform
		if err := db.WithContext(ctx).Where("id IN ?", pids).Find(&plats).Error; err != nil {
			return nil, err
		}
		for _, p := range plats {
			slugs[p.ID] = p.Slug
		}
	}
	out := make([]models.IcingaEline, 0, len(picked))
	for _, ep := range picked {
		dev, ok := byDevice[ep.AppliedDeviceID]
		if !ok || !dev.Enabled || strings.TrimSpace(dev.Name) == "" || strings.TrimSpace(dev.PrimaryIPv4) == "" {
			continue
		}
		svc := byID[ep.ServiceID]
		if strings.TrimSpace(svc.ServiceID) == "" {
			continue
		}
		platform := strings.TrimSpace(dev.Platform)
		if slug := strings.TrimSpace(slugs[dev.PlatformID]); slug != "" {
			platform = slug
		}
		local := false
		if set := deviceSet[ep.ServiceID]; len(set) == 1 && len(endpointsFor(endpoints, ep.ServiceID)) >= 2 {
			local = true
		}
		out = append(out, models.IcingaEline{
			Device:    dev.Name,
			Address:   dev.PrimaryIPv4,
			Platform:  platform,
			Monitor:   dev.CfMonitorIcinga,
			ServiceID: svc.ServiceID,
			PWID:      svc.PseudowireID,
			Local:     local,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].ServiceID < out[j].ServiceID
	})
	return out, nil
}

func endpointsFor(eps []models.ServiceEndpoint, serviceID uint) []models.ServiceEndpoint {
	var out []models.ServiceEndpoint
	for _, ep := range eps {
		if ep.ServiceID == serviceID {
			out = append(out, ep)
		}
	}
	return out
}
