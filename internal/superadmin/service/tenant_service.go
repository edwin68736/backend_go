package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	subssvc "tukifac/internal/subscriptions/service"
	usersvc "tukifac/internal/users/service"
	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"
	"tukifac/pkg/utils"

	"gorm.io/gorm"
)

type TenantService struct {
	db *gorm.DB
}

func strPtr(s string) *string { return &s }

func NewTenantService() *TenantService {
	return &TenantService{db: database.CentralDB}
}

type CreateTenantInput struct {
	Name               string `json:"name"`
	Email              string `json:"email"`
	Phone              string `json:"phone"`
	RUC                string `json:"ruc"`
	Plan               string `json:"plan"`
	Slug               string `json:"slug"`
	Address            string `json:"address"`
	Ubigeo             string `json:"ubigeo"` // código 6 dígitos del distrito
	AdminEmail         string `json:"admin_email"`
	AdminPassword      string `json:"admin_password"`
	SubscriptionMonths int    `json:"subscription_months"` // duración en meses de la suscripción al crear (0 = no crear suscripción automática)
}

func (s *TenantService) Create(input CreateTenantInput) (*database.Tenant, error) {
	if input.Name == "" {
		return nil, errors.New("el nombre es requerido")
	}
	if input.AdminEmail == "" || input.AdminPassword == "" {
		return nil, errors.New("email y contraseña del administrador son requeridos")
	}

	var slug string
	if input.Slug != "" {
		normalized, err := utils.NormalizeSubdomain(input.Slug)
		if err != nil {
			return nil, err
		}
		slug = normalized
	} else {
		slug = utils.Slugify(input.Name)
		if slug == "" {
			return nil, errors.New("nombre inválido para generar subdominio")
		}
		// Quitar guiones para mantener subdominio corto cuando se genera desde el nombre
		slug = strings.ReplaceAll(slug, "-", "")
		if len(slug) < 2 {
			slug = utils.Slugify(input.Name) // fallback con guiones si queda muy corto
		}
	}

	if config.AppConfig != nil && config.AppConfig.IsReservedSubdomain(slug) {
		return nil, fmt.Errorf("el subdominio %q está reservado (api, app, www, etc.). Elige otro identificador", slug)
	}

	// Verificar que el slug sea único
	var existing database.Tenant
	if err := s.db.Where("slug = ?", slug).First(&existing).Error; err == nil {
		return nil, errors.New("ya existe una empresa con ese subdominio. Elige otro identificador")
	}

	dbName := "saas_tenant_" + slug
	plan := input.Plan
	if plan == "" {
		plan = "trial"
	}

	trialEnd := time.Now().AddDate(0, 0, 30)
	tenant := &database.Tenant{
		Name:        input.Name,
		Slug:        slug,
		DBName:      dbName,
		Plan:        plan,
		Status:      "active",
		Email:       input.Email,
		Phone:       input.Phone,
		RUC:         input.RUC,
		Address:     input.Address,
		Ubigeo:      input.Ubigeo,
		TrialEndsAt: &trialEnd,
	}

	if err := s.db.Create(tenant).Error; err != nil {
		return nil, fmt.Errorf("creando tenant: %w", err)
	}

	// Crear la base de datos del tenant
	if err := database.CreateTenantDB(dbName); err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("creando BD del tenant: %w", err)
	}

	database.RemoveTenantFromPool(dbName)
	if err := database.MigrateTenantSchema(dbName); err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("migrando esquema del tenant: %w", err)
	}
	tenantDB, err := database.GetTenantDB(dbName)
	if err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("conectando BD del tenant: %w", err)
	}

	// Seed inicial del tenant (incluye domicilio fiscal: dirección y ubigeo del formulario)
	if err := database.SeedTenant(tenantDB, input.AdminEmail, input.AdminPassword, input.Name, input.RUC, input.Address, input.Ubigeo); err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("inicializando datos del tenant: %w", err)
	}

	// Seed de permisos y asignación de todos los permisos al rol Administrador
	roleSvc := usersvc.NewRoleService(tenantDB)
	if err := roleSvc.SeedPermissions(); err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("inicializando permisos del tenant: %w", err)
	}
	perms, err := roleSvc.AllPermissions()
	if err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("listando permisos del tenant: %w", err)
	}
	var adminRole database.TenantRole
	if err := tenantDB.Where("name = ?", "Administrador").First(&adminRole).Error; err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("rol Administrador no encontrado: %w", err)
	}
	permIDs := make([]uint, len(perms))
	for i, p := range perms {
		permIDs[i] = p.ID
	}
	if err := roleSvc.SetRolePermissions(adminRole.ID, permIDs); err != nil {
		s.rollbackNewTenant(tenant.ID, dbName)
		return nil, fmt.Errorf("asignando permisos al Administrador: %w", err)
	}

	// Activar módulos base
	baseModules := []string{"sales", "purchases", "inventory", "cashbank", "contacts", "products"}
	for _, mod := range baseModules {
		s.db.Create(&database.TenantModule{
			TenantID:   tenant.ID,
			ModuleKey:  mod,
			Enabled:    true,
			ConfigJSON: strPtr("{}"),
		})
	}
	// Registrar módulos adicionales desactivados por defecto (catálogo extendido)
	otherModules := []string{
		"billing",
		"restaurant",
		"ecommerce",
		"hotel",
		"clinic",
		"transport",
		"manufacturing",
		"memberships",
		"hr",
		"accounting",
		"bi",
		"fixedassets",
		"documents",
		"support",
	}
	for _, mod := range otherModules {
		s.db.Create(&database.TenantModule{
			TenantID:   tenant.ID,
			ModuleKey:  mod,
			Enabled:    false,
			ConfigJSON: strPtr("{}"),
		})
	}

	// Crear suscripción según el plan elegido y duración en meses (si se indicó)
	if input.SubscriptionMonths > 0 && plan != "" {
		var saasPlan database.SaasPlan
		if err := s.db.Where("LOWER(name) = LOWER(?) AND active = ?", plan, true).First(&saasPlan).Error; err == nil {
			subSvc := subssvc.NewSubscriptionService()
			_, err = subSvc.Create(subssvc.CreateSubscriptionInput{
				TenantID: tenant.ID,
				PlanID:   saasPlan.ID,
				Months:   input.SubscriptionMonths,
				Notes:    "Suscripción creada al registrar la empresa",
			})
			if err != nil {
				// No fallar la creación del tenant; solo quedaría sin suscripción
				_ = err
			}
		}
	}

	return tenant, nil
}

// rollbackNewTenant revierte alta parcial: quita del pool, borra BD tenant y registro central.
func (s *TenantService) rollbackNewTenant(tenantID uint, dbName string) {
	database.RemoveTenantFromPool(dbName)
	_ = database.DropTenantDB(dbName)
	if tenantID > 0 {
		s.db.Where("tenant_id = ?", tenantID).Delete(&database.TenantModule{})
		s.db.Delete(&database.Tenant{}, tenantID)
	}
}

func (s *TenantService) List(query, status, regionID, provinciaID string) ([]database.Tenant, error) {
	var tenants []database.Tenant
	q := s.db.Model(&database.Tenant{})
	if query != "" {
		q = q.Where("name LIKE ? OR slug LIKE ? OR ruc LIKE ? OR address LIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%", "%"+query+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	// Filtro por departamento: ubigeo empieza con los 2 primeros dígitos del region_id (ej. 15 para Lima)
	if regionID != "" && len(regionID) >= 2 {
		prefix := regionID
		if len(prefix) > 2 {
			prefix = prefix[:2]
		}
		q = q.Where("ubigeo LIKE ?", prefix+"%")
	}
	// Filtro por provincia: ubigeo empieza con los 4 primeros dígitos del provincia_id (ej. 1501 para Lima)
	if provinciaID != "" && len(provinciaID) >= 4 {
		prefix := provinciaID[:4]
		q = q.Where("ubigeo LIKE ?", prefix+"%")
	}
	err := q.Order("created_at DESC").Find(&tenants).Error
	return tenants, err
}

// BillingEnabledByTenantIDs devuelve un mapa tenant_id -> true si el tenant tiene el módulo billing habilitado.
func (s *TenantService) BillingEnabledByTenantIDs(ids []uint) (map[uint]bool, error) {
	if len(ids) == 0 {
		return map[uint]bool{}, nil
	}
	var mods []database.TenantModule
	if err := s.db.Where("tenant_id IN ? AND module_key = ? AND enabled = ?", ids, "billing", true).Find(&mods).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]bool)
	for _, m := range mods {
		out[m.TenantID] = true
	}
	return out, nil
}

func (s *TenantService) GetByID(id uint) (*database.Tenant, error) {
	var tenant database.Tenant
	if err := s.db.First(&tenant, id).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}

func (s *TenantService) Update(id uint, input database.Tenant) error {
	// Obtener plan actual para detectar si cambió
	var current database.Tenant
	s.db.Select("plan").First(&current, id)

	err := s.db.Model(&database.Tenant{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":    input.Name,
		"email":   input.Email,
		"phone":   input.Phone,
		"ruc":     input.RUC,
		"plan":    input.Plan,
		"status":  input.Status,
		"address": input.Address,
		"ubigeo":  input.Ubigeo,
	}).Error
	if err != nil {
		return err
	}

	// Si el plan cambió, sincronizar módulos automáticamente
	if input.Plan != "" && input.Plan != current.Plan {
		s.syncModulesByPlanName(id, input.Plan)
	}

	var updated database.Tenant
	if err := s.db.Select("slug").First(&updated, id).Error; err == nil {
		middleware.InvalidateTenantCache(updated.Slug)
	}
	return nil
}

// syncModulesByPlanName encuentra el SaasPlan correspondiente al nombre y sincroniza TenantModule
func (s *TenantService) syncModulesByPlanName(tenantID uint, planName string) {
	var plan database.SaasPlan
	// Buscar plan por nombre (case-insensitive)
	if err := s.db.Where("LOWER(name) = LOWER(?)", planName).First(&plan).Error; err != nil {
		return // Plan no encontrado en saas_plans, omitir sincronización
	}

	var planModules []database.SaasPlanModule
	s.db.Where("plan_id = ?", plan.ID).Find(&planModules)

	planSet := make(map[string]bool)
	for _, pm := range planModules {
		planSet[pm.ModuleKey] = true
	}

	// Desactivar todos los módulos del tenant primero
	s.db.Model(&database.TenantModule{}).
		Where("tenant_id = ?", tenantID).
		Update("enabled", false)

	// Activar sólo los módulos del nuevo plan
	for key := range planSet {
		var tm database.TenantModule
		if err := s.db.Where("tenant_id = ? AND module_key = ?", tenantID, key).First(&tm).Error; err != nil {
			s.db.Create(&database.TenantModule{
				TenantID:   tenantID,
				ModuleKey:  key,
				Enabled:    true,
				ConfigJSON: strPtr("{}"),
			})
		} else {
			s.db.Model(&tm).Update("enabled", true)
		}
	}
}

func (s *TenantService) SetStatus(id uint, status string) error {
	if status == "inactive" {
		var tenant database.Tenant
		if err := s.db.First(&tenant, id).Error; err != nil {
			return err
		}
		database.RemoveTenantFromPool(tenant.DBName)
	}
	return s.db.Model(&database.Tenant{}).Where("id = ?", id).Update("status", status).Error
}

func (s *TenantService) GetModules(tenantID uint) ([]database.TenantModule, error) {
	var modules []database.TenantModule
	err := s.db.Where("tenant_id = ?", tenantID).Find(&modules).Error
	return modules, err
}

func (s *TenantService) SetModule(tenantID uint, moduleKey string, enabled bool) error {
	var mod database.TenantModule
	err := s.db.Where("tenant_id = ? AND module_key = ?", tenantID, moduleKey).First(&mod).Error
	if err != nil {
		// Crear
		return s.db.Create(&database.TenantModule{
			TenantID:   tenantID,
			ModuleKey:  moduleKey,
			Enabled:    enabled,
			ConfigJSON: strPtr("{}"),
		}).Error
	}
	return s.db.Model(&mod).Update("enabled", enabled).Error
}

// RunMigrations ejecuta las migraciones del tenant (tablas/columnas nuevas) y opcionalmente el seed de permisos si no existen.
func (s *TenantService) RunMigrations(tenantID uint) error {
	tenant, err := s.GetByID(tenantID)
	if err != nil {
		return err
	}
	if err := database.MigrateTenantSchema(tenant.DBName); err != nil {
		return fmt.Errorf("migrando esquema: %w", err)
	}
	tenantDB, err := database.GetTenantDB(tenant.DBName)
	if err != nil {
		return fmt.Errorf("conectando BD del tenant: %w", err)
	}
	// Asegurar que permisos existan (tenants creados antes de este cambio pueden no tenerlos)
	roleSvc := usersvc.NewRoleService(tenantDB)
	if err := roleSvc.SeedPermissions(); err != nil {
		return fmt.Errorf("inicializando permisos: %w", err)
	}
	var adminRole database.TenantRole
	if err := tenantDB.Where("name = ?", "Administrador").First(&adminRole).Error; err == nil {
		perms, _ := roleSvc.AllPermissions()
		permIDs := make([]uint, len(perms))
		for i, p := range perms {
			permIDs[i] = p.ID
		}
		_ = roleSvc.SetRolePermissions(adminRole.ID, permIDs)
	}
	return nil
}

// RunMigrationsAll ejecuta migraciones para todos los tenants activos (no detiene en el primero fallido).
func (s *TenantService) RunMigrationsAll() (database.MigrateSummary, error) {
	summary := database.MigrateTenantsBatch(true, nil)
	for _, f := range summary.Failed {
		if f.Slug == "(list)" {
			return summary, f.Err
		}
	}
	return summary, nil
}

func (s *TenantService) Stats() (map[string]int64, error) {
	stats := make(map[string]int64)
	var total, active, inactive, trial, basic, pro int64
	s.db.Model(&database.Tenant{}).Count(&total)
	s.db.Model(&database.Tenant{}).Where("status = ?", "active").Count(&active)
	s.db.Model(&database.Tenant{}).Where("status = ?", "inactive").Count(&inactive)
	s.db.Model(&database.Tenant{}).Where("plan = ?", "trial").Count(&trial)
	s.db.Model(&database.Tenant{}).Where("plan = ?", "basic").Count(&basic)
	s.db.Model(&database.Tenant{}).Where("plan = ?", "pro").Count(&pro)
	stats["total"] = total
	stats["active"] = active
	stats["inactive"] = inactive
	stats["trial"] = trial
	stats["basic"] = basic
	stats["pro"] = pro
	return stats, nil
}
