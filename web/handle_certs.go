package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/certs"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

func (ctrl *Controller) RequireCertsEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !certs.Enabled(ctrl.DB) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "certificate management is disabled"})
		}
		return next(c)
	}
}

func certSplitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (ctrl *Controller) ApiCertsConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	resp := certs.Config{
		CommonConfig:            util.NewCommonConfig(settings),
		LegoYaml:                settings.CertsLegoYaml,
		EnvFile:                 settings.CertsEnvFile,
		LegoBin:                 settings.CertsLegoBin,
		LegoStorage:             settings.CertsLegoStorage,
		DefaultKeyType:          settings.CertsDefaultKeyType,
		DefaultEnableCommonName: settings.CertsDefaultEnableCommonName != nil && *settings.CertsDefaultEnableCommonName,
	}
	certs.ApplyConfigDefaults(&resp)
	var accounts []models.CertAccount
	if err := ctrl.DB.Order("name").Find(&accounts).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for _, a := range accounts {
		resp.Accounts = append(resp.Accounts, certs.Account{
			Name: a.Name, Email: a.Email, Server: a.Server, KeyType: a.KeyType,
			AcceptsTermsOfService: a.AcceptsTermsOfService, EABKID: a.EABKID, EABHMACKey: a.EABHMACKey,
		})
	}
	var challenges []models.CertChallenge
	if err := ctrl.DB.Order("name").Find(&challenges).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for _, ch := range challenges {
		resp.Challenges = append(resp.Challenges, certs.Challenge{
			Name: ch.Name, Kind: ch.Kind, Provider: ch.Provider, DNSTimeout: ch.DNSTimeout,
			Resolvers: certSplitLines(ch.Resolvers), DisableAuthNS: ch.DisableAuthNS,
			DisableRecursiveNS: ch.DisableRecursiveNS, PropagationWait: ch.PropagationWait,
			RFC2136Nameserver: ch.RFC2136Nameserver, RFC2136TSIGAlgo: ch.RFC2136TSIGAlgo,
			RFC2136TSIGKey: ch.RFC2136TSIGKey, RFC2136TSIGSecret: ch.RFC2136TSIGSecret,
			RFC2136TSIGFile: ch.RFC2136TSIGFile, RFC2136TTL: ch.RFC2136TTL,
			RFC2136PropTimeout: ch.RFC2136PropTimeout, RFC2136PollInterval: ch.RFC2136PollInterval,
			ExtraEnv: ch.ExtraEnv,
		})
	}
	var list []models.Certificate
	if err := ctrl.DB.Preload("Account").Preload("Challenge").
		Preload("Domains", func(db *gorm.DB) *gorm.DB { return db.Order("rank") }).
		Order("name").Find(&list).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for _, cert := range list {
		domains := make([]string, 0, len(cert.Domains))
		for _, d := range cert.Domains {
			domains = append(domains, d.Name)
		}
		resp.Certificates = append(resp.Certificates, certs.Cert{
			Name: cert.Name, Account: cert.Account.Name, Challenge: cert.Challenge.Name,
			KeyType: cert.KeyType, EnableCommonName: cert.EnableCommonName, Domains: domains,
		})
	}
	return c.JSON(http.StatusOK, resp)
}

type certAccountBody struct {
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Server                string `json:"server"`
	KeyType               string `json:"key_type"`
	AcceptsTermsOfService bool   `json:"accepts_terms_of_service"`
	EABKID                string `json:"eab_kid"`
	EABHMACKey            string `json:"eab_hmac_key"`
}

type certChallengeBody struct {
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	Provider            string `json:"provider"`
	DNSTimeout          int    `json:"dns_timeout"`
	Resolvers           string `json:"resolvers"`
	DisableAuthNS       bool   `json:"disable_authoritative_nameservers"`
	DisableRecursiveNS  bool   `json:"disable_recursive_nameservers"`
	PropagationWait     string `json:"propagation_wait"`
	RFC2136Nameserver   string `json:"rfc2136_nameserver"`
	RFC2136TSIGAlgo     string `json:"rfc2136_tsig_algorithm"`
	RFC2136TSIGKey      string `json:"rfc2136_tsig_key"`
	RFC2136TSIGSecret   string `json:"rfc2136_tsig_secret"`
	RFC2136TSIGFile     string `json:"rfc2136_tsig_file"`
	RFC2136TTL          int    `json:"rfc2136_ttl"`
	RFC2136PropTimeout  string `json:"rfc2136_propagation_timeout"`
	RFC2136PollInterval string `json:"rfc2136_polling_interval"`
	ExtraEnv            string `json:"extra_env"`
}

type certificateBody struct {
	Name             string   `json:"name"`
	AccountID        uint     `json:"account_id"`
	ChallengeID      uint     `json:"challenge_id"`
	KeyType          string   `json:"key_type"`
	EnableCommonName *bool    `json:"enable_common_name"`
	Domains          []string `json:"domains"`
}

func applyCertAccount(item *models.CertAccount, req certAccountBody) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	item.Name = name
	item.Email = strings.TrimSpace(req.Email)
	item.Server = strings.TrimSpace(req.Server)
	item.KeyType = strings.TrimSpace(req.KeyType)
	item.AcceptsTermsOfService = req.AcceptsTermsOfService
	item.EABKID = strings.TrimSpace(req.EABKID)
	item.EABHMACKey = strings.TrimSpace(req.EABHMACKey)
	return nil
}

func applyCertChallenge(item *models.CertChallenge, req certChallengeBody) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = models.CertChallengeKindDNS01
	}
	if kind != models.CertChallengeKindDNS01 {
		return errors.New("only dns-01 challenges are supported")
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = models.CertProviderRFC2136
	}
	if provider != models.CertProviderRFC2136 {
		return errors.New("only rfc2136 DNS-01 is supported")
	}
	item.Name = name
	item.Kind = kind
	item.Provider = provider
	item.DNSTimeout = req.DNSTimeout
	item.Resolvers = req.Resolvers
	item.DisableAuthNS = req.DisableAuthNS
	item.DisableRecursiveNS = req.DisableRecursiveNS
	item.PropagationWait = strings.TrimSpace(req.PropagationWait)
	item.RFC2136Nameserver = strings.TrimSpace(req.RFC2136Nameserver)
	item.RFC2136TSIGAlgo = strings.TrimSpace(req.RFC2136TSIGAlgo)
	item.RFC2136TSIGKey = strings.TrimSpace(req.RFC2136TSIGKey)
	item.RFC2136TSIGSecret = strings.TrimSpace(req.RFC2136TSIGSecret)
	item.RFC2136TSIGFile = strings.TrimSpace(req.RFC2136TSIGFile)
	item.RFC2136TTL = req.RFC2136TTL
	item.RFC2136PropTimeout = strings.TrimSpace(req.RFC2136PropTimeout)
	item.RFC2136PollInterval = strings.TrimSpace(req.RFC2136PollInterval)
	item.ExtraEnv = req.ExtraEnv
	return nil
}

func certificateJSON(c *models.Certificate) models.CertificateDTO {
	domains := make([]string, 0, len(c.Domains))
	for _, d := range c.Domains {
		domains = append(domains, d.Name)
	}
	return models.CertificateDTO{
		ID: c.ID, Name: c.Name, AccountID: c.AccountID, Account: c.Account.Name,
		ChallengeID: c.ChallengeID, Challenge: c.Challenge.Name,
		KeyType: c.KeyType, EnableCommonName: c.EnableCommonName, Domains: domains,
	}
}

func (ctrl *Controller) certPreload() *gorm.DB {
	return ctrl.DB.Preload("Account").Preload("Challenge").
		Preload("Domains", func(db *gorm.DB) *gorm.DB { return db.Order("rank") })
}

func (ctrl *Controller) ApiCertAccountList(c *echo.Context) error {
	var items []models.CertAccount
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiCertAccountGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.CertAccount
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCertAccountCreate(c *echo.Context) error {
	var req certAccountBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var item models.CertAccount
	if err := applyCertAccount(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, item)
}

func (ctrl *Controller) ApiCertAccountUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.CertAccount
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req certAccountBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applyCertAccount(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCertAccountDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.Certificate{}).Where("account_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "account is still used by certificates"})
	}
	res := ctrl.DB.Delete(&models.CertAccount{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiCertChallengeList(c *echo.Context) error {
	var items []models.CertChallenge
	if err := ctrl.DB.Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *Controller) ApiCertChallengeGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.CertChallenge
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCertChallengeCreate(c *echo.Context) error {
	var req certChallengeBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var item models.CertChallenge
	if err := applyCertChallenge(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Create(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, item)
}

func (ctrl *Controller) ApiCertChallengeUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.CertChallenge
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req certChallengeBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := applyCertChallenge(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *Controller) ApiCertChallengeDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var n int64
	if err := ctrl.DB.Model(&models.Certificate{}).Where("challenge_id = ?", id).Count(&n).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if n > 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "challenge is still used by certificates"})
	}
	res := ctrl.DB.Delete(&models.CertChallenge{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiCertificateList(c *echo.Context) error {
	var items []models.Certificate
	if err := ctrl.certPreload().Order("name").Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	out := make([]models.CertificateDTO, 0, len(items))
	for i := range items {
		out = append(out, certificateJSON(&items[i]))
	}
	return c.JSON(http.StatusOK, out)
}

func (ctrl *Controller) ApiCertificateGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.Certificate
	if err := ctrl.certPreload().First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, certificateJSON(&item))
}

func (ctrl *Controller) replaceCertificateDomains(tx *gorm.DB, certID uint, names []string) error {
	if err := tx.Where("certificate_id = ?", certID).Delete(&models.CertificateDomain{}).Error; err != nil {
		return err
	}
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		d := models.CertificateDomain{CertificateID: certID, Rank: uint(i), Name: n}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
	}
	return nil
}

func (ctrl *Controller) applyCertificate(item *models.Certificate, req certificateBody) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if req.AccountID == 0 {
		return errors.New("account_id is required")
	}
	if req.ChallengeID == 0 {
		return errors.New("challenge_id is required")
	}
	var n int64
	if err := ctrl.DB.Model(&models.CertAccount{}).Where("id = ?", req.AccountID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return errors.New("account not found")
	}
	if err := ctrl.DB.Model(&models.CertChallenge{}).Where("id = ?", req.ChallengeID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return errors.New("challenge not found")
	}
	item.Name = name
	item.AccountID = req.AccountID
	item.ChallengeID = req.ChallengeID
	item.KeyType = strings.TrimSpace(req.KeyType)
	item.EnableCommonName = req.EnableCommonName
	return nil
}

func (ctrl *Controller) ApiCertificateCreate(c *echo.Context) error {
	var req certificateBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var item models.Certificate
	if err := ctrl.applyCertificate(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return ctrl.replaceCertificateDomains(tx, item.ID, req.Domains)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.certPreload().First(&item, item.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, certificateJSON(&item))
}

func (ctrl *Controller) ApiCertificateUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var item models.Certificate
	if err := ctrl.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	var req certificateBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.applyCertificate(&item, req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		return ctrl.replaceCertificateDomains(tx, item.ID, req.Domains)
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.certPreload().First(&item, item.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, certificateJSON(&item))
}

func (ctrl *Controller) ApiCertificateDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	res := ctrl.DB.Delete(&models.Certificate{}, id)
	if res.Error != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": res.Error.Error()})
	}
	if res.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}
