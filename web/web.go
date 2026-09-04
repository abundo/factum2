// Web backend for factum
package web

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/jobscheduler"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type GuiParams struct {
	cmdbase.Params
	Bind string `default:":8090"`
}

const staticDir = "./web/static"

// staticFS is implemented per build mode: fs_dev.go (default) reads straight
// from disk; fs_release.go (`-tags release`) serves from assets embedded into
// the binary at compile time.

// vueFS is the built Vue frontend (web/static/vue), a subtree of staticFS in
// both build modes, so it needs no build-tag-specific implementation of its
// own.
func vueFS() fs.FS {
	sub, err := fs.Sub(staticFS(), "vue")
	if err != nil {
		panic(err)
	}
	return sub
}

type Controller struct {
	DB            *gorm.DB
	LogHub        *LogHub
	RemoteManager *worker.RemoteManager
}

// ----- GUI -----
func GUI(p *GuiParams) error {
	var err error
	var DB *gorm.DB
	util.Config = &p.Config

	devMode := os.Getenv("APP_ENV") == "development"

	// secureCookies gates the "token" cookie's Secure flag (see
	// setAuthCookie/clearAuthCookie in handle_auth.go). Off only in
	// development, where the server is normally accessed over plain
	// http://localhost and a Secure cookie would just never get sent back.
	secureCookies = !devMode

	// jwtKey signs the API auth token (see auth.go). Always fail-closed:
	// a hardcoded default would let anyone who reads the source forge a
	// valid token for any user. APP_ENV=development does not relax this.
	jwtKey, err = jwtSigningKey(util.Config.Web.JWTSecret)
	if err != nil {
		slog.Error(err.Error())
		return err
	}

	DB, err = util.ConnectDatabase(&util.Config.DB)
	if err != nil {
		return err
	}

	e := echo.New()
	e.HTTPErrorHandler = httpErrorHandler

	// Static files
	e.StaticFS("/static", staticFS())
	e.FileFS("/favicon.ico", "favicon.ico", vueFS())

	// Middleware
	e.Use(middleware.RequestLogger())
	if !devMode {
		e.Use(middleware.Recover())
	}

	// logHub receives a copy of every slog record emitted process-wide (see
	// hubHandler in logstream.go) so the frontend's log window has
	// something to stream - wrapping the handler here, after
	// cmdbase.SetupLog has already run, leaves the normal stderr output
	// untouched.
	logHub := NewLogHub()
	slog.SetDefault(slog.New(newHubHandler(slog.Default().Handler(), logHub)))

	// remoteManager dials out to every enabled models.WorkerNode over the
	// hub transport (internal/worker/hub.go) - see AGENTS.md's "Worker /
	// hub transport" section for why the dial direction is reversed from a
	// traditional agent-connects-to-broker model. It's also where agent
	// command output lands (RunAndWait's LogToSlog call feeds the same
	// hubHandler-backed log window as the process's own logs). Run starts
	// after routes and SetAPIHandler so a connected worker never observes
	// a nil API handler. No separate shutdown path since GUI() has none
	// for anything else either (e.Start below blocks until the process
	// exits).
	remoteManager := worker.NewRemoteManager(DB)

	// scheduler fires user-defined JobSchedule rows through the same
	// StartJob path as the Job overview page - one goroutine for the
	// process lifetime, same as RemoteManager above.
	scheduler := jobscheduler.New(DB, func(jobType, triggeredBy string, targets []string) error {
		_, _, err := remoteManager.StartJob(jobType, triggeredBy, targets)
		return err
	})
	go scheduler.Run(context.Background())

	ctrl := Controller{DB: DB, LogHub: logHub, RemoteManager: remoteManager}

	// -----------------------------------------------------------------
	// Web pages
	// -----------------------------------------------------------------
	// The Vue SPA (web/static/vue, built from web/frontend) owns every page
	// route, served from "/". "/api/*" and "/static/*" are registered as
	// their own more specific routes below/above, so they take precedence
	// over this wildcard regardless of registration order.

	// Plain StaticFS would 404 on any path with no matching file on disk -
	// e.g. /tenant/customer, which only exists as a Vue Router route, not a
	// real file. RouteNotFound + the HTML5 static middleware option makes
	// unmatched paths fall back to index.html instead, so a browser refresh
	// (F5) on a client-side route works.
	e.RouteNotFound("/*", func(c *echo.Context) error {
		return echo.ErrNotFound
	}, middleware.StaticWithConfig(middleware.StaticConfig{
		Filesystem: vueFS(),
		HTML5:      true,
	}))

	// -----------------------------------------------------------------
	// API
	// -----------------------------------------------------------------
	api := e.Group("/api")

	// ----- API auth -----
	api.POST("/login", ctrl.ApiLogin)
	api.POST("/logout", ctrl.ApiLogout)
	api.POST("/forgot-password", ctrl.ApiForgotPassword)
	api.POST("/reset-password", ctrl.ApiResetPassword)

	// Public: the login page and sidebar footer show this, and the values
	// (git tag / commit / date / Go version) are not sensitive.
	api.GET("/version", ctrl.ApiVersion)

	// Operator Markdown from docs/user/ (same files GitHub Pages publishes).
	// Any logged-in user, including those with no role — /doc is help, not
	// an inventory view.
	api.GET("/docs", ctrl.ApiDocsList, ctrl.RequireAPIAuth)
	api.GET("/docs/:slug", ctrl.ApiDocsGet, ctrl.RequireAPIAuth)

	// ----- API customers -----
	customers_ := NewSecureCRUDHandler[models.Customer, models.CustomerDTO](DB)
	api.GET("/customer/:id", ctrl.ApiCustomerByID, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/customer", ctrl.ApiCustomer, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.PUT("/customer/:id", ctrl.ApiCustomerUpdate(customers_), ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/customer", customers_.Create, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.DELETE("/customer/:id", ctrl.ApiCustomerDelete, ctrl.RequireAPIAuth, ctrl.RequireWrite)

	// ----- API contacts -----
	contacts_ := NewSecureCRUDHandler[models.Contact, models.ContactDTO](DB)
	api.GET("/contact/:id", ctrl.ApiContactByID, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/contact", ctrl.ApiContact, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.PUT("/contact/:id", ctrl.ApiContactUpdate(contacts_), ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/contact", contacts_.Create, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.DELETE("/contact/:id", ctrl.ApiContactDelete, ctrl.RequireAPIAuth, ctrl.RequireWrite)

	// ----- API Services -----
	services_ := NewSecureCRUDHandler[models.Service, models.ServiceDTO](DB)
	api.GET("/service/:id", ctrl.ApiServiceByID, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/service", ctrl.APIServiceList, ctrl.RequireAPIAuth, ctrl.RequireRead) // services_.GetAll
	api.PUT("/service/:id", ctrl.ApiServiceUpdate(services_), ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/service", ctrl.ApiServiceCreate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.DELETE("/service/:id", ctrl.ApiServiceDelete(services_), ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.PUT("/service/:id/type", ctrl.ApiServiceTypeUpdate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.PUT("/service/:id/eline", ctrl.ApiServiceElineUpdate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/service/:id/eline/push", ctrl.ApiServiceElinePush, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/service/:id/push", ctrl.ApiServicePush, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/service/:id/endpoints", ctrl.ApiServiceEndpointsGet, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.PUT("/service/:id/endpoints", ctrl.ApiServiceEndpointsPut, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/service/:id/path", ctrl.ApiServicePathGet, ctrl.RequireAPIAuth, ctrl.RequireRead, ctrl.RequireOpticalEnabled)
	api.PUT("/service/:id/path", ctrl.ApiServicePathPut, ctrl.RequireAPIAuth, ctrl.RequireWrite, ctrl.RequireOpticalEnabled)

	// ----- API Sync -----
	api.GET("/sync/targets", ctrl.ApiSyncTargets, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.POST("/sync/:target", ctrl.ApiSyncTrigger, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/sync/all", ctrl.ApiSyncTriggerAll, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/jobs", ctrl.ApiJobs, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/jobs/:id/tasks/:taskid/events", ctrl.ApiJobTaskEvents, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/worker/status", ctrl.ApiWorkerStatus, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/schedules", ctrl.ApiScheduleList, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/schedules/:id", ctrl.ApiScheduleGet, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.POST("/schedules", ctrl.ApiScheduleCreate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.PUT("/schedules/:id", ctrl.ApiScheduleUpdate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.DELETE("/schedules/:id", ctrl.ApiScheduleDelete, ctrl.RequireAPIAuth, ctrl.RequireWrite)

	// ----- API: per-service (and common) config for CLI tools that
	// typically run on a different host than the primary
	// (factum2-librenms-cli, factum2-icinga, factum2-dns, factum2-oxidized,
	// factum2-prometheus, factum2-worker agents) -----
	// Not under adminApi: that group's RequireAdmin rejects service-token
	// callers outright (no "user" in context), but these routes need to
	// accept both an admin browsing the API directly and the remote CLI's
	// service token - see RequireAdminOrServiceToken.
	api.GET("/librenms-config", ctrl.ApiLibrenmsConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/librenms/pending-deletes", ctrl.ApiLibrenmsPendingDeleteList, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.PUT("/librenms/pending-deletes/:device_id", ctrl.ApiLibrenmsPendingDeleteUpsert, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.DELETE("/librenms/pending-deletes/:device_id", ctrl.ApiLibrenmsPendingDeleteRemove, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/librenms/pending-deletes/:device_id/delete-next-sync", ctrl.ApiLibrenmsPendingDeleteNextSync, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/icinga-config", ctrl.ApiIcingaConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/dns-config", ctrl.ApiDNSConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/oxidized-config", ctrl.ApiOxidizedConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/oxidized/nodes", ctrl.ApiOxidizedNodes, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/oxidized/node/config", ctrl.ApiOxidizedNodeConfig, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/oxidized/node/versions", ctrl.ApiOxidizedNodeVersions, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/oxidized/node/version", ctrl.ApiOxidizedNodeVersion, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/oxidized/node/diff", ctrl.ApiOxidizedNodeDiff, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/prometheus-config", ctrl.ApiPrometheusConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	// "factum2-worker run" - streams an NDJSON response, see ApiWorkerRun.
	api.POST("/worker/run", ctrl.ApiWorkerRun, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/netbox-config", ctrl.ApiNetboxConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/device-sync-config", ctrl.ApiDeviceSyncConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)
	api.GET("/common-config", ctrl.ApiCommonConfig, ctrl.RequireAPIAuth, ctrl.RequireAdminOrServiceToken)

	// ----- Netbox webhook -----
	// Netbox itself calls this - no session cookie or service token, so it
	// isn't behind RequireAPIAuth. Authenticated instead by an HMAC request
	// body signature, see ApiNetboxWebhook.
	api.POST("/netbox-webhook", ctrl.ApiNetboxWebhook)

	// ----- API admin -----
	adminApi := api.Group("/admin")
	adminApi.Use(ctrl.RequireAPIAuth, ctrl.RequireAdmin)

	api.GET("/device/:id/impact", ctrl.ApiDeviceImpact, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/device/:id", ctrl.ApiGetDeviceByID, ctrl.RequireAPIAuth, ctrl.RequireRead)
	// /device/name/:name is registered separately from /device/:id - it has
	// its own path segment count, so the two never collide.
	api.GET("/device/name/:name", ctrl.ApiGetDeviceByName, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/device", ctrl.ApiGetDevices, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.POST("/device/:id/interfaces/refresh", ctrl.ApiDeviceInterfacesRefresh, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/device/:id/interfaces/update", ctrl.ApiDeviceInterfacesUpdate, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.POST("/device/:id/interfaces/vlans", ctrl.ApiDeviceInterfacesUpdateVlans, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/topology", ctrl.ApiGetTopology, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/topology/devices", ctrl.ApiGetTopologyDevices, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.GET("/topology/geocode", ctrl.ApiTopologyGeocode, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.POST("/topology/devices/:id/location", ctrl.ApiTopologyDeviceLocation, ctrl.RequireAPIAuth, ctrl.RequireWrite)

	ipamg := api.Group("/ipam", ctrl.RequireAPIAuth, ctrl.RequireIpamEnabled)
	ipamg.GET("/namespaces", ctrl.ApiIpamNamespaceList, ctrl.RequireRead)
	ipamg.POST("/namespaces", ctrl.ApiIpamNamespaceCreate, ctrl.RequireWrite)
	ipamg.GET("/namespaces/:id", ctrl.ApiIpamNamespaceGet, ctrl.RequireRead)
	ipamg.PUT("/namespaces/:id", ctrl.ApiIpamNamespaceUpdate, ctrl.RequireWrite)
	ipamg.DELETE("/namespaces/:id", ctrl.ApiIpamNamespaceDelete, ctrl.RequireWrite)
	ipamg.GET("/namespaces/:id/pools", ctrl.ApiIpamPoolList, ctrl.RequireRead)
	ipamg.POST("/namespaces/:id/pools", ctrl.ApiIpamPoolCreate, ctrl.RequireWrite)
	ipamg.DELETE("/namespaces/:id/pools/:poolId", ctrl.ApiIpamPoolDelete, ctrl.RequireWrite)
	ipamg.GET("/namespaces/:id/vrfs", ctrl.ApiIpamVRFList, ctrl.RequireRead)
	ipamg.POST("/namespaces/:id/vrfs", ctrl.ApiIpamVRFCreate, ctrl.RequireWrite)
	ipamg.PUT("/namespaces/:id/vrfs/:vrfId", ctrl.ApiIpamVRFUpdate, ctrl.RequireWrite)
	ipamg.DELETE("/namespaces/:id/vrfs/:vrfId", ctrl.ApiIpamVRFDelete, ctrl.RequireWrite)
	ipamg.GET("/namespaces/:id/prefixes", ctrl.ApiIpamPrefixList, ctrl.RequireRead)
	ipamg.POST("/namespaces/:id/prefixes", ctrl.ApiIpamPrefixCreate, ctrl.RequireWrite)
	ipamg.PUT("/namespaces/:id/prefixes/:prefixId", ctrl.ApiIpamPrefixUpdate, ctrl.RequireWrite)
	ipamg.DELETE("/namespaces/:id/prefixes/:prefixId", ctrl.ApiIpamPrefixDelete, ctrl.RequireWrite)
	ipamg.GET("/namespaces/:id/tree", ctrl.ApiIpamTree, ctrl.RequireRead)
	ipamg.GET("/tree", ctrl.ApiIpamForest, ctrl.RequireRead)

	cfg := api.Group("/config", ctrl.RequireAPIAuth)
	cfg.GET("/scopes", ctrl.ApiConfigScopeList, ctrl.RequireRead)
	cfg.GET("/scopes/tree", ctrl.ApiConfigScopeTree, ctrl.RequireRead)
	cfg.POST("/scopes", ctrl.ApiConfigScopeCreate, ctrl.RequireWrite)
	cfg.PUT("/scopes/:id", ctrl.ApiConfigScopeUpdate, ctrl.RequireWrite)
	cfg.DELETE("/scopes/:id", ctrl.ApiConfigScopeDelete, ctrl.RequireWrite)
	cfg.GET("/variables", ctrl.ApiConfigVariableList, ctrl.RequireRead)
	cfg.GET("/variables/:id", ctrl.ApiConfigVariableGet, ctrl.RequireRead)
	cfg.POST("/variables", ctrl.ApiConfigVariableCreate, ctrl.RequireWrite)
	cfg.PUT("/variables/:id", ctrl.ApiConfigVariableUpdate, ctrl.RequireWrite)
	cfg.DELETE("/variables/:id", ctrl.ApiConfigVariableDelete, ctrl.RequireWrite)
	cfg.GET("/assignments", ctrl.ApiConfigAssignmentList, ctrl.RequireRead)
	cfg.PUT("/assignments", ctrl.ApiConfigAssignmentUpsert, ctrl.RequireWrite)
	cfg.DELETE("/assignments/:id", ctrl.ApiConfigAssignmentDelete, ctrl.RequireWrite)
	cfg.GET("/resolve", ctrl.ApiConfigResolve, ctrl.RequireRead)
	cfg.GET("/matrix", ctrl.ApiConfigMatrix, ctrl.RequireRead)
	cfg.GET("/service-types", ctrl.ApiConfigServiceTypeList, ctrl.RequireRead)
	cfg.GET("/service-types/:id", ctrl.ApiConfigServiceTypeGet, ctrl.RequireRead)
	cfg.POST("/service-types", ctrl.ApiConfigServiceTypeCreate, ctrl.RequireWrite)
	cfg.PUT("/service-types/:id", ctrl.ApiConfigServiceTypeUpdate, ctrl.RequireWrite)
	cfg.DELETE("/service-types/:id", ctrl.ApiConfigServiceTypeDelete, ctrl.RequireWrite)
	cfg.GET("/platform-packs", ctrl.ApiConfigPlatformPackList, ctrl.RequireRead)
	cfg.GET("/platform-packs/:id", ctrl.ApiConfigPlatformPackGet, ctrl.RequireRead)
	cfg.POST("/platform-packs", ctrl.ApiConfigPlatformPackCreate, ctrl.RequireWrite)
	cfg.PUT("/platform-packs/:id", ctrl.ApiConfigPlatformPackUpdate, ctrl.RequireWrite)
	cfg.DELETE("/platform-packs/:id", ctrl.ApiConfigPlatformPackDelete, ctrl.RequireWrite)
	cfg.GET("/macros", ctrl.ApiConfigMacroList, ctrl.RequireRead)
	cfg.GET("/macros/:id", ctrl.ApiConfigMacroGet, ctrl.RequireRead)
	cfg.POST("/macros", ctrl.ApiConfigMacroCreate, ctrl.RequireWrite)
	cfg.PUT("/macros/:id", ctrl.ApiConfigMacroUpdate, ctrl.RequireWrite)
	cfg.DELETE("/macros/:id", ctrl.ApiConfigMacroDelete, ctrl.RequireWrite)
	cfg.GET("/templates", ctrl.ApiConfigTemplateList, ctrl.RequireRead)
	cfg.GET("/templates/:id", ctrl.ApiConfigTemplateGet, ctrl.RequireRead)
	cfg.POST("/templates", ctrl.ApiConfigTemplateCreate, ctrl.RequireWrite)
	cfg.PUT("/templates/:id", ctrl.ApiConfigTemplateUpdate, ctrl.RequireWrite)
	cfg.DELETE("/templates/:id", ctrl.ApiConfigTemplateDelete, ctrl.RequireWrite)
	cfg.POST("/render", ctrl.ApiConfigRender, ctrl.RequireRead)

	opt := api.Group("/optical", ctrl.RequireAPIAuth, ctrl.RequireOpticalEnabled)
	opt.GET("/kind-maps", ctrl.ApiOpticalKindMapList, ctrl.RequireAdmin)
	opt.POST("/kind-maps", ctrl.ApiOpticalKindMapCreate, ctrl.RequireAdmin)
	opt.PUT("/kind-maps/:id", ctrl.ApiOpticalKindMapUpdate, ctrl.RequireAdmin)
	opt.DELETE("/kind-maps/:id", ctrl.ApiOpticalKindMapDelete, ctrl.RequireAdmin)
	opt.GET("/ports/:id", ctrl.ApiOpticalPortGet, ctrl.RequireRead)
	opt.PUT("/ports/:id", ctrl.ApiOpticalPortPut, ctrl.RequireWrite)
	opt.DELETE("/ports/:id", ctrl.ApiOpticalPortDelete, ctrl.RequireWrite)
	opt.GET("/xconnects", ctrl.ApiOpticalXConnectList, ctrl.RequireRead)
	opt.POST("/xconnects", ctrl.ApiOpticalXConnectCreate, ctrl.RequireWrite)
	opt.DELETE("/xconnects/:id", ctrl.ApiOpticalXConnectDelete, ctrl.RequireWrite)
	opt.POST("/trace", ctrl.ApiOpticalTrace, ctrl.RequireRead)
	opt.POST("/retrace-stale", ctrl.ApiOpticalRetraceStale, ctrl.RequireWrite)
	opt.GET("/device/:id/ports", ctrl.ApiDeviceOpticalPorts, ctrl.RequireRead)
	opt.PUT("/device/:id/inventory", ctrl.ApiOpticalDeviceInventoryPut, ctrl.RequireWrite)

	maint := api.Group("/maintenance", ctrl.RequireAPIAuth, ctrl.RequireOpticalEnabled)
	maint.GET("", ctrl.ApiMaintenanceList, ctrl.RequireRead)
	maint.GET("/:id", ctrl.ApiMaintenanceGet, ctrl.RequireRead)
	maint.POST("", ctrl.ApiMaintenanceCreate, ctrl.RequireWrite)
	maint.PUT("/:id", ctrl.ApiMaintenanceUpdate, ctrl.RequireWrite)
	maint.GET("/:id/impact", ctrl.ApiMaintenanceImpact, ctrl.RequireRead)
	maint.POST("/:id/notify", ctrl.ApiMaintenanceNotify, ctrl.RequireWrite)

	api.GET("/contact/:id/customers", ctrl.ApiContactCustomers, ctrl.RequireAPIAuth, ctrl.RequireRead)
	api.PUT("/contact/:id/customers", ctrl.ApiContactCustomersPut, ctrl.RequireAPIAuth, ctrl.RequireWrite)
	api.GET("/customer/:id/contacts", ctrl.ApiCustomerContacts, ctrl.RequireAPIAuth, ctrl.RequireRead)

	// ----- API users -----
	// All five use custom handlers (not the generic CRUD handler): Create/
	// Update hash an incoming password into PasswordHash, List/GetOne
	// preload+flatten the Roles many2many association into UserDTO.RoleIDs -
	// neither can happen through the generic DTO<->JSON<->model round-trip,
	// since both PasswordHash and Roles are json:"-" on the model - and
	// Delete refuses to remove a user holding the "admin" role.
	adminApi.GET("/users", ctrl.ApiUserList)
	adminApi.GET("/users/:id", ctrl.ApiUserGetOne)
	adminApi.PUT("/users/:id", ctrl.ApiUserUpdate)
	adminApi.POST("/users", ctrl.ApiUserCreate)
	adminApi.DELETE("/users/:id", ctrl.ApiUserDelete)

	// ----- API roles -----
	roles_ := NewSecureCRUDHandler[models.Role, models.RoleDTO](DB)
	adminApi.GET("/roles", roles_.GetAll)
	adminApi.GET("/roles/:id", roles_.GetOne)
	adminApi.PUT("/roles/:id", roles_.Update)
	adminApi.POST("/roles", roles_.Create)

	// ----- API worker nodes -----
	// The admin-editable list of remote hosts RemoteManager dials out to
	// (see internal/worker/hub.go) - reconciled against live connections
	// every ~10s, not notified on write, so no extra wiring is needed here
	// beyond the CRUD handlers. GetAll/GetOne use the generic handler
	// (Token is json:"-", so it's never leaked); Create/Update are
	// hand-written - see ApiWorkerNodeCreate's doc comment.
	workerNodes_ := NewSecureCRUDHandler[models.WorkerNode, models.WorkerNodeDTO](DB)
	adminApi.GET("/worker-nodes", workerNodes_.GetAll)
	adminApi.GET("/worker-nodes/:id", workerNodes_.GetOne)
	adminApi.GET("/worker-nodes/:id/token", ctrl.ApiWorkerNodeToken)
	adminApi.PUT("/worker-nodes/:id", ctrl.ApiWorkerNodeUpdate)
	adminApi.POST("/worker-nodes", ctrl.ApiWorkerNodeCreate)

	// ----- API device-sync credentials -----
	// The admin-editable list of per-device login credentials
	// internal/device-sync uses (see web/handle_device_sync.go's
	// ApiDeviceSyncConfig) - same shape as worker nodes above: GetAll/GetOne/
	// Delete use the generic handler (Password is json:"-", so it's never
	// leaked); Create/Update are hand-written.
	deviceSyncAuth_ := NewSecureCRUDHandler[models.DeviceSyncAuth, models.DeviceSyncAuthDTO](DB)
	adminApi.GET("/device-sync-auth", deviceSyncAuth_.GetAll)
	adminApi.GET("/device-sync-auth/:id", deviceSyncAuth_.GetOne)
	adminApi.GET("/device-sync-auth/:id/password", ctrl.ApiDeviceSyncAuthPassword)
	adminApi.PUT("/device-sync-auth/:id", ctrl.ApiDeviceSyncAuthUpdate)
	adminApi.POST("/device-sync-auth", ctrl.ApiDeviceSyncAuthCreate)
	adminApi.DELETE("/device-sync-auth/:id", deviceSyncAuth_.Delete)

	// ----- API settings -----
	adminApi.GET("/settings", ctrl.ApiSettingsGet)
	adminApi.PUT("/settings", ctrl.ApiSettingsUpdate)
	adminApi.POST("/settings/test-email", ctrl.ApiSettingsTestEmail)

	// ----- API: LDAP/AD test connection (Admin -> Authentication page) -----
	adminApi.POST("/ldap/test", ctrl.ApiLdapTestConnection)

	// ----- API: LDAP/AD tree browse (Admin -> Authorization page, group picker) -----
	adminApi.GET("/ldap/browse", ctrl.ApiLdapBrowse)

	// ----- API: LDAP group -> role mappings (Admin -> Authorization page) -----
	ldapRoleMappings_ := NewSecureCRUDHandler[models.LdapRoleMapping, models.LdapRoleMappingDTO](DB)
	adminApi.GET("/ldap-role-mappings", ldapRoleMappings_.GetAll)
	adminApi.GET("/ldap-role-mappings/:id", ldapRoleMappings_.GetOne)
	adminApi.PUT("/ldap-role-mappings/:id", ldapRoleMappings_.Update)
	adminApi.POST("/ldap-role-mappings", ldapRoleMappings_.Create)
	adminApi.DELETE("/ldap-role-mappings/:id", ldapRoleMappings_.Delete)

	// ----- API: dashboard links (Admin -> Settings -> Dashboard) -----
	// GetOne/Update/Delete use the generic handler; List/Create/Reorder are
	// hand-written to maintain Position - see web/handle_links.go.
	links_ := NewSecureCRUDHandler[models.Link, models.LinkDTO](DB)
	adminApi.PUT("/links/reorder", ctrl.ApiLinksReorder)
	adminApi.GET("/links", ctrl.ApiLinksList)
	adminApi.GET("/links/:id", links_.GetOne)
	adminApi.PUT("/links/:id", links_.Update)
	adminApi.POST("/links", ctrl.ApiLinkCreate)
	adminApi.DELETE("/links/:id", links_.Delete)
	// Plain-auth read route so the dashboard (visible to every logged-in
	// user, not just admins) can display the same list.
	api.GET("/links", ctrl.ApiLinksList, ctrl.RequireAPIAuth)

	// ----- API: current user's own profile -----
	api.GET("/me", ctrl.ApiMeGet, ctrl.RequireAPIAuth)
	api.PUT("/me", ctrl.ApiMeUpdate, ctrl.RequireAPIAuth)

	// ----- API: live log stream for the frontend's log window -----
	api.GET("/logs/ws", ctrl.ApiLogsWebSocket, ctrl.RequireAPIAuth, ctrl.RequireAdmin)

	// Without this, an unmatched /api/* request (typo'd endpoint, removed
	// route, ...) would fall through to the "/*" SPA catch-all below and get
	// back a 200 HTML page instead of a 404 - confusing for an API caller
	// expecting JSON.
	api.Any("/*", func(c *echo.Context) error {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "not found"})
	})

	remoteManager.SetAPIHandler(e)
	go remoteManager.Run(context.Background())

	// Start server
	var bind string
	if p.Bind != "" {
		bind = p.Bind
	} else {
		bind = util.Config.Web.Bind
	}
	slog.Info("Server started", "bind-listen", bind)
	err = e.Start(bind)
	if err != http.ErrServerClosed {
		slog.Error("Error closing Server", "err", err)
	}
	return err
}
