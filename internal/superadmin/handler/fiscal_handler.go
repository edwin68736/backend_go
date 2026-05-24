package handler

import (
	"encoding/json"
	"net/url"
	"strings"

	"tukifac/pkg/fiscaladmin"

	"github.com/gofiber/fiber/v3"
)

// FiscalHandler BFF superadmin → facturador_lycet (sin BD fiscal local).
type FiscalHandler struct{}

func NewFiscalHandler() *FiscalHandler {
	return &FiscalHandler{}
}

func (h *FiscalHandler) ensureConfigured(c fiber.Ctx) error {
	if !fiscaladmin.Enabled() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Facturador fiscal no configurado (FACTURADOR_BASE_URL / FACTURADOR_TOKEN)",
		})
	}
	return nil
}

func (h *FiscalHandler) proxyError(c fiber.Ctx, err error, raw json.RawMessage, status int) error {
	if len(raw) > 0 {
		c.Status(status)
		c.Set("Content-Type", "application/json")
		return c.Send(raw)
	}
	if status == 0 {
		status = fiber.StatusBadGateway
	}
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}

func collectQuery(c fiber.Ctx) url.Values {
	q := url.Values{}
	c.Request().URI().QueryArgs().VisitAll(func(k, v []byte) {
		q.Set(string(k), string(v))
	})
	return q
}

// GET /api/superadmin/fiscal/stats
func (h *FiscalHandler) StatsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/stats", collectQuery(c))
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/documents
func (h *FiscalHandler) ListDocumentsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents", collectQuery(c))
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/documents/:uuid
func (h *FiscalHandler) DocumentDetailAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents/"+url.PathEscape(uuid), nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/documents/:uuid/download/:type
func (h *FiscalHandler) DownloadAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	typ := c.Params("type")
	path := "/api/v1/fiscal/documents/" + url.PathEscape(uuid) + "/download/" + url.PathEscape(typ)
	data, ct, status, err := fiscaladmin.Download(path)
	if err != nil {
		if status >= 400 && len(data) > 0 {
			c.Status(status)
			return c.Send(data)
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	if ct != "" {
		c.Set("Content-Type", ct)
	}
	c.Set("Content-Disposition", "attachment")
	return c.Send(data)
}

type bulkBody struct {
	DocumentUUIDs []string               `json:"document_uuids"`
	Filters       map[string]interface{} `json:"filters"`
	Max           int                    `json:"max"`
}

func (h *FiscalHandler) bulkAction(c fiber.Ctx, action string) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	var body bulkBody
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	payload := map[string]interface{}{"max": body.Max}
	if len(body.DocumentUUIDs) > 0 {
		payload["document_uuids"] = body.DocumentUUIDs
	} else if len(body.Filters) > 0 {
		payload["filters"] = body.Filters
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "document_uuids o filters requerido"})
	}
	raw, status, err := fiscaladmin.PostJSON("/api/v1/fiscal/documents/bulk/"+action, payload)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Status(fiber.StatusAccepted)
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// POST /api/superadmin/fiscal/documents/bulk/:action
func (h *FiscalHandler) BulkActionAPI(c fiber.Ctx) error {
	action := strings.TrimSpace(c.Params("action"))
	switch action {
	case "send", "retry", "force", "email", "poll":
		return h.bulkAction(c, action)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "acción bulk no soportada"})
	}
}

// POST /api/superadmin/fiscal/documents/:uuid/:action
func (h *FiscalHandler) DocumentActionAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	action := strings.TrimSpace(c.Params("action"))
	switch action {
	case "send", "retry", "force", "email", "poll", "cancel":
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "acción no soportada"})
	}
	path := "/api/v1/fiscal/documents/" + url.PathEscape(uuid) + "/" + action
	raw, status, err := fiscaladmin.PostJSON(path, nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	if action == "cancel" {
		c.Status(fiber.StatusOK)
	} else {
		c.Status(fiber.StatusAccepted)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/health
func (h *FiscalHandler) HealthAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/health", nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/operations/summary
func (h *FiscalHandler) OperationsSummaryAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/operations/summary", collectQuery(c))
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/operations/tenants
func (h *FiscalHandler) OperationsTenantsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/operations/tenants", collectQuery(c))
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/operations/queue
func (h *FiscalHandler) OperationsQueueAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/operations/queue", nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/alerts
func (h *FiscalHandler) AlertsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/alerts", nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/superadmin/fiscal/documents/:uuid/audit-timeline
func (h *FiscalHandler) AuditTimelineAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents/"+url.PathEscape(uuid)+"/audit-timeline", nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}
