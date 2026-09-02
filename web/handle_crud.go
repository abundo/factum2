package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// idField locates the "ID" field on a *M pointer via reflection, since M is
// generic and Go has no interface for "settable primary key" - every model
// gets it from the embedded FactumModel, but that's a convention, not
// something the type system can express here.
func idField(itemPtr any) reflect.Value {
	return reflect.ValueOf(itemPtr).Elem().FieldByName("ID")
}

// M = Database Model, Req = Request DTO
type SecureCRUDHandler[M any, Req any] struct {
	DB *gorm.DB
}

func NewSecureCRUDHandler[M any, Req any](db *gorm.DB) *SecureCRUDHandler[M, Req] {
	return &SecureCRUDHandler[M, Req]{DB: db}
}

// 1. GET ALL: GET /api/resource
func (h *SecureCRUDHandler[M, Req]) GetAll(c *echo.Context) error {
	var items []M
	if err := h.DB.Find(&items).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, items)
}

// 2. GET ONE: GET /api/resource/:id
// Safety: Safe by default if your Model utilizes `json:"-"` tags on private fields.
// The path id is parsed as uint before it reaches GORM: a non-numeric string
// cond is treated as a raw SQL fragment (Statement.BuildCondition), so
// First(&item, c.Param("id")) is a SQL-injection hole.
func (h *SecureCRUDHandler[M, Req]) GetOne(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item M

	if err := h.DB.First(&item, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	// Returns the model. Sensitive fields marked with `json:"-"` are automatically omitted.
	return c.JSON(http.StatusOK, item)
}

// 3. CREATE: POST /api/resource
// Safety: Prevents Mass Assignment by binding to the DTO first.
func (h *SecureCRUDHandler[M, Req]) Create(c *echo.Context) error {
	var dto Req

	// Bind incoming JSON strictly to the Request DTO
	if err := c.Bind(&dto); err != nil {
		slog.Error("bind formdata to json", "err", err)
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	// Convert the DTO struct into the Database Model struct safely using JSON marshalling
	var item M
	dtoBytes, _ := json.Marshal(dto)
	json.Unmarshal(dtoBytes, &item)

	// The DTO round trip above also happily copies over an "id" field, since
	// every DTO needs ID for its own GET responses - without this, a caller
	// could choose their own primary key on create, colliding with or
	// overwriting an existing row. The DB must always assign a fresh one.
	if f := idField(&item); f.IsValid() && f.CanSet() {
		f.SetZero()
	}

	// Save the clean model to the database
	if err := h.DB.Create(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	// Returns the newly created item (including its new DB-assigned ID)
	return c.JSON(http.StatusCreated, item)
}

// 4. UPDATE: PUT /api/resource/:id
func (h *SecureCRUDHandler[M, Req]) Update(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item M

	if err := h.DB.First(&item, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var dto Req
	if err := c.Bind(&dto); err != nil {
		fmt.Printf("err: %#v\n", err)
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	// Capture the ID as loaded from the URL-addressed row, before merging in
	// the request body: the DTO carries an "id" field too (needed for GET
	// responses), and without restoring it after the merge, a caller could
	// use PUT /resource/:id to move the row to a different, possibly
	// colliding, primary key via the body instead of the URL.
	originalID := idField(&item)
	var savedID reflect.Value
	if originalID.IsValid() {
		savedID = reflect.ValueOf(originalID.Interface())
	}

	// 3. Merge the DTO updates onto your existing database model
	dtoBytes, _ := json.Marshal(dto)
	json.Unmarshal(dtoBytes, &item)

	if f := idField(&item); f.IsValid() && f.CanSet() && savedID.IsValid() {
		f.Set(savedID)
	}

	if err := h.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, item)
}

// 5. DELETE: DELETE /api/resource/:id
func (h *SecureCRUDHandler[M, Req]) Delete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var item M

	if err := h.DB.First(&item, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	if err := h.DB.Delete(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}
