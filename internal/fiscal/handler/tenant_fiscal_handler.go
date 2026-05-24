package handler

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"tukifac/pkg/fiscaladmin"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// TenantFiscalHandler BFF tenant → facturador (tenant_slug forzado desde JWT, cero bleed).
type TenantFiscalHandler struct{}

func NewTenantFiscalHandler() *TenantFiscalHandler {
	return &TenantFiscalHandler{}
}

func (h *TenantFiscalHandler) tenantSlug(c fiber.Ctx) (string, error) {
	claims, ok := c.Locals("tenant_claims").(*middleware.TenantClaims)
	if !ok || claims == nil || strings.TrimSpace(claims.TenantSlug) == "" {
		return "", fiber.NewError(fiber.StatusForbidden, "tenant no identificado")
	}
	return claims.TenantSlug, nil
}

func (h *TenantFiscalHandler) tenantID(c fiber.Ctx) uint {
	claims, ok := c.Locals("tenant_claims").(*middleware.TenantClaims)
	if !ok || claims == nil {
		return 0
	}
	return claims.TenantID
}

func (h *TenantFiscalHandler) ensureConfigured(c fiber.Ctx) error {
	if !fiscaladmin.Enabled() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Facturador fiscal no configurado",
		})
	}
	return nil
}

func (h *TenantFiscalHandler) proxyError(c fiber.Ctx, err error, raw json.RawMessage, status int) error {
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

func (h *TenantFiscalHandler) scopedQuery(c fiber.Ctx) (url.Values, error) {
	slug, err := h.tenantSlug(c)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	c.Request().URI().QueryArgs().VisitAll(func(k, v []byte) {
		key := string(k)
		if key == "tenant_slug" || key == "tenant_id" {
			return
		}
		q.Set(key, string(v))
	})
	q.Set("tenant_slug", slug)
	if tid := h.tenantID(c); tid > 0 {
		q.Set("tenant_id", strconv.FormatUint(uint64(tid), 10))
	}
	return q, nil
}

func (h *TenantFiscalHandler) assertDocumentTenant(c fiber.Ctx, uuid string) bool {
	slug, err := h.tenantSlug(c)
	if err != nil {
		_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
		return false
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents/"+url.PathEscape(uuid), nil)
	if err != nil {
		_ = h.proxyError(c, err, raw, status)
		return false
	}
	var detail struct {
		Document struct {
			TenantSlug string `json:"tenant_slug"`
		} `json:"document"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		_ = c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "respuesta fiscal inválida"})
		return false
	}
	if detail.Document.TenantSlug != slug {
		_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "documento fuera del tenant"})
		return false
	}
	return true
}

// GET /api/fiscal/stats
func (h *TenantFiscalHandler) StatsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	q, err := h.scopedQuery(c)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/stats", q)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/fiscal/documents
func (h *TenantFiscalHandler) ListDocumentsAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	q, err := h.scopedQuery(c)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents", q)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/fiscal/documents/:uuid
func (h *TenantFiscalHandler) DocumentDetailAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	if !h.assertDocumentTenant(c, uuid) {
		return nil
	}
	raw, status, err := fiscaladmin.GetJSON("/api/v1/fiscal/documents/"+url.PathEscape(uuid), nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// GET /api/fiscal/documents/:uuid/download/:type
func (h *TenantFiscalHandler) DownloadAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	if !h.assertDocumentTenant(c, uuid) {
		return nil
	}
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

type tenantBulkBody struct {
	DocumentUUIDs []string               `json:"document_uuids"`
	Filters       map[string]interface{} `json:"filters"`
	Max           int                    `json:"max"`
}

func (h *TenantFiscalHandler) bulkAction(c fiber.Ctx, action string) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	slug, err := h.tenantSlug(c)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
	}
	var body tenantBulkBody
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	payload := map[string]interface{}{"max": body.Max, "tenant_slug": slug}
	if len(body.DocumentUUIDs) > 0 {
		payload["document_uuids"] = body.DocumentUUIDs
	} else {
		filters := body.Filters
		if filters == nil {
			filters = map[string]interface{}{}
		}
		filters["tenant_slug"] = slug
		if tid := h.tenantID(c); tid > 0 {
			filters["tenant_id"] = tid
		}
		payload["filters"] = filters
	}
	raw, status, err := fiscaladmin.PostJSON("/api/v1/fiscal/documents/bulk/"+action, payload)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Status(fiber.StatusAccepted)
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}

// POST /api/fiscal/documents/bulk/:action
func (h *TenantFiscalHandler) BulkActionAPI(c fiber.Ctx) error {
	action := strings.TrimSpace(c.Params("action"))
	switch action {
	case "send", "retry", "force", "email", "poll":
		return h.bulkAction(c, action)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "acción bulk no soportada"})
	}
}

// POST /api/fiscal/documents/:uuid/:action
func (h *TenantFiscalHandler) DocumentActionAPI(c fiber.Ctx) error {
	if err := h.ensureConfigured(c); err != nil {
		return err
	}
	uuid := c.Params("uuid")
	if !h.assertDocumentTenant(c, uuid) {
		return nil
	}
	action := strings.TrimSpace(c.Params("action"))
	switch action {
	case "send", "retry", "force", "email", "poll":
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "acción no soportada"})
	}
	path := "/api/v1/fiscal/documents/" + url.PathEscape(uuid) + "/" + action
	raw, status, err := fiscaladmin.PostJSON(path, nil)
	if err != nil {
		return h.proxyError(c, err, raw, status)
	}
	c.Status(fiber.StatusAccepted)
	c.Set("Content-Type", "application/json")
	return c.Send(raw)
}
