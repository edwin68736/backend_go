package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"tukifac/config"
	companysvc "tukifac/internal/company/service"
	"tukifac/internal/superadmin/service"
	"tukifac/pkg/database"
	"tukifac/pkg/facturador"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type TenantHandler struct {
	svc *service.TenantService
}

func NewTenantHandler() *TenantHandler {
	return &TenantHandler{svc: service.NewTenantService()}
}

// GET /api/superadmin/tenants?q=&status=&region_id=&provincia_id=
func (h *TenantHandler) ListAPI(c fiber.Ctx) error {
	tenants, err := h.svc.List(c.Query("q"), c.Query("status"), c.Query("region_id"), c.Query("provincia_id"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// Incluir billing_enabled por tenant para la columna Modo SUNAT
	billingByTenant, _ := h.svc.BillingEnabledByTenantIDs(tenantIDs(tenants))
	out := make([]fiber.Map, 0, len(tenants))
	for _, t := range tenants {
		m := enrichTenantMap(&t)
		m["billing_enabled"] = billingByTenant[t.ID]
		out = append(out, m)
	}
	return c.JSON(fiber.Map{"data": out})
}

func tenantIDs(tenants []database.Tenant) []uint {
	ids := make([]uint, 0, len(tenants))
	for _, t := range tenants {
		ids = append(ids, t.ID)
	}
	return ids
}

// GET /api/superadmin/tenants/:id
func (h *TenantHandler) GetAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	tenant, err := h.svc.GetByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	modules, _ := h.svc.GetModules(uint(id))
	return c.JSON(fiber.Map{"data": tenant, "modules": modules})
}

// POST /api/superadmin/tenants
func (h *TenantHandler) CreateAPI(c fiber.Ctx) error {
	var input service.CreateTenantInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	tenant, err := h.svc.Create(input)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    enrichTenantMap(tenant),
	})
}

// PUT /api/superadmin/tenants/:id
func (h *TenantHandler) UpdateAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var input database.Tenant
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := h.svc.Update(uint(id), input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// PATCH /api/superadmin/tenants/:id/status
func (h *TenantHandler) ToggleStatusAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	status := strings.TrimSpace(body.Status)
	if status == "active" || status == "inactive" {
		billingByTenant, _ := h.svc.BillingEnabledByTenantIDs([]uint{uint(id)})
		if billingByTenant[uint(id)] {
			tenantDB, derr := h.getTenantDBByID(uint(id))
			if derr != nil {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
			}
			cfg, cerr := companysvc.NewCompanyService(tenantDB).GetConfig()
			if cerr == nil && cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.InvoicingMode), "pse") {
				ruc := strings.TrimSpace(cfg.RUC)
				if ruc != "" {
					client := newValidaPSEClient()
					if strings.TrimSpace(client.token) == "" {
						return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
					}
					empresaID, ferr := validaPSEFindEmpresaIDByRUC(client, ruc)
					if ferr != nil {
						return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": ferr.Error()})
					}
					detail, gerr := validaPSEGetEmpresa(client, empresaID)
					if gerr != nil {
						return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": gerr.Error()})
					}
					desired := "ACTIVO"
					if status == "inactive" {
						desired = "INACTIVO"
					}
					if strings.ToUpper(strings.TrimSpace(detail.Estado)) != desired {
						if err := validaPSEToggleEmpresa(client, empresaID); err != nil {
							return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
						}
					}
				}
			}
		}
	}

	if err := h.svc.SetStatus(uint(id), body.Status); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// GET /api/superadmin/tenants/:id/modules
func (h *TenantHandler) GetModulesAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	modules, err := h.svc.GetModules(uint(id))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": modules})
}

// POST /api/superadmin/tenants/:id/modules
func (h *TenantHandler) SetModuleAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		ModuleKey string `json:"module_key"`
		Enabled   bool   `json:"enabled"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := h.svc.SetModule(uint(id), body.ModuleKey, body.Enabled); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/tenants/:id/migrate — ejecuta migraciones del tenant (columnas/tablas nuevas).
func (h *TenantHandler) MigrateAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	if err := h.svc.RunMigrations(uint(id)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Migraciones ejecutadas"})
}

// POST /api/superadmin/tenants/migrate-all — ejecuta migraciones para todos los tenants activos.
func (h *TenantHandler) MigrateAllAPI(c fiber.Ctx) error {
	summary, err := h.svc.RunMigrationsAll()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	resp := fiber.Map{
		"success":  len(summary.Failed) == 0,
		"migrated": len(summary.Success),
		"failed":   len(summary.Failed),
	}
	if len(summary.Failed) > 0 {
		failed := make([]string, 0, len(summary.Failed))
		for _, f := range summary.Failed {
			failed = append(failed, f.Slug)
		}
		resp["failed_tenants"] = failed
	}
	return c.JSON(resp)
}

// getTenantDBByID obtiene el *gorm.DB del tenant por su ID (para uso en sunat/sync).
func (h *TenantHandler) getTenantDBByID(id uint) (*gorm.DB, error) {
	tenant, err := h.svc.GetByID(id)
	if err != nil {
		return nil, err
	}
	return database.GetTenantDB(tenant.DBName)
}

// mapEnvToLycetAmbiente mapea sunat_env_mode interno (production/beta/demo) al ambiente del facturador (produccion/pruebas).
func mapEnvToLycetAmbiente(sunatEnvMode string) string {
	if sunatEnvMode == "production" {
		return "produccion"
	}
	return "pruebas"
}

// GET /api/superadmin/tenants/:id/sunat-config — configuración SUNAT del tenant (desde su BD).
func (h *TenantHandler) GetSunatConfigAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	tenantDB, err := h.getTenantDBByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	cfg, err := companysvc.NewCompanyService(tenantDB).GetConfig()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// Sincronizar sunat_env_mode en central para que el listado lo muestre (backfill)
	if cfg.SunatEnvMode != "" {
		database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", cfg.SunatEnvMode)
	}
	pseProvider := ""
	if strings.TrimSpace(cfg.PSEConfigJSON) != "" {
		var v struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal([]byte(cfg.PSEConfigJSON), &v); err == nil {
			pseProvider = strings.TrimSpace(v.Provider)
		}
	}
	mode := strings.TrimSpace(cfg.InvoicingMode)
	if mode == "" {
		mode = "legacy_backend"
	}
	return c.JSON(fiber.Map{
		"sunat_enabled":        cfg.SunatEnabled,
		"sunat_env_mode":       cfg.SunatEnvMode,
		"sunat_sol_user":       cfg.SunatSOLUser,
		"tax_rate":             cfg.TaxRate,
		"igv_regime":           cfg.IgvRegime,
		"tax_benefit_zone":     cfg.TaxBenefitZone,
		"ruc":                  cfg.RUC,
		"business_name":        cfg.BusinessName,
		"invoicing_mode":       mode,
		"pse_provider":         pseProvider,
		"pse_base_url":         strings.TrimSpace(cfg.PSEBaseURL),
		"pse_token_configured": strings.TrimSpace(cfg.PSEToken) != "",
	})
}

// PUT /api/superadmin/tenants/:id/sunat-config — actualiza configuración SUNAT del tenant.
func (h *TenantHandler) UpdateSunatConfigAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	tenantDB, err := h.getTenantDBByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	var body struct {
		SunatEnabled   bool    `json:"sunat_enabled"`
		SunatSolUser   string  `json:"sunat_sol_user"`
		SunatSolPass   string  `json:"sunat_sol_pass"`
		Certificate    string  `json:"certificate"`
		SunatEnvMode   string  `json:"sunat_env_mode"`
		TukifacToken   string  `json:"tukifac_token"`
		TaxRate        float64 `json:"tax_rate"`
		IgvRegime      string  `json:"igv_regime"`
		TaxBenefitZone bool    `json:"tax_benefit_zone"`
		InvoicingMode  string  `json:"invoicing_mode"`
		PSEProvider    string  `json:"pse_provider"`
		PSEBaseURL     string  `json:"pse_base_url"`
		PSEToken       string  `json:"pse_token"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	svc := companysvc.NewCompanyService(tenantDB)
	if err := svc.SaveSunatConfig(
		body.SunatEnabled, body.SunatSolUser, body.SunatSolPass,
		body.Certificate, body.SunatEnvMode, body.TukifacToken,
		body.TaxRate, body.IgvRegime, body.TaxBenefitZone,
	); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if strings.TrimSpace(body.InvoicingMode) != "" ||
		strings.TrimSpace(body.PSEProvider) != "" ||
		strings.TrimSpace(body.PSEBaseURL) != "" ||
		strings.TrimSpace(body.PSEToken) != "" {
		if err := svc.SaveInvoicingConfigCentral(body.InvoicingMode, body.PSEBaseURL, body.PSEToken, body.PSEProvider); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
	}
	// Sincronizar sunat_env_mode en la tabla central de tenants para mostrarlo en el listado
	if body.SunatEnvMode != "" {
		database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", body.SunatEnvMode)
	}
	if cfg, _ := svc.GetConfig(); cfg != nil && cfg.RUC != "" {
		mode := strings.TrimSpace(cfg.InvoicingMode)
		if mode == "" {
			mode = "legacy_backend"
		}
		if mode != "pse" && config.AppConfig.FacturadorBaseURL != "" && config.AppConfig.FacturadorToken != "" {
			if syncErr := svc.SyncFacturadorConfigWithFiles("", "", "", "", ""); syncErr == nil {
				database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_connected_at", time.Now())
			}
			_ = facturador.NewClient(config.AppConfig.FacturadorBaseURL, config.AppConfig.FacturadorToken).PatchAmbiente(cfg.RUC, mapEnvToLycetAmbiente(body.SunatEnvMode))
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

// PATCH /api/superadmin/tenants/:id/sunat-env — cambia modo pruebas/producción del tenant.
func (h *TenantHandler) PatchSunatEnvAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		SunatEnvMode string `json:"sunat_env_mode"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if body.SunatEnvMode != "beta" && body.SunatEnvMode != "demo" && body.SunatEnvMode != "production" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "sunat_env_mode debe ser beta, demo o production"})
	}
	tenantDB, err := h.getTenantDBByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	svc := companysvc.NewCompanyService(tenantDB)
	cfg, err := svc.GetConfig()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	prevMode := strings.TrimSpace(cfg.SunatEnvMode)
	if err := svc.SaveSunatConfig(
		cfg.SunatEnabled, cfg.SunatSOLUser, "", "", body.SunatEnvMode, "",
		float64(cfg.TaxRate), cfg.IgvRegime, cfg.TaxBenefitZone,
	); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", body.SunatEnvMode)
	// Actualizar ambiente en Lycet (PATCH /api/v1/empresas/{ruc}/ambiente)
	if cfg, _ := svc.GetConfig(); cfg != nil && cfg.RUC != "" && config.AppConfig.FacturadorBaseURL != "" && config.AppConfig.FacturadorToken != "" {
		_ = facturador.NewClient(config.AppConfig.FacturadorBaseURL, config.AppConfig.FacturadorToken).PatchAmbiente(cfg.RUC, mapEnvToLycetAmbiente(body.SunatEnvMode))
	}

	{
		cfg2, _ := svc.GetConfig()
		if cfg2 != nil && strings.EqualFold(strings.TrimSpace(cfg2.InvoicingMode), "pse") {
			ruc := strings.TrimSpace(cfg2.RUC)
			if ruc == "" {
				_ = svc.SaveSunatConfig(
					cfg2.SunatEnabled, cfg2.SunatSOLUser, "", "", prevMode, "",
					float64(cfg2.TaxRate), cfg2.IgvRegime, cfg2.TaxBenefitZone,
				)
				_ = database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", prevMode).Error
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El tenant no tiene RUC configurado"})
			}
			client := newValidaPSEClient()
			if strings.TrimSpace(client.token) == "" {
				_ = svc.SaveSunatConfig(
					cfg2.SunatEnabled, cfg2.SunatSOLUser, "", "", prevMode, "",
					float64(cfg2.TaxRate), cfg2.IgvRegime, cfg2.TaxBenefitZone,
				)
				_ = database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", prevMode).Error
				return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
			}
			empresaID, ferr := validaPSEFindEmpresaIDByRUC(client, ruc)
			if ferr != nil {
				_ = svc.SaveSunatConfig(
					cfg2.SunatEnabled, cfg2.SunatSOLUser, "", "", prevMode, "",
					float64(cfg2.TaxRate), cfg2.IgvRegime, cfg2.TaxBenefitZone,
				)
				_ = database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", prevMode).Error
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": ferr.Error()})
			}
			servidor := validaPSEServidorFromEnvMode(body.SunatEnvMode)
			if uerr := validaPSEUpdateEmpresa(client, empresaID, map[string]interface{}{"servidor": servidor}); uerr != nil {
				_ = svc.SaveSunatConfig(
					cfg2.SunatEnabled, cfg2.SunatSOLUser, "", "", prevMode, "",
					float64(cfg2.TaxRate), cfg2.IgvRegime, cfg2.TaxBenefitZone,
				)
				_ = database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", prevMode).Error
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": uerr.Error()})
			}
		}
	}

	return c.JSON(fiber.Map{"success": true, "sunat_env_mode": body.SunatEnvMode})
}

// POST /api/superadmin/tenants/:id/sync-facturador — sincroniza configuración del tenant con el facturador (Lycet).
func (h *TenantHandler) SyncFacturadorAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	tenantDB, err := h.getTenantDBByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	var body struct {
		CertificateBase64 string `json:"certificate_base64"`
		PrivateKeyBase64  string `json:"private_key_base64"`
		LogoBase64        string `json:"logo_base64"`
		SolUser           string `json:"sol_user"`
		SolPass           string `json:"sol_pass"`
	}
	_ = c.Bind().JSON(&body)
	svc := companysvc.NewCompanyService(tenantDB)
	hasFiles := body.CertificateBase64 != "" || body.PrivateKeyBase64 != "" || body.LogoBase64 != ""
	if hasFiles {
		if err := svc.SyncFacturadorConfigWithFiles(body.CertificateBase64, body.PrivateKeyBase64, body.LogoBase64, body.SolUser, body.SolPass); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
	} else {
		if err := svc.SyncFacturadorConfigWithFiles("", "", "", body.SolUser, body.SolPass); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
	}
	now := time.Now()
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_connected_at", now)
	return c.JSON(fiber.Map{"success": true, "message": "Configuración sincronizada con el facturador"})
}

// GET /api/superadmin/tenants/conectados-sunat — lista empresas registradas en Lycet (GET /api/v1/empresas).
// La fuente es el facturador Lycet; se cruza con la BD central por RUC para mostrar tenant (nombre, slug, id) y última sincronización si existe.
func (h *TenantHandler) ListConectadosSunatAPI(c fiber.Ctx) error {
	if config.AppConfig.FacturadorBaseURL == "" || config.AppConfig.FacturadorToken == "" {
		return c.JSON(fiber.Map{"data": []interface{}{}})
	}
	client := facturador.NewClient(config.AppConfig.FacturadorBaseURL, config.AppConfig.FacturadorToken)
	empresasLycet, err := client.ListEmpresas()
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "no se pudo obtener la lista del facturador: " + err.Error(), "data": []interface{}{}})
	}
	// RUCs en Lycet pueden venir como keys del mapa (map[RUC]Entry) o desde array; ListEmpresas ya normaliza a mapa por RUC.
	// Obtener todos los tenants de la BD central con RUC para cruzar
	var tenantsByRuc map[string]database.Tenant
	{
		var tenants []database.Tenant
		if err := database.CentralDB.Where("ruc != '' AND ruc IS NOT NULL").Find(&tenants).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		tenantsByRuc = make(map[string]database.Tenant)
		for _, t := range tenants {
			tenantsByRuc[t.RUC] = t
		}
	}
	type item struct {
		ID               uint       `json:"id"` // 0 si no hay tenant en tukifac con este RUC
		Name             string     `json:"name"`
		Slug             string     `json:"slug"`
		RUC              string     `json:"ruc"`
		SunatConnectedAt *time.Time `json:"sunat_connected_at"`
		EnLycet          bool       `json:"en_lycet"` // siempre true en esta lista (viene de Lycet)
		AmbienteLycet    string     `json:"ambiente_lycet,omitempty"`
	}
	out := make([]item, 0, len(empresasLycet))
	for ruc, entry := range empresasLycet {
		if ruc == "" {
			continue
		}
		t, hasTenant := tenantsByRuc[ruc]
		ambiente := entry.Ambiente
		if ambiente == "" {
			ambiente = "pruebas"
		}
		if hasTenant {
			out = append(out, item{
				ID:               t.ID,
				Name:             t.Name,
				Slug:             t.Slug,
				RUC:              ruc,
				SunatConnectedAt: t.SunatConnectedAt,
				EnLycet:          true,
				AmbienteLycet:    ambiente,
			})
		} else {
			out = append(out, item{
				ID:               0,
				Name:             "",
				Slug:             "",
				RUC:              ruc,
				SunatConnectedAt: nil,
				EnLycet:          true,
				AmbienteLycet:    ambiente,
			})
		}
	}
	// Ordenar por RUC para orden estable
	sort.Slice(out, func(i, j int) bool { return out[i].RUC < out[j].RUC })
	return c.JSON(fiber.Map{"data": out})
}

type validaPSEManagementEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type validaPSEEmpresa struct {
	ID              int    `json:"id"`
	RUC             string `json:"ruc"`
	RazonSocial     string `json:"razon_social"`
	Servidor        string `json:"servidor"`
	FechaInicio     string `json:"fecha_inicio"`
	FechaFin        string `json:"fecha_fin"`
	SinLimiteFecha  bool   `json:"sin_limite_fecha"`
	Estado          string `json:"estado"`
	FirmasUsadas    int    `json:"firmas_usadas"`
	CredencialesCPE struct {
		UsuarioSecundaria string `json:"usuario_secundaria"`
		TokenAcceso       string `json:"token_acceso"`
	} `json:"credenciales_cpe"`
	OSE struct {
		Activo bool `json:"activo"`
	} `json:"ose"`
}

type validaPSEEmpresasList struct {
	Data []validaPSEEmpresa `json:"data"`
	Meta json.RawMessage    `json:"meta"`
}

type validaPSEClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newValidaPSEClient() *validaPSEClient {
	base := strings.TrimSpace(config.AppConfig.ValidaPSEManagementBaseURL)
	if base == "" {
		base = "https://app.validapse.com/api"
	}
	return &validaPSEClient{
		baseURL: strings.TrimRight(base, "/"),
		token:   strings.TrimSpace(config.AppConfig.ValidaPSEManagementToken),
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *validaPSEClient) do(method, path string, q url.Values, body io.Reader) (*validaPSEManagementEnvelope, int, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, 0, fmt.Errorf("ValidaPSE no configurado: define VALIDAPSE_MGMT_TOKEN")
	}
	u := c.baseURL + path
	if q != nil && len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return nil, resp.StatusCode, fmt.Errorf("ValidaPSE respondió %d: %s", resp.StatusCode, msg)
	}

	var env validaPSEManagementEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, resp.StatusCode, err
	}
	return &env, resp.StatusCode, nil
}

func parseValidaPSEEmpresasList(raw json.RawMessage) ([]validaPSEEmpresa, interface{}, error) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return []validaPSEEmpresa{}, nil, nil
	}

	if b[0] == '[' {
		var items []validaPSEEmpresa
		if err := json.Unmarshal(b, &items); err != nil {
			return nil, nil, err
		}
		return items, nil, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, nil, err
	}

	if dataRaw, ok := obj["data"]; ok {
		d := bytes.TrimSpace(dataRaw)
		if len(d) > 0 && d[0] == '[' {
			var items []validaPSEEmpresa
			if err := json.Unmarshal(d, &items); err != nil {
				return nil, nil, err
			}
			var meta interface{}
			if metaRaw, ok := obj["meta"]; ok && len(bytes.TrimSpace(metaRaw)) > 0 {
				_ = json.Unmarshal(metaRaw, &meta)
			}
			return items, meta, nil
		}

		var inner map[string]json.RawMessage
		if err := json.Unmarshal(d, &inner); err == nil {
			if innerData, ok := inner["data"]; ok && len(bytes.TrimSpace(innerData)) > 0 {
				var items []validaPSEEmpresa
				if err := json.Unmarshal(innerData, &items); err != nil {
					return nil, nil, err
				}
				var meta interface{}
				if metaRaw, ok := inner["meta"]; ok && len(bytes.TrimSpace(metaRaw)) > 0 {
					_ = json.Unmarshal(metaRaw, &meta)
				} else {
					var metaObj map[string]interface{}
					if err := json.Unmarshal(d, &metaObj); err == nil {
						delete(metaObj, "data")
						meta = metaObj
					}
				}
				return items, meta, nil
			}
		}
	}

	list := validaPSEEmpresasList{}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, nil, err
	}
	var meta interface{}
	if len(list.Meta) > 0 {
		_ = json.Unmarshal(list.Meta, &meta)
	}
	return list.Data, meta, nil
}

func validaPSEServidorFromEnvMode(envMode string) string {
	m := strings.ToLower(strings.TrimSpace(envMode))
	if m == "production" {
		return "PRODUCCION"
	}
	return "DEMO"
}

func validaPSETenantFechaFin(tenantID uint) string {
	var sub database.SaasSubscription
	if err := database.CentralDB.
		Where("tenant_id = ? AND status IN ('active','trial')", tenantID).
		Order("created_at desc").
		First(&sub).Error; err != nil {
		return ""
	}
	if sub.EndDate.IsZero() {
		return ""
	}
	return sub.EndDate.Format("2006-01-02")
}

func validaPSEFindEmpresaIDByRUC(client *validaPSEClient, ruc string) (int, error) {
	ruc = strings.TrimSpace(ruc)
	if ruc == "" {
		return 0, fmt.Errorf("RUC inválido")
	}
	q := url.Values{}
	q.Set("per_page", "100")
	q.Set("search", ruc)
	env, _, err := client.do(http.MethodGet, "/empresas", q, nil)
	if err != nil {
		return 0, err
	}
	items, _, perr := parseValidaPSEEmpresasList(env.Data)
	if perr != nil {
		return 0, fmt.Errorf("respuesta inválida de ValidaPSE")
	}
	for _, it := range items {
		if strings.TrimSpace(it.RUC) == ruc {
			if it.ID != 0 {
				return it.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("No existe una empresa en el PSE para este RUC")
}

func validaPSEGetEmpresa(client *validaPSEClient, empresaID int) (*validaPSEEmpresa, error) {
	if empresaID == 0 {
		return nil, fmt.Errorf("ID inválido")
	}
	env, _, err := client.do(http.MethodGet, fmt.Sprintf("/empresas/%d", empresaID), nil, nil)
	if err != nil {
		return nil, err
	}
	var detail validaPSEEmpresa
	if err := json.Unmarshal(env.Data, &detail); err != nil {
		return nil, fmt.Errorf("respuesta inválida de ValidaPSE")
	}
	return &detail, nil
}

func validaPSEUpdateEmpresa(client *validaPSEClient, empresaID int, body map[string]interface{}) error {
	if empresaID == 0 {
		return fmt.Errorf("ID inválido")
	}
	b, _ := json.Marshal(body)
	_, _, err := client.do(http.MethodPut, fmt.Sprintf("/empresas/%d", empresaID), nil, bytes.NewBuffer(b))
	return err
}

func validaPSEToggleEmpresa(client *validaPSEClient, empresaID int) error {
	if empresaID == 0 {
		return fmt.Errorf("ID inválido")
	}
	_, _, err := client.do(http.MethodPatch, fmt.Sprintf("/empresas/%d/toggle", empresaID), nil, nil)
	return err
}

func (h *TenantHandler) ListPSEEmpresasAPI(c fiber.Ctx) error {
	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)",
			"data":  []interface{}{},
		})
	}

	q := url.Values{}
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		q.Set("page", v)
	}
	if v := strings.TrimSpace(c.Query("per_page")); v != "" {
		q.Set("per_page", v)
	}
	if v := strings.TrimSpace(c.Query("search")); v != "" {
		q.Set("search", v)
	}
	if v := strings.TrimSpace(c.Query("estado")); v != "" {
		q.Set("estado", v)
	}
	if v := strings.TrimSpace(c.Query("servidor")); v != "" {
		q.Set("servidor", v)
	}

	env, _, err := client.do(http.MethodGet, "/empresas", q, nil)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error(), "data": []interface{}{}})
	}

	items, meta, err := parseValidaPSEEmpresasList(env.Data)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "respuesta inválida de ValidaPSE", "data": []interface{}{}})
	}

	rucs := make([]string, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.RUC) != "" {
			rucs = append(rucs, strings.TrimSpace(it.RUC))
		}
	}

	tenantsByRuc := map[string]database.Tenant{}
	if len(rucs) > 0 {
		var tenants []database.Tenant
		if err := database.CentralDB.Where("ruc IN ?", rucs).Find(&tenants).Error; err == nil {
			for _, t := range tenants {
				if strings.TrimSpace(t.RUC) != "" {
					tenantsByRuc[strings.TrimSpace(t.RUC)] = t
				}
			}
		}
	}

	out := make([]fiber.Map, 0, len(items))
	for _, it := range items {
		m := fiber.Map{
			"id":               it.ID,
			"ruc":              it.RUC,
			"razon_social":     it.RazonSocial,
			"servidor":         it.Servidor,
			"estado":           it.Estado,
			"fecha_inicio":     it.FechaInicio,
			"fecha_fin":        it.FechaFin,
			"sin_limite_fecha": it.SinLimiteFecha,
			"firmas_usadas":    it.FirmasUsadas,
			"ose_activo":       it.OSE.Activo,
		}
		if t, ok := tenantsByRuc[strings.TrimSpace(it.RUC)]; ok {
			m["tenant"] = fiber.Map{
				"id":   t.ID,
				"name": t.Name,
				"slug": t.Slug,
			}
		} else {
			m["tenant"] = nil
		}
		out = append(out, m)
	}

	resp := fiber.Map{"data": out}
	if meta != nil {
		resp["meta"] = meta
	}
	return c.JSON(resp)
}

func (h *TenantHandler) GetPSEEmpresaAPI(c fiber.Ctx) error {
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
	}
	env, _, err := client.do(http.MethodGet, "/empresas/"+id, nil, nil)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	var it validaPSEEmpresa
	if err := json.Unmarshal(env.Data, &it); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "respuesta inválida de ValidaPSE"})
	}

	var tenant database.Tenant
	hasTenant := false
	ruc := strings.TrimSpace(it.RUC)
	if ruc != "" {
		if err := database.CentralDB.Where("ruc = ?", ruc).First(&tenant).Error; err == nil {
			hasTenant = true
		}
	}

	out := fiber.Map{
		"id":               it.ID,
		"ruc":              it.RUC,
		"razon_social":     it.RazonSocial,
		"servidor":         it.Servidor,
		"estado":           it.Estado,
		"fecha_inicio":     it.FechaInicio,
		"fecha_fin":        it.FechaFin,
		"sin_limite_fecha": it.SinLimiteFecha,
		"firmas_usadas":    it.FirmasUsadas,
		"ose_activo":       it.OSE.Activo,
	}
	if hasTenant {
		out["tenant"] = fiber.Map{"id": tenant.ID, "name": tenant.Name, "slug": tenant.Slug}
	} else {
		out["tenant"] = nil
	}
	return c.JSON(out)
}

func validaPSEDefaultCPEBaseURL(mgmtBaseURL string) string {
	u := strings.TrimRight(strings.TrimSpace(mgmtBaseURL), "/")
	if strings.HasSuffix(u, "/api") {
		u = strings.TrimSuffix(u, "/api")
	}
	return u
}

func (h *TenantHandler) CreatePSEEmpresaAPI(c fiber.Ctx) error {
	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
	}
	var body map[string]interface{}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	b, _ := json.Marshal(body)
	env, _, err := client.do(http.MethodPost, "/empresas", nil, bytes.NewBuffer(b))
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	var it validaPSEEmpresa
	_ = json.Unmarshal(env.Data, &it)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": env.Success,
		"message": env.Message,
		"data": fiber.Map{
			"id":                       it.ID,
			"ruc":                      it.RUC,
			"razon_social":             it.RazonSocial,
			"servidor":                 it.Servidor,
			"estado":                   it.Estado,
			"fecha_inicio":             it.FechaInicio,
			"fecha_fin":                it.FechaFin,
			"sin_limite_fecha":         it.SinLimiteFecha,
			"firmas_usadas":            it.FirmasUsadas,
			"ose_activo":               it.OSE.Activo,
			"token_acceso_configurado": strings.TrimSpace(it.CredencialesCPE.TokenAcceso) != "",
		},
	})
}

func (h *TenantHandler) UpdatePSEEmpresaAPI(c fiber.Ctx) error {
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
	}
	var body map[string]interface{}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	b, _ := json.Marshal(body)
	env, _, err := client.do(http.MethodPut, "/empresas/"+id, nil, bytes.NewBuffer(b))
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	var it validaPSEEmpresa
	_ = json.Unmarshal(env.Data, &it)
	return c.JSON(fiber.Map{
		"success": env.Success,
		"message": env.Message,
		"data": fiber.Map{
			"id":                       it.ID,
			"ruc":                      it.RUC,
			"razon_social":             it.RazonSocial,
			"servidor":                 it.Servidor,
			"estado":                   it.Estado,
			"fecha_inicio":             it.FechaInicio,
			"fecha_fin":                it.FechaFin,
			"sin_limite_fecha":         it.SinLimiteFecha,
			"firmas_usadas":            it.FirmasUsadas,
			"ose_activo":               it.OSE.Activo,
			"token_acceso_configurado": strings.TrimSpace(it.CredencialesCPE.TokenAcceso) != "",
		},
	})
}

func (h *TenantHandler) TogglePSEEmpresaAPI(c fiber.Ctx) error {
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
	}
	env, _, err := client.do(http.MethodPatch, "/empresas/"+id+"/toggle", nil, nil)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	var it validaPSEEmpresa
	_ = json.Unmarshal(env.Data, &it)
	return c.JSON(fiber.Map{
		"success": env.Success,
		"message": env.Message,
		"data": fiber.Map{
			"id":                       it.ID,
			"ruc":                      it.RUC,
			"razon_social":             it.RazonSocial,
			"servidor":                 it.Servidor,
			"estado":                   it.Estado,
			"fecha_inicio":             it.FechaInicio,
			"fecha_fin":                it.FechaFin,
			"sin_limite_fecha":         it.SinLimiteFecha,
			"firmas_usadas":            it.FirmasUsadas,
			"ose_activo":               it.OSE.Activo,
			"token_acceso_configurado": strings.TrimSpace(it.CredencialesCPE.TokenAcceso) != "",
		},
	})
}

func (h *TenantHandler) SyncTenantPSECredentialsAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}

	billingByTenant, _ := h.svc.BillingEnabledByTenantIDs([]uint{uint(id)})
	if !billingByTenant[uint(id)] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El tenant no tiene el módulo billing habilitado"})
	}

	tenantDB, err := h.getTenantDBByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tenant no encontrado"})
	}
	svc := companysvc.NewCompanyService(tenantDB)
	cfg, err := svc.GetConfig()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	ruc := strings.TrimSpace(cfg.RUC)
	if ruc == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El tenant no tiene RUC configurado"})
	}

	client := newValidaPSEClient()
	if strings.TrimSpace(client.token) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "ValidaPSE no configurado en el servidor (falta VALIDAPSE_MGMT_TOKEN)"})
	}

	empresaID, ferr := validaPSEFindEmpresaIDByRUC(client, ruc)
	if ferr != nil {
		msg := ferr.Error()
		if strings.Contains(strings.ToLower(msg), "no existe una empresa") {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": msg})
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": msg})
	}

	detail, derr := validaPSEGetEmpresa(client, empresaID)
	if derr != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": derr.Error()})
	}

	tokenAcceso := strings.TrimSpace(detail.CredencialesCPE.TokenAcceso)
	if tokenAcceso == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La empresa existe en el PSE, pero no tiene token_acceso configurado"})
	}

	cpeBaseURL := strings.TrimSpace(cfg.PSEBaseURL)
	if cpeBaseURL == "" {
		cpeBaseURL = validaPSEDefaultCPEBaseURL(config.AppConfig.ValidaPSEManagementBaseURL)
	}

	servidor := strings.ToUpper(strings.TrimSpace(detail.Servidor))
	envMode := "production"
	if strings.Contains(servidor, "DEMO") {
		envMode = "demo"
	}

	if err := svc.SaveInvoicingConfigCentral("pse", cpeBaseURL, tokenAcceso, "validapse"); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	_ = tenantDB.Model(&database.TenantCompanyConfig{}).Where("id = ?", cfg.ID).Update("sunat_env_mode", envMode).Error
	_ = database.CentralDB.Model(&database.Tenant{}).Where("id = ?", id).Update("sunat_env_mode", envMode).Error

	{
		cfg2, _ := svc.GetConfig()
		m := map[string]interface{}{"provider": "validapse", "empresa_id": detail.ID, "servidor": detail.Servidor}
		if strings.TrimSpace(detail.CredencialesCPE.UsuarioSecundaria) != "" {
			m["usuario_secundaria"] = strings.TrimSpace(detail.CredencialesCPE.UsuarioSecundaria)
		}
		b, _ := json.Marshal(m)
		_ = tenantDB.Model(&database.TenantCompanyConfig{}).Where("id = ?", cfg2.ID).Update("pse_config_json", string(b)).Error
	}

	{
		razonSocial := strings.TrimSpace(cfg.BusinessName)
		if razonSocial == "" {
			var t database.Tenant
			_ = database.CentralDB.First(&t, uint(id)).Error
			razonSocial = strings.TrimSpace(t.Name)
		}
		body := map[string]interface{}{}
		if razonSocial != "" {
			body["razon_social"] = razonSocial
		}
		if fechaFin := validaPSETenantFechaFin(uint(id)); fechaFin != "" {
			body["fecha_fin"] = fechaFin
		}
		if len(body) > 0 {
			if uerr := validaPSEUpdateEmpresa(client, empresaID, body); uerr != nil {
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": uerr.Error()})
			}
		}
	}

	return c.JSON(fiber.Map{
		"success":              true,
		"empresa_id":           empresaID,
		"servidor":             detail.Servidor,
		"sunat_env_mode":       envMode,
		"pse_token_configured": true,
		"pse_base_url":         cpeBaseURL,
		"invoicing_mode":       "pse",
		"pse_provider":         "validapse",
	})
}
