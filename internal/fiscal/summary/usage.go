package summary

import (
	"time"

	"gorm.io/gorm"

	"tukifac/pkg/database"
	"tukifac/pkg/saas"
)

// usage lo que el tenant tiene hoy de cada entidad limitada por su plan. Se cuenta con los mismos criterios con los que el sistema
// bloquea las altas (pkg/saas/quota.go): usuarios activos (incluye operativos de Tukichef), bienes del catálogo y sucursales activas.
// Los servicios se cuentan aparte: el bloqueo no los cuenta, pero el panel los muestra sumados a los productos.
type usage struct {
	Users, Products, Services, Branches int
}

// countUsage cuenta el uso del tenant. Un esquema antiguo sin alguna de las tablas no es un error del escaneo fiscal: devuelve ok=false y
// el llamador conserva los últimos conteos guardados.
func countUsage(tdb *gorm.DB) (u usage, ok bool) {
	m := tdb.Migrator()
	if !m.HasTable(&database.TenantUser{}) || !m.HasTable(&database.TenantProduct{}) || !m.HasTable(&database.TenantBranch{}) {
		return usage{}, false
	}
	return usage{
		Users:    int(saas.CountSeatUsers(tdb)),
		Products: int(saas.CountProductSeats(tdb)),
		Services: int(saas.CountServiceSeats(tdb)),
		Branches: int(saas.CountActiveBranches(tdb)),
	}, true
}

// applyUsageAndPlan completa la fila de salud con el uso y el plan vigente. Sin conteo nuevo (esquema viejo) se conserva el anterior.
func (s *Scanner) applyUsageAndPlan(h *database.TenantFiscalHealth, existing *database.TenantFiscalHealth, tdb *gorm.DB, tenantID uint, now time.Time) {
	if u, ok := countUsage(tdb); ok {
		h.UsersCount, h.ProductsCount, h.ServicesCount, h.BranchesCount = u.Users, u.Products, u.Services, u.Branches
		h.UsageScannedAt = &now
	} else if existing != nil {
		h.UsersCount, h.ProductsCount, h.ServicesCount, h.BranchesCount = existing.UsersCount, existing.ProductsCount, existing.ServicesCount, existing.BranchesCount
		h.UsageScannedAt = existing.UsageScannedAt
	}
	// Plan = suscripción no cancelada más reciente (la misma regla del bloqueo de altas). Un límite 0 es ilimitado.
	pl := saas.TenantPlanLimitsDB(s.Central, tenantID)
	h.PlanID, h.PlanName = pl.PlanID, pl.PlanName
	h.MaxUsers, h.MaxBranches, h.MaxProducts = pl.MaxUsers, pl.MaxBranches, pl.MaxProducts
	h.PlanSyncedAt = &now
}
