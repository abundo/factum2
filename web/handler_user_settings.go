package web

import (
	"net/http"

	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// --------------------------------------------------------------------------
//
//   API
//
// --------------------------------------------------------------------------

// SaveUserSetings is unrouted (superseded by ApiMeUpdate) but left in place;
// it doesn't touch templates so it's out of scope for this cleanup.
func (ctrl *Controller) SaveUserSetings(c *echo.Context) error {
	user := c.Get("user").(models.User)

	var user1 models.User
	if err := ctrl.DB.Where("id = ?", user.ID).First(&user1).Error; err != nil {
		return echo.ErrUnauthorized
	}

	user1.Name = c.FormValue("name")
	user1.Mobile = c.FormValue("mobile")
	ctrl.DB.Save(&user1)

	return c.Redirect(http.StatusFound, ".")
}
