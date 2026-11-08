package web

import (
	"net/http"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func (ctrl *Controller) ApiIpamASNRangeList(c *echo.Context) error {
	var rows []models.IpamASNRange
	if err := ctrl.DB.Order("name").Find(&rows).Error; err != nil {
		return ipamWriteError(c, err)
	}
	if rows == nil {
		rows = []models.IpamASNRange{}
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiIpamASNList(c *echo.Context) error {
	var rows []models.IpamASN
	if err := ctrl.DB.Order("asn").Find(&rows).Error; err != nil {
		return ipamWriteError(c, err)
	}
	if rows == nil {
		rows = []models.IpamASN{}
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiIpamRouteTargetList(c *echo.Context) error {
	var rows []models.IpamRouteTarget
	if err := ctrl.DB.Order("name").Find(&rows).Error; err != nil {
		return ipamWriteError(c, err)
	}
	if rows == nil {
		rows = []models.IpamRouteTarget{}
	}
	return c.JSON(http.StatusOK, rows)
}
