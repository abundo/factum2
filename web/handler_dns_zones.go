package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func (ctrl *Controller) RequireDnsZonesEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !dns.ZonesEnabled(ctrl.DB) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "DNS zone editor is disabled"})
		}
		return next(c)
	}
}

type dnsSOABody struct {
	Name    string `json:"name"`
	Mname   string `json:"mname"`
	Rname   string `json:"rname"`
	Refresh uint   `json:"refresh"`
	Retry   uint   `json:"retry"`
	Expire  uint   `json:"expire"`
	TTL     uint   `json:"ttl"`
}

type dnsPolicyBody struct {
	Name                     string `json:"name"`
	KSKLifetime              string `json:"ksk_lifetime"`
	KSKAlgorithm             string `json:"ksk_algorithm"`
	ZSKLifetime              string `json:"zsk_lifetime"`
	ZSKAlgorithm             string `json:"zsk_algorithm"`
	PurgeKeys                string `json:"purge_keys"`
	SignaturesValidity       string `json:"signatures_validity"`
	SignaturesValidityDNSKEY string `json:"signatures_validity_dnskey"`
	SignaturesRefresh        string `json:"signatures_refresh"`
}

type dnsTemplateBody struct {
	Name           string   `json:"name"`
	SOATemplateID  uint     `json:"soa_template_id"`
	DefaultTTL     uint     `json:"default_ttl"`
	DNSSECPolicyID *uint    `json:"dnssec_policy_id"`
	Nameservers    []string `json:"nameservers"`
}

type dnsZoneBody struct {
	Name          string                    `json:"name"`
	Type          string                    `json:"type"`
	DnsTemplateID uint                      `json:"dns_template_id"`
	Comment       string                    `json:"comment"`
	Records       []models.DnsZoneRecordDTO `json:"records"`
}

func dnsTemplateJSON(t *models.DnsTemplate) models.DnsTemplateDTO {
	ns := make([]string, 0, len(t.Nameservers))
	for _, n := range t.Nameservers {
		ns = append(ns, n.Hostname)
	}
	policyID := t.DNSSECPolicyID
	policyName := ""
	if t.DNSSECPolicy != nil {
		policyName = t.DNSSECPolicy.Name
	}
	return models.DnsTemplateDTO{
		ID:             t.ID,
		Name:           t.Name,
		SOATemplateID:  t.SOATemplateID,
		SOATemplate:    t.SOATemplate.Name,
		DefaultTTL:     t.DefaultTTL,
		DNSSECPolicyID: policyID,
		DNSSECPolicy:   policyName,
		Nameservers:    ns,
	}
}

func dnsZoneJSON(z *models.DnsZone, withRecords bool) models.DnsZoneDTO {
	out := models.DnsZoneDTO{
		ID:            z.ID,
		Name:          z.Name,
		Type:          z.Type,
		DnsTemplateID: z.DnsTemplateID,
		DnsTemplate:   z.DnsTemplate.Name,
		Comment:       z.Comment,
	}
	if withRecords {
		out.Records = dns.ZoneRecordsDTO(z.Records)
		if out.Records == nil {
			out.Records = []models.DnsZoneRecordDTO{}
		}
	}
	return out
}

func (ctrl *Controller) dnsTemplatePreload() *gorm.DB {
	return ctrl.DB.Preload("SOATemplate").Preload("DNSSECPolicy").
		Preload("Nameservers", func(db *gorm.DB) *gorm.DB { return db.Order("rank") })
}

func (ctrl *Controller) dnsZonePreload(withRecords bool) *gorm.DB {
	q := ctrl.DB.Preload("DnsTemplate")
	if withRecords {
		q = q.Preload("Records", func(db *gorm.DB) *gorm.DB { return db.Order("rank") })
	}
	return q
}

func (ctrl *Controller) ApiDnsSOAList(c *echo.Context) error {
	var items []models.DnsSOATemplate
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiDnsSOAGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsSOATemplate
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func applySOA(item *models.DnsSOATemplate, req dnsSOABody) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	item.Name = name
	item.Mname = strings.TrimSpace(req.Mname)
	item.Rname = strings.TrimSpace(req.Rname)
	item.Refresh = req.Refresh
	item.Retry = req.Retry
	item.Expire = req.Expire
	item.TTL = req.TTL
	return nil
}

func (ctrl *Controller) ApiDnsSOACreate(c *echo.Context) error {
	var req dnsSOABody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var item models.DnsSOATemplate
	if err := applySOA(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, item)
}

func (ctrl *Controller) ApiDnsSOAUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsSOATemplate
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req dnsSOABody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applySOA(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiDnsSOADelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.DnsTemplate{}).Where("soa_template_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "SOA template is still used by DNS templates"})
	}
	res := ctrl.DB.Delete(&models.DnsSOATemplate{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiDnsPolicyList(c *echo.Context) error {
	var items []models.DnsDNSSECPolicy
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiDnsPolicyGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsDNSSECPolicy
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func applyPolicy(item *models.DnsDNSSECPolicy, req dnsPolicyBody) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	item.Name = name
	item.KSKLifetime = strings.TrimSpace(req.KSKLifetime)
	item.KSKAlgorithm = strings.TrimSpace(req.KSKAlgorithm)
	item.ZSKLifetime = strings.TrimSpace(req.ZSKLifetime)
	item.ZSKAlgorithm = strings.TrimSpace(req.ZSKAlgorithm)
	item.PurgeKeys = strings.TrimSpace(req.PurgeKeys)
	item.SignaturesValidity = strings.TrimSpace(req.SignaturesValidity)
	item.SignaturesValidityDNSKEY = strings.TrimSpace(req.SignaturesValidityDNSKEY)
	item.SignaturesRefresh = strings.TrimSpace(req.SignaturesRefresh)
	return nil
}

func (ctrl *Controller) ApiDnsPolicyCreate(c *echo.Context) error {
	var req dnsPolicyBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var item models.DnsDNSSECPolicy
	if err := applyPolicy(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, item)
}

func (ctrl *Controller) ApiDnsPolicyUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsDNSSECPolicy
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req dnsPolicyBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applyPolicy(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiDnsPolicyDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.DnsTemplate{}).Where("dnssec_policy_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "DNSSEC policy is still used by DNS templates"})
	}
	res := ctrl.DB.Delete(&models.DnsDNSSECPolicy{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiDnsTemplateList(c *echo.Context) error {
	var items []models.DnsTemplate
	if err := ctrl.dnsTemplatePreload().Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]models.DnsTemplateDTO, 0, len(items))
	for i := range items {
		out = append(out, dnsTemplateJSON(&items[i]))
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiDnsTemplateGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsTemplate
	if err := ctrl.dnsTemplatePreload().First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dnsTemplateJSON(&item))
}

func (ctrl *Controller) resolveTemplateRefs(req dnsTemplateBody) (*uint, error) {
	if req.SOATemplateID == 0 {
		return nil, errors.New("SOA template is required")
	}
	var soa models.DnsSOATemplate
	if err := ctrl.DB.First(&soa, req.SOATemplateID).Error; err != nil {
		return nil, errors.New("SOA template not found")
	}
	policyID := dns.OptionalPolicyID(req.DNSSECPolicyID)
	if policyID != nil {
		var pol models.DnsDNSSECPolicy
		if err := ctrl.DB.First(&pol, *policyID).Error; err != nil {
			return nil, errors.New("DNSSEC policy not found")
		}
	}
	return policyID, nil
}

func (ctrl *Controller) ApiDnsTemplateCreate(c *echo.Context) error {
	var req dnsTemplateBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	policyID, err := ctrl.resolveTemplateRefs(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	ns, err := dns.ParseTemplateNameservers(req.Nameservers)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	item := models.DnsTemplate{
		Name:           name,
		SOATemplateID:  req.SOATemplateID,
		DefaultTTL:     req.DefaultTTL,
		DNSSECPolicyID: policyID,
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return dns.ReplaceTemplateNameservers(tx, item.ID, ns)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.dnsTemplatePreload().First(&item, item.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, dnsTemplateJSON(&item))
}

func (ctrl *Controller) ApiDnsTemplateUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsTemplate
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req dnsTemplateBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	policyID, err := ctrl.resolveTemplateRefs(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	ns, err := dns.ParseTemplateNameservers(req.Nameservers)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	item.Name = name
	item.SOATemplateID = req.SOATemplateID
	item.DefaultTTL = req.DefaultTTL
	item.DNSSECPolicyID = policyID
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Nameservers", "SOATemplate", "DNSSECPolicy").Save(&item).Error; err != nil {
			return err
		}
		return dns.ReplaceTemplateNameservers(tx, item.ID, ns)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.dnsTemplatePreload().First(&item, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dnsTemplateJSON(&item))
}

func (ctrl *Controller) ApiDnsTemplateDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.DnsZone{}).Where("dns_template_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "DNS template is still used by zones"})
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dns_template_id = ?", id).Delete(&models.DnsTemplateNameserver{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&models.DnsTemplate{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiDnsZoneList(c *echo.Context) error {
	var items []models.DnsZone
	if err := ctrl.dnsZonePreload(false).Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]models.DnsZoneDTO, 0, len(items))
	for i := range items {
		out = append(out, dnsZoneJSON(&items[i], false))
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiDnsZoneGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.DnsZone
	if err := ctrl.dnsZonePreload(true).First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dnsZoneJSON(&item, true))
}

func validateDnsZoneName(name, typ string) error {
	if name == "" {
		return errors.New("name is required")
	}
	switch typ {
	case models.DnsZoneTypeForward:
		return nil
	case models.DnsZoneTypeReverse4, models.DnsZoneTypeReverse6:
		pfx, err := netip.ParsePrefix(name)
		if err != nil {
			return fmt.Errorf("reverse zone name must be a prefix")
		}
		if pfx != pfx.Masked() {
			return errors.New("reverse prefix has host bits set")
		}
		if typ == models.DnsZoneTypeReverse4 && !pfx.Addr().Is4() {
			return errors.New("reverse4 zone requires an IPv4 prefix")
		}
		if typ == models.DnsZoneTypeReverse6 && !pfx.Addr().Is6() {
			return errors.New("reverse6 zone requires an IPv6 prefix")
		}
		return nil
	default:
		return fmt.Errorf("unknown zone type %q", typ)
	}
}

func (ctrl *Controller) parseZoneBody(req dnsZoneBody) (string, string, uint, []models.DnsZoneRecord, error) {
	name := strings.TrimSpace(req.Name)
	typ := strings.TrimSpace(req.Type)
	if typ == "" {
		typ = models.DnsZoneTypeForward
	}
	if err := validateDnsZoneName(name, typ); err != nil {
		return "", "", 0, nil, err
	}
	if req.DnsTemplateID == 0 {
		return "", "", 0, nil, errors.New("DNS template is required")
	}
	var tmpl models.DnsTemplate
	if err := ctrl.DB.First(&tmpl, req.DnsTemplateID).Error; err != nil {
		return "", "", 0, nil, errors.New("DNS template not found")
	}
	recs, err := dns.ParseZoneRecords(req.Records)
	if err != nil {
		return "", "", 0, nil, err
	}
	return name, typ, req.DnsTemplateID, recs, nil
}

func (ctrl *Controller) ApiDnsZoneCreate(c *echo.Context) error {
	var req dnsZoneBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name, typ, tmplID, recs, err := ctrl.parseZoneBody(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	zone := models.DnsZone{
		Name:          name,
		Type:          typ,
		DnsTemplateID: tmplID,
		Comment:       strings.TrimSpace(req.Comment),
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("DnsTemplate", "Records").Create(&zone).Error; err != nil {
			return err
		}
		return dns.ReplaceZoneRecords(tx, zone.ID, recs)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.dnsZonePreload(true).First(&zone, zone.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, dnsZoneJSON(&zone, true))
}

func (ctrl *Controller) ApiDnsZoneUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var zone models.DnsZone
	if err := ctrl.DB.First(&zone, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req dnsZoneBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name, typ, tmplID, recs, err := ctrl.parseZoneBody(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	zone.Name = name
	zone.Type = typ
	zone.DnsTemplateID = tmplID
	zone.Comment = strings.TrimSpace(req.Comment)
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("DnsTemplate", "Records").Save(&zone).Error; err != nil {
			return err
		}
		return dns.ReplaceZoneRecords(tx, zone.ID, recs)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.dnsZonePreload(true).First(&zone, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, dnsZoneJSON(&zone, true))
}

func (ctrl *Controller) ApiDnsZoneDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var zone models.DnsZone
	if err := ctrl.DB.First(&zone, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dns_zone_id = ?", zone.ID).Delete(&models.DnsZoneRecord{}).Error; err != nil {
			return err
		}
		return tx.Delete(&zone).Error
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
