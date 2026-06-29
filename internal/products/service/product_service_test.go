package service

import (
	"fmt"
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupProductServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantProduct{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProductCreate_ManageStockFalsePersistsInDB(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)

	p, err := svc.Create(ProductInput{
		Code:               "TST-NO-STOCK",
		Name:               "Sin control stock",
		Type:               "product",
		Unit:               "NIU",
		SalePrice:          10,
		TaxRate:            18,
		IgvAffectationType: "10",
		ManageStock:        false,
		Active:             true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ManageStock {
		t.Fatalf("ManageStock en memoria: got true, want false")
	}

	var loaded database.TenantProduct
	if err := db.First(&loaded, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.ManageStock {
		t.Fatalf("manage_stock en BD: got true, want false")
	}
}

func TestProductList_NoManageStockOnly(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)
	branchID := uint(1)
	_, err := svc.Create(ProductInput{
		Code: "WITH-STK", Name: "Con stock", Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		ManageStock: true, IsRestaurant: true, BranchID: branchID, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(ProductInput{
		Code: "NO-STK", Name: "Sin stock", Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		ManageStock: false, IsRestaurant: true, BranchID: branchID, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	items, total, err := svc.ListReport(ProductListParams{
		RestaurantOnly:    true,
		NoManageStockOnly: true,
		ActiveOnly:        true,
		BranchID:          branchID,
		Limit:             50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d want 1", total, len(items))
	}
	if items[0].Code != "NO-STK" {
		t.Fatalf("got %q want NO-STK", items[0].Code)
	}
}

func TestProductCreate_ManageStockTruePersistsInDB(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)

	p, err := svc.Create(ProductInput{
		Code:               "TST-WITH-STOCK",
		Name:               "Con control stock",
		Type:               "product",
		Unit:               "NIU",
		SalePrice:          15,
		TaxRate:            18,
		IgvAffectationType: "10",
		ManageStock:        true,
		Active:             true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !p.ManageStock {
		t.Fatalf("ManageStock en memoria: got false, want true")
	}

	var loaded database.TenantProduct
	if err := db.First(&loaded, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !loaded.ManageStock {
		t.Fatalf("manage_stock en BD: got false, want true")
	}
}

func TestProductCreate_NonRestaurantClearsPreparationArea(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)

	p, err := svc.Create(ProductInput{
		Code: "ERP-1", Name: "Producto ERP", Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		IsRestaurant: false, PreparationArea: "cocina", Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.PreparationArea != "" {
		t.Fatalf("PreparationArea=%q want empty", p.PreparationArea)
	}
}

func TestProductCreate_ManageStockFalseClearsMinStock(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)

	p, err := svc.Create(ProductInput{
		Code: "NO-MIN", Name: "Sin min", Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		ManageStock: false, MinStock: 5, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.MinStock != 0 {
		t.Fatalf("MinStock=%v want 0", p.MinStock)
	}
}

func TestProductUpdate_DemoteRestaurantClearsPreparationArea(t *testing.T) {
	db := setupProductServiceTestDB(t)
	svc := NewProductService(db)

	p, err := svc.Create(ProductInput{
		Code: "REST-1", Name: "Plato", Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		IsRestaurant: true, PreparationArea: "bar", BranchID: 1, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(p.ID, ProductInput{
		Code: p.Code, Name: p.Name, Type: "product", Unit: "NIU",
		SalePrice: 10, TaxRate: 18, IgvAffectationType: "10",
		IsRestaurant: false, PreparationArea: "bar", ManageStock: true,
	}); err != nil {
		t.Fatal(err)
	}
	var loaded database.TenantProduct
	if err := db.First(&loaded, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.PreparationArea != "" {
		t.Fatalf("PreparationArea=%q want empty after demote", loaded.PreparationArea)
	}
}
