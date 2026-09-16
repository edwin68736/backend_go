package service

import "testing"

// Corrección "Unidad Comercial Fiscal": TenantProductSaleUnit gana UnitID/Unit (unidad comercial
// SUNAT propia, ej. "Caja" → BX), siguiendo el mismo patrón que TenantProduct.UnitID/Unit
// (resolveUnitReference). Estos tests cubren la validación/resolución a nivel de catálogo — la
// resolución al vender (SaleItem.Unit) se prueba en internal/sales/service/sale_unit_unit_code_test.go.

// Una SaleUnit nueva sin UnitID se rechaza — "SaleUnits nuevas deben exigir una unidad válida".
func TestSaleUnit_Create_RequiresUnitID(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)
	p := newSaleUnitTestProduct(t, db)

	if _, err := svc.CreateSaleUnit(p.ID, SaleUnitInput{
		Name: "Caja", ConversionFactor: 12, Price1: 45,
	}); err == nil {
		t.Fatal("una SaleUnit nueva sin UnitID debería rechazarse")
	}
}

// Un UnitID que no existe en TenantUnit se rechaza (no se acepta un ID inventado ni se cae a NIU
// silenciosamente).
func TestSaleUnit_Create_RejectsInvalidUnitID(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)
	p := newSaleUnitTestProduct(t, db)
	bogus := uint(999999)

	if _, err := svc.CreateSaleUnit(p.ID, SaleUnitInput{
		Name: "Caja", UnitID: &bogus, ConversionFactor: 12, Price1: 45,
	}); err == nil {
		t.Fatal("un UnitID inexistente debería rechazarse")
	}
}

// Con un UnitID válido, CreateSaleUnit resuelve y persiste el código real del catálogo (TenantUnit.Code)
// — nunca lo deriva del nombre de la SaleUnit ("Caja" no implica "BX" por sí solo).
func TestSaleUnit_Create_ResolvesUnitCodeFromCatalog(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)
	p := newSaleUnitTestProduct(t, db)
	unitID := newSaleUnitTestUnit(t, db) // TenantUnit{Code: "BX", Name: "Caja"}

	u, err := svc.CreateSaleUnit(p.ID, SaleUnitInput{
		// Nombre comercial deliberadamente distinto del nombre de la unidad SUNAT, para probar
		// que no hay ninguna derivación mágica entre ambos.
		Name: "Caja Grande", UnitID: &unitID, ConversionFactor: 12, Price1: 45,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.UnitID == nil || *u.UnitID != unitID {
		t.Errorf("UnitID = %v, want %d", u.UnitID, unitID)
	}
	if u.Unit != "BX" {
		t.Errorf("Unit = %q, want BX (resuelto desde TenantUnit.Code, no desde el nombre)", u.Unit)
	}
	if u.ConversionFactor != 12 {
		t.Errorf("ConversionFactor = %v, want 12 — el factor no debe derivarse ni verse afectado por el código de unidad", u.ConversionFactor)
	}
}

// UpdateSaleUnit NO exige UnitID — una SaleUnit creada antes de esta corrección (sin unidad
// configurada) debe poder seguir editándose (ej. cambiar el precio) sin verse forzada a completar
// retroactivamente su unidad SUNAT.
func TestSaleUnit_Update_UnitIDOptional_PreExistingRowKeepsWorking(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)
	p := newSaleUnitTestProduct(t, db)
	unitID := newSaleUnitTestUnit(t, db)

	// Simula una SaleUnit creada ANTES de esta corrección (unit_id/unit vacíos), vaciándolos
	// manualmente después de crearla con CreateSaleUnit (que hoy exigiría UnitID).
	created, err := svc.CreateSaleUnit(p.ID, SaleUnitInput{Name: "Caja", UnitID: &unitID, ConversionFactor: 12, Price1: 45})
	if err != nil {
		t.Fatal(err)
	}
	// Vacía manualmente unit_id/unit para reproducir el estado "creada antes de esta fase".
	if err := db.Model(created).Updates(map[string]interface{}{"unit_id": nil, "unit": ""}).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := svc.UpdateSaleUnit(p.ID, created.ID, SaleUnitInput{
		Name: "Caja", ConversionFactor: 12, Price1: 48, Active: true, // sin UnitID
	})
	if err != nil {
		t.Fatalf("Update sin UnitID no debería fallar para una fila preexistente: %v", err)
	}
	if updated.Price1 != 48 {
		t.Errorf("Price1 = %v, want 48 (el update debe aplicarse igual)", updated.Price1)
	}
	if updated.UnitID != nil || updated.Unit != "" {
		t.Errorf("UnitID/Unit deberían seguir vacíos (no se inventa nada), got UnitID=%v Unit=%q", updated.UnitID, updated.Unit)
	}
}
