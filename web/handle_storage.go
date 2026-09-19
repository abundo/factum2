package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/storage"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/labstack/echo/v5"
)

func (ctrl *Controller) RequireStorageEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !storage.Enabled(ctrl.DB) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "software management is disabled"})
		}
		return next(c)
	}
}

func (ctrl *Controller) ApiStorageConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	resp := storage.Config{
		CommonConfig: util.NewCommonConfig(settings),
		Root:         settings.StorageRoot,
		HTTPListen:   settings.StorageHTTPListen,
		HTTPURL:      settings.StorageHTTPURL,
		TFTPListen:   settings.StorageTFTPListen,
		TFTPHost:     settings.StorageTFTPHost,
		SFTPListen:   settings.StorageSFTPListen,
		SFTPHost:     settings.StorageSFTPHost,
		SFTPUser:     settings.StorageSFTPUser,
		SFTPPassword: settings.StorageSFTPPassword,
	}
	storage.ApplyConfigDefaults(&resp)
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *Controller) ApiSoftwareList(c *echo.Context) error {
	res, err := ctrl.storageDo(c.Request().Context(), http.MethodGet, "/files?path="+url.QueryEscape(c.QueryParam("path")), nil, nil)
	if err != nil {
		return storageProxyError(c, err)
	}
	return c.Blob(res.Status, headerOr(res.Header, "Content-Type", "application/json"), res.Body)
}

func (ctrl *Controller) ApiSoftwareMkdir(c *echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	res, err := ctrl.storageDo(c.Request().Context(), http.MethodPost, "/mkdir", map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return storageProxyError(c, err)
	}
	return c.Blob(res.Status, headerOr(res.Header, "Content-Type", "application/json"), res.Body)
}

func (ctrl *Controller) ApiSoftwareMove(c *echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	res, err := ctrl.storageDo(c.Request().Context(), http.MethodPost, "/move", map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return storageProxyError(c, err)
	}
	return c.Blob(res.Status, headerOr(res.Header, "Content-Type", "application/json"), res.Body)
}

func (ctrl *Controller) ApiSoftwareDelete(c *echo.Context) error {
	res, err := ctrl.storageDo(c.Request().Context(), http.MethodDelete, "/files?path="+url.QueryEscape(c.QueryParam("path")), nil, nil)
	if err != nil {
		return storageProxyError(c, err)
	}
	if res.Status == http.StatusNoContent {
		return c.NoContent(http.StatusNoContent)
	}
	return c.Blob(res.Status, headerOr(res.Header, "Content-Type", "application/json"), res.Body)
}

func (ctrl *Controller) ApiSoftwareUpload(c *echo.Context) error {
	path := c.QueryParam("path")
	if strings.TrimSpace(path) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "path is required"})
	}
	var r io.Reader
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/") {
		fh, err := c.FormFile("file")
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "multipart field \"file\" is required"})
		}
		f, err := fh.Open()
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		defer f.Close()
		r = f
		if strings.HasSuffix(path, "/") {
			path = strings.TrimRight(path, "/") + "/" + fh.Filename
		}
	} else {
		r = c.Request().Body
	}
	var offset int64
	buf := make([]byte, storage.MaxChunk)
	for {
		n, readErr := io.ReadFull(r, buf)
		if n == 0 {
			if readErr == io.EOF {
				if offset == 0 {
					_, err := ctrl.storageDo(c.Request().Context(), http.MethodPut, "/files?path="+url.QueryEscape(path)+"&offset=0", nil, []byte{})
					if err != nil {
						return storageProxyError(c, err)
					}
				}
				break
			}
			if readErr != nil && readErr != io.ErrUnexpectedEOF {
				return c.JSON(http.StatusBadRequest, map[string]any{"error": readErr.Error()})
			}
		}
		chunk := buf[:n]
		q := "/files?path=" + url.QueryEscape(path) + "&offset=" + strconv.FormatInt(offset, 10)
		res, err := ctrl.storageDo(c.Request().Context(), http.MethodPut, q, nil, chunk)
		if err != nil {
			return storageProxyError(c, err)
		}
		if res.Status >= 300 {
			return c.Blob(res.Status, headerOr(res.Header, "Content-Type", "application/json"), res.Body)
		}
		offset += int64(n)
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": readErr.Error()})
		}
	}
	return c.JSON(http.StatusCreated, map[string]any{"path": path, "size": offset})
}

type softwareCopyBody struct {
	Path        string `json:"path"`
	Device      string `json:"device"`
	Protocol    string `json:"protocol"`
	Destination string `json:"destination"`
}

func (ctrl *Controller) ApiSoftwareCopy(c *echo.Context) error {
	var body softwareCopyBody
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json"})
	}
	proto, err := storage.NormalizeProtocol(body.Protocol)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if strings.TrimSpace(body.Path) == "" || strings.TrimSpace(body.Device) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "path and device are required"})
	}
	if ctrl.RemoteManager == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{"error": "worker hub is not running"})
	}
	args := []string{"copy", "-n", body.Device, "--path", body.Path, "--protocol", proto, "--job"}
	if strings.TrimSpace(body.Destination) != "" {
		args = append(args, "--destination", body.Destination)
	}
	matched, id, err := ctrl.RemoteManager.SendCommand(worker.StorageRole, args)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	if matched == 0 {
		return c.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "no connected worker with role storage (add worker.commands.storage running factum2-storage)",
		})
	}
	return c.JSON(http.StatusAccepted, map[string]any{"id": id, "matched": matched})
}

func (ctrl *Controller) storageDo(ctx context.Context, method, path string, header map[string]string, body []byte) (worker.CallResultMsg, error) {
	if res, ok, err := ctrl.storageLocal(ctx, method, path, header, body); ok {
		return res, err
	}
	if ctrl.RemoteManager == nil {
		return worker.CallResultMsg{}, fmt.Errorf("factum2-storage is not running on this host and no worker hub is available")
	}
	return ctrl.RemoteManager.CallRole(ctx, worker.StorageRole, method, path, header, body)
}

func (ctrl *Controller) storageLocal(ctx context.Context, method, path string, header map[string]string, body []byte) (worker.CallResultMsg, bool, error) {
	socket := storage.StorageSocketPath("")
	if util.Config != nil {
		socket = storage.StorageSocketPath(util.Config.Storage.Socket)
	}
	if socket == "" {
		return worker.CallResultMsg{}, false, nil
	}
	if _, err := os.Stat(socket); err != nil {
		return worker.CallResultMsg{}, false, nil
	}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, "unix", socket)
		},
	}
	client := &http.Client{Transport: tr, Timeout: util.HubRPCTimeout}
	req, err := http.NewRequestWithContext(ctx, method, "http://factum2-storage"+path, bytes.NewReader(body))
	if err != nil {
		return worker.CallResultMsg{}, true, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return worker.CallResultMsg{}, false, nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(workerHubBodyCap())+1))
	if err != nil {
		return worker.CallResultMsg{}, true, err
	}
	hdr := map[string]string{}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		hdr["Content-Type"] = ct
	}
	return worker.CallResultMsg{Status: resp.StatusCode, Header: hdr, Body: raw}, true, nil
}

func workerHubBodyCap() int { return 32 << 20 }

func headerOr(h map[string]string, key, fallback string) string {
	if h != nil {
		if v := h[key]; v != "" {
			return v
		}
	}
	return fallback
}

func storageProxyError(c *echo.Context, err error) error {
	return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
}
