package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abundo/factum2/internal/dcim"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func netboxCableExtra(label string) map[string]any {
	return map[string]any{"label": strings.TrimSpace(label)}
}

func (ctrl *Controller) netboxClientIfConfigured() (*netboxtool.NetboxClient, error) {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.NetboxApiURL) == "" || strings.TrimSpace(settings.NetboxApiToken) == "" {
		return nil, nil
	}
	return ctrl.newNetboxClient(settings)
}

func netboxUnavailable(c *echo.Context) error {
	return c.JSON(http.StatusBadRequest, map[string]any{
		"error": "NetBox is not configured; cables between NetBox interfaces cannot be changed here",
	})
}

func netboxUpstream(c *echo.Context, err error) error {
	return c.JSON(http.StatusBadGateway, map[string]any{"error": fmt.Sprintf("NetBox: %v", err)})
}

func (ctrl *Controller) ApiDCIMConnectionPair(c *echo.Context) error {
	aID := parseUintQuery(c, "a")
	bID := parseUintQuery(c, "b")
	if aID == 0 {
		aID = parseUintQuery(c, "device_a")
	}
	if bID == 0 {
		bID = parseUintQuery(c, "device_b")
	}
	row, err := dcim.ConnectionPair(ctrl.DB, aID, bID)
	if err != nil {
		return dcimError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMConnectionCreate(c *echo.Context) error {
	var dto models.ConnectionWriteDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	a, b, err := dcim.LoadCableEnds(ctrl.DB, dto.InterfaceAID, dto.InterfaceBID)
	if err != nil {
		return dcimError(c, err)
	}
	if err := dcim.AssertInterfacesFree(ctrl.DB, a.ID, b.ID, 0); err != nil {
		return dcimError(c, err)
	}
	var nbID uint
	if dcim.BothNetboxInterfaces(a, b) {
		nb, err := ctrl.netboxClientIfConfigured()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if nb == nil {
			return netboxUnavailable(c)
		}
		created, err := nb.CreateCableWithOptions(a.NetboxID, b.NetboxID, netboxCableExtra(dto.Label))
		if err != nil {
			return netboxUpstream(c, err)
		}
		nbID = created.NetboxID
	}
	row, err := dcim.CreateCable(ctrl.DB, dto.InterfaceAID, dto.InterfaceBID, dto.Label)
	if err != nil {
		if nbID != 0 {
			if nb, nerr := ctrl.netboxClientIfConfigured(); nerr == nil && nb != nil {
				_ = nb.DeleteCable(nbID)
			}
		}
		return dcimError(c, err)
	}
	if nbID != 0 {
		if err := dcim.SetCableNetboxID(ctrl.DB, row.ID, nbID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		row.NetboxID = nbID
	}
	if err := optical.RebuildStale(ctrl.DB); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiDCIMConnectionUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.ConnectionWriteDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	existing, err := dcim.GetCable(ctrl.DB, id)
	if err != nil {
		return dcimError(c, err)
	}
	a, b, err := dcim.LoadCableEnds(ctrl.DB, dto.InterfaceAID, dto.InterfaceBID)
	if err != nil {
		return dcimError(c, err)
	}
	if existing.NetboxID != 0 || dcim.BothNetboxInterfaces(a, b) {
		if existing.NetboxID != 0 && (a.NetboxID == 0 || b.NetboxID == 0) {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"error": "a NetBox cable cannot terminate on a Factum-only interface",
			})
		}
		nb, err := ctrl.netboxClientIfConfigured()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if nb == nil {
			return netboxUnavailable(c)
		}
		if existing.NetboxID != 0 {
			if err := nb.UpdateCable(existing.NetboxID, a.NetboxID, b.NetboxID, netboxCableExtra(dto.Label)); err != nil {
				return netboxUpstream(c, err)
			}
		} else {
			created, err := nb.CreateCableWithOptions(a.NetboxID, b.NetboxID, netboxCableExtra(dto.Label))
			if err != nil {
				return netboxUpstream(c, err)
			}
			if err := dcim.SetCableNetboxID(ctrl.DB, existing.ID, created.NetboxID); err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
			}
		}
	}
	_ = optical.MarkStaleByConnection(ctrl.DB, id)
	row, err := dcim.UpdateCable(ctrl.DB, id, dto.InterfaceAID, dto.InterfaceBID, dto.Label)
	if err != nil {
		return dcimError(c, err)
	}
	if err := optical.RebuildStale(ctrl.DB); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiDCIMConnectionDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	existing, err := dcim.GetCable(ctrl.DB, id)
	if err != nil {
		return dcimError(c, err)
	}
	if existing.NetboxID != 0 {
		nb, err := ctrl.netboxClientIfConfigured()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		if nb == nil {
			return netboxUnavailable(c)
		}
		if err := nb.DeleteCable(existing.NetboxID); err != nil {
			return netboxUpstream(c, err)
		}
	}
	_ = optical.MarkStaleByConnection(ctrl.DB, id)
	if err := dcim.DeleteCable(ctrl.DB, id); err != nil {
		return dcimError(c, err)
	}
	if err := optical.RebuildStale(ctrl.DB); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
