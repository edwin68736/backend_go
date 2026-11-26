package service

import (
	"fmt"
	"testing"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// El conteo por plan usa la suscripción vigente más reciente (ignorando las canceladas) y no hace una consulta por tenant.
func TestStats_CuentaPorPlanVigenteSinConsultasPorTenant(t *testing.T) {
	db := setupPlanRefTestDB(t)
	if err := db.AutoMigrate(&database.Tenant{}); err != nil {
		t.Fatal(err)
	}
	basic := seedPlan(t, db, "Basic", true)
	pro := seedPlan(t, db, "Pro", true)

	n := 0
	tenant := func(status string) uint {
		n++
		slug := fmt.Sprintf("empresa-%d", n)
		tn := database.Tenant{Name: slug, Slug: slug, DBName: "db_" + slug, RUC: fmt.Sprintf("2000000000%d", n), Status: status}
		if err := db.Create(&tn).Error; err != nil {
			t.Fatal(err)
		}
		return tn.ID
	}
	sub := func(tenantID, planID uint, status string, daysAgo int) {
		s := database.SaasSubscription{TenantID: tenantID, PlanID: planID, Status: status,
			StartDate: time.Now(), EndDate: time.Now().AddDate(0, 1, 0), CreatedAt: time.Now().AddDate(0, 0, -daysAgo)}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}

	cambioDePlan := tenant("active") // antes Pro, ahora Basic
	sub(cambioDePlan, pro.ID, "active", 30)
	sub(cambioDePlan, basic.ID, "active", 1)
	conCancelada := tenant("active") // la más nueva está cancelada: vale la anterior
	sub(conCancelada, pro.ID, "active", 20)
	sub(conCancelada, basic.ID, database.SaasSubCancelled, 1)
	tenant("inactive") // sin suscripción

	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:count_query", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	svc := &TenantService{db: db}
	stats, err := svc.Stats()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"total": 3, "active": 2, "inactive": 1, "suspended": 0, "plan_basic": 1, "plan_pro": 1, "plan_sin_plan": 1, "basic": 1, "pro": 1}
	for k, v := range want {
		if stats[k] != v {
			t.Errorf("stats[%q] = %d, want %d", k, stats[k], v)
		}
	}
	if queries > 7 {
		t.Errorf("consultas = %d: no debe haber una por tenant", queries)
	}
}
