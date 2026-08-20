package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/labstack/echo/v5"
)

func (ctrl *Controller) ipamOn() bool {
	return ipam.Enabled(ctrl.DB)
}

// RequireIpamEnabled 404s IPAM routes when the Factum setting is off.
// Rows are left untouched — this is a visibility gate, not a delete.
func (ctrl *Controller) RequireIpamEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !ctrl.ipamOn() {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "IPAM is disabled"})
		}
		return next(c)
	}
}

func ipamWriteError(c *echo.Context, err error) error {
	var se *ipam.StatusError
	if errors.As(err, &se) {
		return c.JSON(se.Status, map[string]any{"error": se.Message})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func pathUint(c *echo.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

func (ctrl *Controller) ApiIpamNamespaceList(c *echo.Context) error {
	rows, err := ipam.ListNamespaces(ctrl.DB)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiIpamNamespaceGet(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	row, err := ipam.GetNamespace(ctrl.DB, id)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

type ipamNamespaceBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (ctrl *Controller) ApiIpamNamespaceCreate(c *echo.Context) error {
	var body ipamNamespaceBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.CreateNamespace(ctrl.DB, body.Name, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiIpamNamespaceUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var body ipamNamespaceBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.UpdateNamespace(ctrl.DB, id, body.Name, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiIpamNamespaceDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	if err := ipam.DeleteNamespace(ctrl.DB, id); err != nil {
		return ipamWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiIpamPoolList(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	rows, err := ipam.ListPools(ctrl.DB, id)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

type ipamPrefixBody struct {
	Prefix      string `json:"prefix"`
	VRFID       uint   `json:"vrf_id"`
	Description string `json:"description"`
}

func (ctrl *Controller) ApiIpamPoolCreate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var body ipamPrefixBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.AddPool(ctrl.DB, id, body.Prefix)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiIpamPoolDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	poolID, err := pathUint(c, "poolId")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid pool id"})
	}
	if err := ipam.DeletePool(ctrl.DB, id, poolID); err != nil {
		return ipamWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiIpamVRFList(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	rows, err := ipam.ListVRFs(ctrl.DB, id)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

type ipamVRFBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (ctrl *Controller) ApiIpamVRFCreate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var body ipamVRFBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.CreateVRF(ctrl.DB, id, body.Name, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiIpamVRFUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	vrfID, err := pathUint(c, "vrfId")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid vrf id"})
	}
	var body ipamVRFBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.UpdateVRF(ctrl.DB, id, vrfID, body.Name, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiIpamVRFDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	vrfID, err := pathUint(c, "vrfId")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid vrf id"})
	}
	if err := ipam.DeleteVRF(ctrl.DB, id, vrfID); err != nil {
		return ipamWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiIpamPrefixList(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	rows, err := ipam.ListPrefixes(ctrl.DB, id)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiIpamPrefixCreate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	var body ipamPrefixBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.Allocate(ctrl.DB, id, body.VRFID, body.Prefix, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusCreated, row)
}

func (ctrl *Controller) ApiIpamPrefixUpdate(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	prefixID, err := pathUint(c, "prefixId")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid prefix id"})
	}
	var body ipamPrefixBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	row, err := ipam.UpdatePrefix(ctrl.DB, id, prefixID, body.Description)
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, row)
}

func (ctrl *Controller) ApiIpamPrefixDelete(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	prefixID, err := pathUint(c, "prefixId")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid prefix id"})
	}
	if err := ipam.DeletePrefix(ctrl.DB, id, prefixID); err != nil {
		return ipamWriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *Controller) ApiIpamTree(c *echo.Context) error {
	id, err := pathUint(c, "id")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid id"})
	}
	nodes, err := ipam.Tree(ctrl.DB, id, c.QueryParam("prefix"))
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, nodes)
}

// ApiIpamForest is the single-tree API: no parent → namespaces; parent is
// a node key (ns:1, vrf:2, pool:3, pfx:4) → that node's children.
func (ctrl *Controller) ApiIpamForest(c *echo.Context) error {
	parent := c.QueryParam("parent")
	var nodes []ipam.TreeNode
	var err error
	if parent == "" {
		nodes, err = ipam.Roots(ctrl.DB)
	} else {
		nodes, err = ipam.Children(ctrl.DB, parent)
	}
	if err != nil {
		return ipamWriteError(c, err)
	}
	return c.JSON(http.StatusOK, nodes)
}
