package database

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSeedInventoryDocumentSeriesForBranch_MultipleBranches(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&TenantBranch{}, &TenantDocumentSeries{}); err != nil {
		t.Fatal(err)
	}
	b1 := TenantBranch{Name: "Principal", Active: true, IsMain: true}
	b2 := TenantBranch{Name: "Sucursal 2", Active: true}
	if err := db.Create(&b1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b2).Error; err != nil {
		t.Fatal(err)
	}
	if err := SeedInventoryDocumentSeriesForBranch(db, b1.ID); err != nil {
		t.Fatal(err)
	}
	if err := SeedInventoryDocumentSeriesForBranch(db, b2.ID); err != nil {
		t.Fatal(err)
	}
	for _, branchID := range []uint{b1.ID, b2.ID} {
		for _, code := range []string{"ING001", "EGR001"} {
			var n int64
			db.Model(&TenantDocumentSeries{}).
				Where("branch_id = ? AND category = ? AND series = ?", branchID, "almacen", code).
				Count(&n)
			if n != 1 {
				t.Fatalf("branch %d series %s count=%d want 1", branchID, code, n)
			}
		}
	}
	var total int64
	db.Model(&TenantDocumentSeries{}).Where("category = ?", "almacen").Count(&total)
	if total != 4 {
		t.Fatalf("almacen series total=%d want 4", total)
	}
}
