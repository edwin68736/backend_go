package service

import (
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupRoleServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantPermission{}, &database.TenantRole{}, &database.TenantRolePermission{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func catalogSize(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.TenantPermission{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSeedPermissions_FreshEmptyTable_SeedsFullCatalog(t *testing.T) {
	db := setupRoleServiceTestDB(t)
	svc := NewRoleService(db)

	if err := svc.SeedPermissions(); err != nil {
		t.Fatalf("SeedPermissions: %v", err)
	}

	got := catalogSize(t, db)
	if got == 0 {
		t.Fatal("esperaba que se sembrara el catálogo completo, quedó vacío")
	}
	// El catálogo real vive hardcodeado dentro de SeedPermissions; solo verificamos que
	// contenga los permisos "base" que el bug de producción dejaba sin sembrar.
	for _, want := range [][2]string{
		{"dashboard", "view"}, {"users", "view"}, {"roles", "view"}, {"company", "view"},
		{"contacts", "view"}, {"products", "view"}, {"sales", "view"}, {"purchases", "view"},
		{"cashbank", "view"}, {"billing", "send"}, {"memberships", "view"},
	} {
		var count int64
		db.Model(&database.TenantPermission{}).
			Where("module = ? AND action = ?", want[0], want[1]).Count(&count)
		if count == 0 {
			t.Fatalf("falta %s.%s en el catálogo sembrado", want[0], want[1])
		}
	}
}

// TestSeedPermissions_PartiallyPrePopulated_StillCompletesFullCatalog es la regresión del bug
// real de producción (2026-09-12, RUC 20615266010): una migración de schema como
// V131/V132PermissionCatalogRedesign puede insertar, ANTES de que SeedPermissions corra, un
// subconjunto de filas nuevas del catálogo (vía FirstOrCreate) para un tenant recién creado —
// antes de este fix, la guarda "if count > 0 return nil" hacía que SeedPermissions abortara sin
// sembrar el resto del catálogo "base" (dashboard, users, roles, company, contacts, products,
// sales, purchases, cashbank básico, billing.send, memberships), dejando el catálogo de ese
// tenant permanentemente incompleto.
func TestSeedPermissions_PartiallyPrePopulated_StillCompletesFullCatalog(t *testing.T) {
	db := setupRoleServiceTestDB(t)
	// Simula lo que V131/V132 adelantan para un tenant nuevo, antes de que SeedPermissions corra.
	if err := db.Create(&database.TenantPermission{
		Module: "modules", Action: "manage", Label: "Activar/desactivar módulos",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantPermission{
		Module: "quotations", Action: "view", Label: "Ver cotizaciones",
	}).Error; err != nil {
		t.Fatal(err)
	}
	preCount := catalogSize(t, db)
	if preCount != 2 {
		t.Fatalf("setup: esperaba 2 filas pre-existentes, got %d", preCount)
	}

	svc := NewRoleService(db)
	if err := svc.SeedPermissions(); err != nil {
		t.Fatalf("SeedPermissions con catálogo parcial: %v", err)
	}

	// Los permisos "base" que el bug dejaba sin sembrar deben existir ahora.
	for _, want := range [][2]string{
		{"dashboard", "view"}, {"users", "view"}, {"roles", "view"}, {"company", "view"},
		{"contacts", "view"}, {"products", "view"}, {"sales", "view"}, {"purchases", "view"},
		{"cashbank", "view"}, {"billing", "send"}, {"memberships", "view"},
	} {
		var count int64
		db.Model(&database.TenantPermission{}).
			Where("module = ? AND action = ?", want[0], want[1]).Count(&count)
		if count == 0 {
			t.Fatalf("REGRESIÓN: falta %s.%s tras SeedPermissions con catálogo parcial "+
				"pre-poblado (el bug original hacía que esto se saltara)", want[0], want[1])
		}
	}

	// No debe haber duplicado los 2 permisos que ya existían.
	var dupCheck int64
	db.Model(&database.TenantPermission{}).
		Where("module = ? AND action = ?", "modules", "manage").Count(&dupCheck)
	if dupCheck != 1 {
		t.Fatalf("modules.manage no debe duplicarse, got count=%d", dupCheck)
	}
}

func TestSeedPermissions_Idempotent(t *testing.T) {
	db := setupRoleServiceTestDB(t)
	svc := NewRoleService(db)

	if err := svc.SeedPermissions(); err != nil {
		t.Fatalf("first: %v", err)
	}
	first := catalogSize(t, db)

	if err := svc.SeedPermissions(); err != nil {
		t.Fatalf("second (idempotent): %v", err)
	}
	second := catalogSize(t, db)

	if first != second {
		t.Fatalf("correrlo 2 veces no debe cambiar el tamaño del catálogo: first=%d second=%d", first, second)
	}
}
