package service

import (
	"fmt"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/productbranch"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupProductBranchTestDB(t *testing.T, withTable bool) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models := []interface{}{
		&database.TenantProduct{}, &database.TenantBranch{}, &database.TenantProductStock{},
		&database.TenantProductPresentation{}, &database.TenantProductPresentationStock{},
	}
	if withTable {
		models = append(models, &database.TenantProductBranch{})
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	for _, b := range []database.TenantBranch{{ID: 1, Name: "Principal"}, {ID: 2, Name: "Norte"}, {ID: 3, Name: "Sur"}} {
		if err := db.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func createBranchTestProduct(t *testing.T, db *gorm.DB, code string, manageStock bool) uint {
	t.Helper()
	p := database.TenantProduct{Code: code, Name: code, Type: "product", Unit: "NIU", ManageStock: manageStock, Active: true}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func listCodesInBranch(t *testing.T, db *gorm.DB, branchID uint) map[string]bool {
	t.Helper()
	var rows []database.TenantProduct
	if err := NewProductService(db).buildListQuery(ProductListParams{BranchID: branchID}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Code] = true
	}
	return out
}

// Un producto sin control de stock asignado a una sucursal ya no aparece en las demás; sin
// asignación sigue apareciendo en todas (lo de siempre).
func TestProductBranchList_AssignmentRestrictsNonStockProduct(t *testing.T) {
	db := setupProductBranchTestDB(t, true)
	free := createBranchTestProduct(t, db, "LIBRE", false)
	onlyNorte := createBranchTestProduct(t, db, "SOLO-NORTE", false)
	_ = free
	if err := productbranch.Set(db, onlyNorte, []uint{2}); err != nil {
		t.Fatal(err)
	}

	principal := listCodesInBranch(t, db, 1)
	if !principal["LIBRE"] || principal["SOLO-NORTE"] {
		t.Fatalf("sucursal 1: esperaba LIBRE sí y SOLO-NORTE no, got %v", principal)
	}
	norte := listCodesInBranch(t, db, 2)
	if !norte["LIBRE"] || !norte["SOLO-NORTE"] {
		t.Fatalf("sucursal 2: esperaba ambos, got %v", norte)
	}
}

// Producto con control de stock asignado a una sucursal sin fila de stock: aparece igual (la
// asignación manda); y deja de aparecer en la sucursal donde tiene stock pero no está asignado.
func TestProductBranchList_AssignmentOverridesStockRule(t *testing.T) {
	db := setupProductBranchTestDB(t, true)
	id := createBranchTestProduct(t, db, "STOCK", true)
	if err := db.Create(&database.TenantProductStock{ProductID: id, BranchID: 1, Quantity: 5}).Error; err != nil {
		t.Fatal(err)
	}
	if !listCodesInBranch(t, db, 1)["STOCK"] || listCodesInBranch(t, db, 3)["STOCK"] {
		t.Fatal("sin asignación debe seguir la regla de stock")
	}
	if err := productbranch.Set(db, id, []uint{3}); err != nil {
		t.Fatal(err)
	}
	if listCodesInBranch(t, db, 1)["STOCK"] || !listCodesInBranch(t, db, 3)["STOCK"] {
		t.Fatal("con asignación debe aparecer solo en la sucursal asignada")
	}
	// Volver a "todas".
	if err := productbranch.Set(db, id, nil); err != nil {
		t.Fatal(err)
	}
	if !listCodesInBranch(t, db, 1)["STOCK"] {
		t.Fatal("sin asignación vuelve la regla de stock")
	}
}

// Entrada de stock en una sucursal no asignada: se asigna sola; un producto sin asignación no
// se restringe.
func TestProductBranch_AssignIfRestricted(t *testing.T) {
	db := setupProductBranchTestDB(t, true)
	restricted := createBranchTestProduct(t, db, "R", true)
	free := createBranchTestProduct(t, db, "F", true)
	if err := productbranch.Set(db, restricted, []uint{1}); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []uint{restricted, free} {
		if err := productbranch.AssignIfRestricted(db, pid, 2); err != nil {
			t.Fatal(err)
		}
	}
	got, err := productbranch.BranchIDs(db, []uint{restricted, free})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got[restricted]) != "[1 2]" {
		t.Fatalf("restringido: esperaba [1 2], got %v", got[restricted])
	}
	if len(got[free]) != 0 {
		t.Fatalf("sin asignación no debe restringirse, got %v", got[free])
	}
	if err := productbranch.EnsureAvailable(db, restricted, 3, "R"); err == nil {
		t.Fatal("debe rechazar vender en una sucursal no asignada")
	}
	if err := productbranch.EnsureAvailable(db, free, 3, "F"); err != nil {
		t.Fatalf("sin asignación se vende en todas: %v", err)
	}
}

// Tenant sin la migración v162: nada se rompe y todo se comporta como "sin asignación".
func TestProductBranch_WithoutTableBehavesAsBefore(t *testing.T) {
	db := setupProductBranchTestDB(t, false)
	id := createBranchTestProduct(t, db, "X", false)
	if !listCodesInBranch(t, db, 2)["X"] {
		t.Fatal("sin la tabla el listado debe usar la regla anterior")
	}
	if err := productbranch.EnsureAvailable(db, id, 2, "X"); err != nil {
		t.Fatalf("sin la tabla no debe bloquear ventas: %v", err)
	}
	if err := productbranch.AssignIfRestricted(db, id, 2); err != nil {
		t.Fatalf("sin la tabla no debe fallar la entrada de stock: %v", err)
	}
	if err := productbranch.Set(db, id, []uint{2}); err != productbranch.ErrNotMigrated {
		t.Fatalf("asignar sin la tabla debe avisar que falta la migración, got %v", err)
	}
}

// Columna "sucursales" del import: nombres sin distinguir mayúsculas, "todas" y vacío = no tocar.
func TestResolveImportBranches(t *testing.T) {
	db := setupProductBranchTestDB(t, true)
	svc := NewProductService(db)
	var cache map[string]uint
	str := func(s string) *string { return &s }

	if _, set, err := svc.resolveImportBranches(nil, &cache); err != nil || set {
		t.Fatalf("nil: set=%v err=%v", set, err)
	}
	if _, set, err := svc.resolveImportBranches(str("  "), &cache); err != nil || set {
		t.Fatalf("vacío: set=%v err=%v", set, err)
	}
	if ids, set, err := svc.resolveImportBranches(str("Todas"), &cache); err != nil || !set || len(ids) != 0 {
		t.Fatalf("todas: ids=%v set=%v err=%v", ids, set, err)
	}
	ids, set, err := svc.resolveImportBranches(str("norte, PRINCIPAL"), &cache)
	if err != nil || !set || fmt.Sprint(ids) != "[2 1]" {
		t.Fatalf("nombres: ids=%v set=%v err=%v", ids, set, err)
	}
	if _, _, err := svc.resolveImportBranches(str("Inexistente"), &cache); err == nil {
		t.Fatal("una sucursal inexistente debe fallar la fila")
	}
}
