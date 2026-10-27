package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/certs"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

const maxLegoImportBytes = 1 << 20

type certImportBody struct {
	YAML string `json:"yaml"`
	Env  string `json:"env"`
}

type certImportResult struct {
	AccountsCreated     int      `json:"accounts_created"`
	AccountsUpdated     int      `json:"accounts_updated"`
	ChallengesCreated   int      `json:"challenges_created"`
	ChallengesUpdated   int      `json:"challenges_updated"`
	CertificatesCreated int      `json:"certificates_created"`
	CertificatesUpdated int      `json:"certificates_updated"`
	Skipped             []string `json:"skipped"`
}

func (ctrl *Controller) ApiCertsImport(c *echo.Context) error {
	var req certImportBody
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if len(req.YAML) > maxLegoImportBytes || len(req.Env) > maxLegoImportBytes {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "file is too large"})
	}
	if strings.TrimSpace(req.YAML) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "yaml is required"})
	}
	imported, err := certs.ParseLegoFile(req.YAML, req.Env)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	var result certImportResult
	result.Skipped = imported.Skipped
	err = ctrl.DB.Transaction(func(tx *gorm.DB) error {
		accountIDs, created, updated, err := upsertImportedAccounts(tx, imported.Accounts)
		if err != nil {
			return err
		}
		result.AccountsCreated = created
		result.AccountsUpdated = updated
		challengeIDs, created, updated, err := upsertImportedChallenges(tx, imported.Challenges, imported.EnvApplied)
		if err != nil {
			return err
		}
		result.ChallengesCreated = created
		result.ChallengesUpdated = updated
		created, updated, err = ctrl.upsertImportedCertificates(tx, imported.Certificates, accountIDs, challengeIDs)
		if err != nil {
			return err
		}
		result.CertificatesCreated = created
		result.CertificatesUpdated = updated
		return nil
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "was not found") {
			status = http.StatusBadRequest
		}
		return c.JSON(status, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, result)
}

func resolveImportedID(tx *gorm.DB, known map[string]uint, model any, name, kind string) (uint, error) {
	if id := known[name]; id != 0 {
		return id, nil
	}
	err := tx.Where("name = ?", name).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, errors.New(kind + " " + name + " was not found")
	}
	if err != nil {
		return 0, err
	}
	switch row := model.(type) {
	case *models.CertAccount:
		return row.ID, nil
	case *models.CertChallenge:
		return row.ID, nil
	default:
		return 0, errors.New(kind + " " + name + " was not found")
	}
}

func upsertImportedAccounts(tx *gorm.DB, items []certs.Account) (map[string]uint, int, int, error) {
	ids := map[string]uint{}
	created, updated := 0, 0
	for _, raw := range items {
		var item models.CertAccount
		err := tx.Where("name = ?", raw.Name).First(&item).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !isNew {
			return nil, 0, 0, err
		}
		item.Name = raw.Name
		item.Email = raw.Email
		item.Server = raw.Server
		item.KeyType = raw.KeyType
		item.AcceptsTermsOfService = raw.AcceptsTermsOfService
		if isNew || raw.EABKID != "" || raw.EABHMACKey != "" {
			item.EABKID = raw.EABKID
			item.EABHMACKey = raw.EABHMACKey
		}
		if isNew {
			if err := tx.Create(&item).Error; err != nil {
				return nil, 0, 0, err
			}
			created++
		} else if err := tx.Save(&item).Error; err != nil {
			return nil, 0, 0, err
		} else {
			updated++
		}
		ids[raw.Name] = item.ID
	}
	return ids, created, updated, nil
}

func upsertImportedChallenges(tx *gorm.DB, items []certs.Challenge, envApplied bool) (map[string]uint, int, int, error) {
	ids := map[string]uint{}
	created, updated := 0, 0
	for _, raw := range items {
		var item models.CertChallenge
		err := tx.Where("name = ?", raw.Name).First(&item).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !isNew {
			return nil, 0, 0, err
		}
		item.Name = raw.Name
		item.Kind = raw.Kind
		item.Provider = raw.Provider
		item.DNSTimeout = raw.DNSTimeout
		item.Resolvers = strings.Join(raw.Resolvers, "\n")
		item.DisableAuthNS = raw.DisableAuthNS
		item.DisableRecursiveNS = raw.DisableRecursiveNS
		item.PropagationWait = raw.PropagationWait
		if isNew || envApplied {
			// A value present in the dotenv replaces the stored one. A missing
			// key leaves the stored value, so a partial .env does not wipe it.
			setEnv := func(dst *string, v string) {
				if isNew || v != "" {
					*dst = v
				}
			}
			setEnv(&item.RFC2136Nameserver, raw.RFC2136Nameserver)
			setEnv(&item.RFC2136TSIGAlgo, raw.RFC2136TSIGAlgo)
			setEnv(&item.RFC2136TSIGKey, raw.RFC2136TSIGKey)
			setEnv(&item.RFC2136TSIGSecret, raw.RFC2136TSIGSecret)
			setEnv(&item.RFC2136TSIGFile, raw.RFC2136TSIGFile)
			setEnv(&item.RFC2136PropTimeout, raw.RFC2136PropTimeout)
			setEnv(&item.RFC2136PollInterval, raw.RFC2136PollInterval)
			if isNew || raw.RFC2136TTL > 0 {
				item.RFC2136TTL = raw.RFC2136TTL
			}
			item.ExtraEnv = raw.ExtraEnv
		}
		if isNew {
			if item.RFC2136TSIGAlgo == "" {
				item.RFC2136TSIGAlgo = "hmac-sha256."
			}
			if item.RFC2136TTL == 0 {
				item.RFC2136TTL = 120
			}
			if err := tx.Create(&item).Error; err != nil {
				return nil, 0, 0, err
			}
			created++
		} else if err := tx.Save(&item).Error; err != nil {
			return nil, 0, 0, err
		} else {
			updated++
		}
		ids[raw.Name] = item.ID
	}
	return ids, created, updated, nil
}

func (ctrl *Controller) upsertImportedCertificates(tx *gorm.DB, items []certs.Cert, accountIDs, challengeIDs map[string]uint) (int, int, error) {
	created, updated := 0, 0
	for _, raw := range items {
		accountID, err := resolveImportedID(tx, accountIDs, &models.CertAccount{}, raw.Account, "account")
		if err != nil {
			return 0, 0, err
		}
		challengeID, err := resolveImportedID(tx, challengeIDs, &models.CertChallenge{}, raw.Challenge, "challenge")
		if err != nil {
			return 0, 0, err
		}
		var item models.Certificate
		err = tx.Where("name = ?", raw.Name).First(&item).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !isNew {
			return 0, 0, err
		}
		item.Name = raw.Name
		item.AccountID = accountID
		item.ChallengeID = challengeID
		if isNew || strings.TrimSpace(raw.KeyType) != "" {
			item.KeyType = strings.TrimSpace(raw.KeyType)
		}
		if isNew || raw.EnableCommonName != nil {
			item.EnableCommonName = raw.EnableCommonName
		}
		if isNew {
			if err := tx.Create(&item).Error; err != nil {
				return 0, 0, err
			}
			created++
		} else if err := tx.Save(&item).Error; err != nil {
			return 0, 0, err
		} else {
			updated++
		}
		if err := ctrl.replaceCertificateDomains(tx, item.ID, raw.Domains); err != nil {
			return 0, 0, err
		}
	}
	return created, updated, nil
}
