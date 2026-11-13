package tenantmigrations

// Estructuras MÍNIMAS de V180–V184: cada migración agrega solo sus columnas. Con el struct completo, GORM compara TODAS las columnas
// existentes de la tabla y puede emitir ALTER ... MODIFY (copia de tabla) sobre tablas con millones de filas. Mismo patrón que V129.
// Las etiquetas de cada columna son idénticas a las del struct completo en pkg/database/migrations.go.

type v182CashMovement struct {
	ID           uint     `gorm:"primaryKey"`
	Currency     string   `gorm:"size:10;default:'PEN'"`
	ExchangeRate *float64 `gorm:"type:decimal(10,4)"`
	AmountPEN    *float64 `gorm:"column:amount_pen;type:decimal(15,2)"`
}

func (v182CashMovement) TableName() string { return "tenant_cash_movements" }

type v182BankMovement struct {
	ID           uint     `gorm:"primaryKey"`
	Currency     string   `gorm:"size:10;default:'PEN'"`
	ExchangeRate *float64 `gorm:"type:decimal(10,4)"`
	AmountPEN    *float64 `gorm:"column:amount_pen;type:decimal(15,2)"`
}

func (v182BankMovement) TableName() string { return "tenant_bank_movements" }

type v183CashMovement struct {
	ID         uint  `gorm:"primaryKey"`
	TransferID *uint `gorm:"column:transfer_id;index"`
}

func (v183CashMovement) TableName() string { return "tenant_cash_movements" }

type v183BankMovement struct {
	ID            uint     `gorm:"primaryKey"`
	AccountAmount *float64 `gorm:"column:account_amount;type:decimal(15,2)"`
	TransferID    *uint    `gorm:"column:transfer_id;index"`
}

func (v183BankMovement) TableName() string { return "tenant_bank_movements" }

type v184BankAccount struct {
	ID     uint  `gorm:"primaryKey"`
	BankID *uint `gorm:"column:bank_id;index"`
}

func (v184BankAccount) TableName() string { return "tenant_bank_accounts" }

type v184PaymentMethod struct {
	ID                uint   `gorm:"primaryKey"`
	AppliesTo         string `gorm:"column:applies_to;size:10;default:'both'"`
	RequiresReference bool   `gorm:"column:requires_reference;default:false"`
}

func (v184PaymentMethod) TableName() string { return "tenant_payment_methods" }

type v180Purchase struct {
	ID              uint     `gorm:"primaryKey"`
	ExchangeRate    *float64 `gorm:"column:exchange_rate;type:decimal(10,4)"`
	PurchaseOrderID *uint    `gorm:"column:purchase_order_id;index"`
}

func (v180Purchase) TableName() string { return "tenant_purchases" }

// V187: país opcional del cliente (solo esa columna).
type v187Contact struct {
	ID      uint   `gorm:"primaryKey"`
	Country string `gorm:"column:country;size:2"`
}

func (v187Contact) TableName() string { return "tenant_contacts" }

// V186: interruptor de exportaciones en la configuración de la empresa (solo esa columna, sin MODIFY de las demás).
type v186CompanyConfig struct {
	ID             uint `gorm:"primaryKey"`
	ExportsEnabled bool `gorm:"column:exports_enabled;default:false"`
}

func (v186CompanyConfig) TableName() string { return "tenant_company_configs" }
