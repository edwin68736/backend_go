package tenantmigrations

import "gorm.io/gorm"

// V166POSCostAndPricesPermissions agrega pos.view_cost y pos.view_prices (botones «Costo» y
// «Precios» de las tarjetas del POS). V165 se amplió con ellos después de haberse aplicado en
// algunas bases, así que esta versión vuelve a correr su Up, que es idempotente (FirstOrCreate de
// los permisos y grantIfMissing por rol), para completar lo que faltaba.
type V166POSCostAndPricesPermissions struct{}

func (V166POSCostAndPricesPermissions) Version() int { return 166 }
func (V166POSCostAndPricesPermissions) Name() string { return "pos_cost_and_prices_permissions" }

func (V166POSCostAndPricesPermissions) Up(db *gorm.DB) error {
	return V165POSStockAndProductCostPermissions{}.Up(db)
}
