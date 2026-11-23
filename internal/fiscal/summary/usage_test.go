package summary

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)


func seedUsage(t *testing.T, name string) *Scanner {
	t.Helper()
	tdb := tenantDB(t, name)
	mustExec(t, tdb, `CREATE TABLE tenant_users (id INTEGER PRIMARY KEY AUTOINCREMENT, active BOOLEAN, deleted_at DATETIME)`)
	mustExec(t, tdb, `CREATE TABLE tenant_products (id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT, deleted_at DATETIME)`)
	mustExec(t, tdb, `CREATE TABLE tenant_branches (id INTEGER PRIMARY KEY AUTOINCREMENT, active BOOLEAN, deleted_at DATETIME)`)
	for _, a := range []int{1, 1, 0} { // 2 usuarios activos y 1 inactivo
		mustExec(t, tdb, `INSERT INTO tenant_users (active) VALUES (?)`, a)
	}
	for _, ty := range []string{"product", "product", "product", "service", "service"} {
		mustExec(t, tdb, `INSERT INTO tenant_products (type) VALUES (?)`, ty)
	}
	mustExec(t, tdb, `INSERT INTO tenant_products (type, deleted_at) VALUES ('product', '2026-10-01 10:00:00')`) // eliminado: no cuenta
	for _, a := range []int{1, 0} {
		mustExec(t, tdb, `INSERT INTO tenant_branches (active) VALUES (?)`, a)
	}
	s, _ := newScanner(t, tdb, time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC))
	if err := s.Central.AutoMigrate(&database.SaasPlan{}, &database.SaasSubscription{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScan_guardaUsoYPlanVigenteConElMismoCriterioDelBloqueo(t *testing.T) {
	s := seedUsage(t, "usage_a")
	old := database.SaasPlan{Name: "MICROEMPRESA", MaxUsers: 3, MaxBranches: 1, MaxProducts: 1000}
	cur := database.SaasPlan{Name: "EMPRENDEDOR", MaxUsers: 4, MaxBranches: 2, MaxProducts: 2000}
	s.Central.Create(&old)
	s.Central.Create(&cur)
	var tenant database.Tenant
	s.Central.First(&tenant)
	s.Central.Create(&database.SaasSubscription{TenantID: tenant.ID, PlanID: old.ID, Status: database.SaasSubActive, CreatedAt: time.Now().Add(-48 * time.Hour)})
	s.Central.Create(&database.SaasSubscription{TenantID: tenant.ID, PlanID: cur.ID, Status: database.SaasSubActive, CreatedAt: time.Now()})

	if err := s.ScanTenant(tenant, false); err != nil {
		t.Fatal(err)
	}
	var h database.TenantFiscalHealth
	if err := s.Central.First(&h, "tenant_id = ?", tenant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if h.UsersCount != 2 || h.ProductsCount != 3 || h.ServicesCount != 2 || h.BranchesCount != 1 || h.UsageScannedAt == nil {
		t.Fatalf("uso: users=%d products=%d services=%d branches=%d", h.UsersCount, h.ProductsCount, h.ServicesCount, h.BranchesCount)
	}
	if h.PlanName != "EMPRENDEDOR" || h.MaxUsers != 4 || h.MaxBranches != 2 || h.MaxProducts != 2000 {
		t.Fatalf("plan vigente = la suscripción más reciente: %+v", h)
	}
}

func TestScan_esquemaViejoConservaLosUltimosConteos(t *testing.T) {
	s := seedUsage(t, "usage_b")
	var tenant database.Tenant
	s.Central.First(&tenant)
	if err := s.ScanTenant(tenant, false); err != nil {
		t.Fatal(err)
	}
	// El tenant "pierde" una tabla (esquema antiguo): el escaneo fiscal sigue y los conteos anteriores quedan.
	tdb, _, _ := s.Open(tenant)
	tdb.Exec(`DROP TABLE tenant_branches`)
	if err := s.ScanTenant(tenant, false); err != nil {
		t.Fatalf("sin tablas de uso no debe fallar el escaneo fiscal: %v", err)
	}
	var h database.TenantFiscalHealth
	s.Central.First(&h, "tenant_id = ?", tenant.ID)
	if h.UsersCount != 2 || h.ProductsCount != 3 || h.BranchesCount != 1 {
		t.Fatalf("se conservan los últimos conteos: %+v", h)
	}
}

func TestSummarize_catalogoSumaProductosYServiciosYEstadoDeLimite(t *testing.T) {
	s := seedUsage(t, "usage_c")
	plan := database.SaasPlan{Name: "PREMIUM", MaxUsers: 2, MaxBranches: 0, MaxProducts: 6}
	s.Central.Create(&plan)
	var tenant database.Tenant
	s.Central.First(&tenant)
	s.Central.Create(&database.SaasSubscription{TenantID: tenant.ID, PlanID: plan.ID, Status: database.SaasSubActive, CreatedAt: time.Now()})
	if err := s.ScanTenant(tenant, false); err != nil {
		t.Fatal(err)
	}
	res, err := Summarize(s.Central, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Items[0]
	if r.CatalogCount != 5 || r.ProductsCount != 3 || r.ServicesCount != 2 {
		t.Fatalf("catálogo = productos + servicios: %+v", r)
	}
	// 2/2 usuarios llegó al límite → excedido; las sucursales ilimitadas (0) no cuentan.
	if r.LimitStatus != "exceeded" || res.Totals.OverLimit != 1 {
		t.Fatalf("estado de límite = %q, over=%d", r.LimitStatus, res.Totals.OverLimit)
	}
	if only, _ := Summarize(s.Central, Filter{LimitStatus: "exceeded"}); only.Total != 1 {
		t.Fatalf("filtro excedidos: %d", only.Total)
	}
	// Con el límite de usuarios holgado y el catálogo al 83 % (5/6): cerca del límite.
	s.Central.Model(&database.TenantFiscalHealth{}).Where("tenant_id = ?", tenant.ID).Update("max_users", 10)
	res, _ = Summarize(s.Central, Filter{})
	if res.Items[0].LimitStatus != "near" || res.Totals.NearLimit != 1 {
		t.Fatalf("5/6 = cerca del límite: %q", res.Items[0].LimitStatus)
	}
	if none, _ := Summarize(s.Central, Filter{LimitStatus: "exceeded"}); none.Total != 0 {
		t.Fatalf("ya no está excedido: %d", none.Total)
	}
}

func TestSortRows_usoPorLimiteConIlimitadosAlFinal(t *testing.T) {
	rows := []*Row{
		{Slug: "ilim", Name: "I", UsersCount: 9, MaxUsers: 0},
		{Slug: "bajo", Name: "B", UsersCount: 1, MaxUsers: 10},
		{Slug: "alto", Name: "A", UsersCount: 5, MaxUsers: 5},
	}
	sortRows(rows, "users", "")
	sameOrder(t, "uso desc", rows, "alto", "bajo", "ilim")
	sortRows(rows, "users", "asc")
	sameOrder(t, "uso asc: ilimitados siempre al final", rows, "bajo", "alto", "ilim")
}
