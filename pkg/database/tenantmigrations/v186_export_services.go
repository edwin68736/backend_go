package tenantmigrations

import (
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V186ExportServices habilita la exportación de servicios (operaciones 0201, 0202 y 0208): crea tenant_sale_exports (datos de la
// exportación por venta) y agrega tenant_company_configs.exports_enabled (apagado por defecto). Idempotente; no toca datos existentes.
type V186ExportServices struct{}

func (V186ExportServices) Version() int { return 186 }
func (V186ExportServices) Name() string { return "export_services" }

func (V186ExportServices) Up(db *gorm.DB) error {
	return db.AutoMigrate(&database.TenantSaleExport{}, &v186CompanyConfig{})
}
