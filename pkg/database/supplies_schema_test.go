package database_test

import (
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Una empresa nueva (AutoMigrate del modelo) debe quedar igual que una actualizada por la V195: sin vacíos en la marca de insumo.
func TestEmpresaNueva_marcaDeInsumoNoAdmiteVacio(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:supplies_schema?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantProduct{}); err != nil {
		t.Fatal(err)
	}
	columns, err := db.Migrator().ColumnTypes(&database.TenantProduct{})
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, c := range columns {
		if c.Name() != "is_supply" && c.Name() != "supply_kind" {
			continue
		}
		checked++
		if nullable, ok := c.Nullable(); !ok || nullable {
			t.Errorf("%s admite vacío", c.Name())
		}
	}
	if checked != 2 {
		t.Fatalf("columnas revisadas = %d, quería 2", checked)
	}
}
