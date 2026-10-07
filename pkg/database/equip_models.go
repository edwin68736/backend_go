package database

import "time"

// Módulo «Gestión de Equipos» (panel central, exclusivo del dueño): venta y despacho de equipos POS.
// Todas las tablas viven en la BD CENTRAL con prefijo equip_ y las crea MigrateCentral (AutoMigrate);
// no hay nada en las bases de los tenants. Ver PROPUESTA_MODULO_GESTION_EQUIPOS_PANEL_CENTRAL.md.

// Tipos de producto del catálogo de equipos.
const (
	EquipKindEquipo     = "equipo"
	EquipKindConsumible = "consumible" // rollos de papel
	EquipKindEtiqueta   = "etiqueta"
	EquipKindAccesorio  = "accesorio" // lectores, gavetas…
)

// Tipo de salida de un pedido (se propaga a sus movimientos de stock).
const (
	EquipSaleIndependiente = "independiente"
	EquipSalePromoTK       = "promo_tk"
)

// Tipos de movimiento del kardex.
const (
	EquipMoveApertura   = "apertura"          // stock inicial del período (+)
	EquipMoveIngreso    = "ingreso"           // reposición / compra (+)
	EquipMoveSalida     = "salida_pedido"     // venta (−)
	EquipMoveReenvio    = "salida_reenvio"    // nuevo envío tras un retorno (−)
	EquipMoveReingreso  = "reingreso_retorno" // equipo devuelto en buen estado (+)
	EquipMoveAjuste     = "ajuste"            // corrección con nota (±)
	EquipMoveBaja       = "baja"              // equipo dañado/perdido (−)
	EquipPeriodLayoutYM = "2006-01"
)

// EquipProduct catálogo de equipos, consumibles y accesorios.
type EquipProduct struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	Code           string  `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Name           string  `gorm:"size:150;not null" json:"name"`
	Kind           string  `gorm:"size:20;not null;default:'equipo'" json:"kind"`
	Notes          string  `gorm:"size:255" json:"notes"`
	ReferencePrice float64 `gorm:"type:decimal(15,2);default:0" json:"reference_price"`
	// Umbrales del semáforo: stock >= GreenThreshold = suficiente, >= YellowThreshold = moderado, si no = bajo.
	YellowThreshold int       `gorm:"default:0" json:"yellow_threshold"`
	GreenThreshold  int       `gorm:"default:0" json:"green_threshold"`
	Active          bool      `gorm:"default:true" json:"active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (EquipProduct) TableName() string { return "equip_products" }

// EquipCombo combo de venta: no tiene stock propio, descuenta sus componentes.
type EquipCombo struct {
	ID             uint             `gorm:"primaryKey" json:"id"`
	Code           string           `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Name           string           `gorm:"size:150;not null" json:"name"`
	ReferencePrice float64          `gorm:"type:decimal(15,2);default:0" json:"reference_price"`
	Active         bool             `gorm:"default:true" json:"active"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	Items          []EquipComboItem `gorm:"foreignKey:ComboID" json:"items,omitempty"`
}

func (EquipCombo) TableName() string { return "equip_combos" }

// EquipComboItem componente de un combo.
type EquipComboItem struct {
	ID        uint `gorm:"primaryKey" json:"id"`
	ComboID   uint `gorm:"not null;index" json:"combo_id"`
	ProductID uint `gorm:"not null;index" json:"product_id"`
	Quantity  int  `gorm:"not null;default:1" json:"quantity"`
}

func (EquipComboItem) TableName() string { return "equip_combo_items" }

// EquipCustomer cliente reutilizable (con historial de cobros). «Clientes varios» no lleva documento.
type EquipCustomer struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"size:200;not null" json:"name"`
	DocType    string    `gorm:"size:10;uniqueIndex:ux_equip_customer_doc,priority:1" json:"doc_type"` // RUC | DNI | CE | SIN_DOC
	DocNumber  *string   `gorm:"size:20;uniqueIndex:ux_equip_customer_doc,priority:2" json:"doc_number"`
	ContactDNI string    `gorm:"size:20" json:"contact_dni"`
	Phone      string    `gorm:"size:50" json:"phone"`
	PhoneKind  string    `gorm:"size:12;default:'whatsapp'" json:"phone_kind"` // whatsapp | usuario
	TenantID   *uint     `gorm:"index" json:"tenant_id"`
	Notes      string    `gorm:"size:255" json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (EquipCustomer) TableName() string { return "equip_customers" }

// EquipOrder cabecera del pedido (una fila del «Control de Envíos»).
type EquipOrder struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	OrderNumber int    `gorm:"not null;uniqueIndex" json:"order_number"`
	SaleType    string `gorm:"size:20;not null;default:'independiente'" json:"sale_type"`
	// Fechas: cada hito tiene la suya (ver §5.5 de la propuesta).
	OrderDate    time.Time  `gorm:"type:date;index" json:"order_date"`
	RegisteredAt time.Time  `json:"registered_at"`
	ConfirmedAt  *time.Time `json:"confirmed_at"`
	ValidatedAt  *time.Time `json:"validated_at"`
	IsGift       bool       `gorm:"default:false" json:"is_gift"`
	CustomerID   *uint      `gorm:"index" json:"customer_id"`
	// Copia (snapshot) del cliente al momento del pedido.
	CustomerName      string           `gorm:"size:200" json:"customer_name"`
	CustomerDocType   string           `gorm:"size:10" json:"customer_doc_type"`
	CustomerDocNumber string           `gorm:"size:20" json:"customer_doc_number"`
	ContactDNI        string           `gorm:"size:20" json:"contact_dni"`
	CustomerPhone     string           `gorm:"size:50" json:"customer_phone"`
	BillingDocType    string           `gorm:"size:12;default:'ninguno'" json:"billing_doc_type"` // boleta | factura | ninguno
	TotalAmount       float64          `gorm:"type:decimal(15,2);default:0" json:"total_amount"`
	PaidAmount        float64          `gorm:"type:decimal(15,2);default:0" json:"paid_amount"`
	BalanceAmount     float64          `gorm:"type:decimal(15,2);default:0" json:"balance_amount"`
	PaymentStatus     string           `gorm:"size:12;default:'pendiente';index" json:"payment_status"`               // pendiente | parcial | pagado | no_pago
	ValidationStatus  string           `gorm:"size:24;default:'pendiente_validacion';index" json:"validation_status"` // pendiente_validacion | validado | observado
	ValidatedBy       *uint            `json:"validated_by"`
	ValidationNotes   string           `gorm:"size:500" json:"validation_notes"`
	Status            string           `gorm:"size:12;default:'borrador';index" json:"status"` // borrador | registrado | anulado
	Notes             string           `gorm:"size:500" json:"notes"`
	ImportBatchID     *uint            `gorm:"index" json:"import_batch_id"`
	CreatedBy         *uint            `json:"created_by"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	Items             []EquipOrderItem `gorm:"foreignKey:OrderID" json:"items,omitempty"`
}

func (EquipOrder) TableName() string { return "equip_orders" }

// EquipOrderItem línea del pedido: producto, combo, plan de Tukifac u otro concepto.
type EquipOrderItem struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	OrderID     uint    `gorm:"not null;index" json:"order_id"`
	LineNo      int     `gorm:"not null;default:1" json:"line_no"`
	LineType    string  `gorm:"size:12;not null;default:'producto'" json:"line_type"` // producto | combo | plan | otro
	ProductID   *uint   `gorm:"index" json:"product_id"`
	ComboID     *uint   `gorm:"index" json:"combo_id"`
	SaasPlanID  *uint   `json:"saas_plan_id"`
	Description string  `gorm:"size:200" json:"description"`
	PlanMonths  int     `gorm:"default:0" json:"plan_months"`
	Quantity    float64 `gorm:"type:decimal(15,3);not null;default:1" json:"quantity"`
	UnitPrice   float64 `gorm:"type:decimal(15,2);default:0" json:"unit_price"`
	Subtotal    float64 `gorm:"type:decimal(15,2);default:0" json:"subtotal"`
	IsCourtesy  bool    `gorm:"default:false" json:"is_courtesy"`
	// ComboSnapshotJSON composición vigente al vender: [{product_id, code, quantity}].
	ComboSnapshotJSON string `gorm:"type:text" json:"combo_snapshot_json"`
	Notes             string `gorm:"size:255" json:"notes"`
}

func (EquipOrderItem) TableName() string { return "equip_order_items" }

// EquipPayment cobro: dinero que entra, a nombre del cliente. Forma su historial de pagos.
type EquipPayment struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	CustomerID        *uint     `gorm:"index" json:"customer_id"`
	Amount            float64   `gorm:"type:decimal(15,2);not null" json:"amount"`
	PaidAt            time.Time `gorm:"index" json:"paid_at"`
	Method            string    `gorm:"size:20;default:'otro'" json:"method"`       // yape | plin | efectivo | transferencia | deposito | otro
	Moment            string    `gorm:"size:14;default:'anticipado'" json:"moment"` // anticipado | al_recoger
	Reference         string    `gorm:"size:120" json:"reference"`
	InvoiceDocType    string    `gorm:"size:12" json:"invoice_doc_type"`
	InvoiceSeries     string    `gorm:"size:10" json:"invoice_series"`
	InvoiceNumber     string    `gorm:"size:20" json:"invoice_number"`
	UnallocatedAmount float64   `gorm:"type:decimal(15,2);default:0" json:"unallocated_amount"`
	Status            string    `gorm:"size:10;default:'vigente'" json:"status"` // vigente | anulado
	Notes             string    `gorm:"size:255" json:"notes"`
	ImportBatchID     *uint     `gorm:"index" json:"import_batch_id"`
	CreatedBy         *uint     `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
}

func (EquipPayment) TableName() string { return "equip_payments" }

// EquipPaymentAllocation a qué pedido se aplica (parte de) un cobro.
type EquipPaymentAllocation struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	PaymentID uint    `gorm:"not null;index" json:"payment_id"`
	OrderID   uint    `gorm:"not null;index" json:"order_id"`
	Amount    float64 `gorm:"type:decimal(15,2);not null" json:"amount"`
}

func (EquipPaymentAllocation) TableName() string { return "equip_payment_allocations" }

// EquipCarrier transportista (Shalom, Olva…): sus días de despacho y plazo de recojo son propios.
type EquipCarrier struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Code           string    `gorm:"size:30;not null;uniqueIndex" json:"code"`
	Name           string    `gorm:"size:100;not null" json:"name"`
	DispatchDays   string    `gorm:"size:20" json:"dispatch_days"` // días de la semana 0=dom..6=sáb, separados por coma: "1,3,5"
	PickupDays     int       `gorm:"default:15" json:"pickup_days"`
	GuideLabel     string    `gorm:"size:60;default:'N° de guía'" json:"guide_label"`
	GuideFormat    string    `gorm:"size:120" json:"guide_format"`          // expresión regular opcional para validar la guía
	TrackingURLTpl string    `gorm:"size:255" json:"tracking_url_template"` // con {guia}
	LabelTemplate  string    `gorm:"size:60" json:"label_template"`
	IsDefault      bool      `gorm:"default:false" json:"is_default"`
	Active         bool      `gorm:"default:true" json:"active"`
	SortOrder      int       `gorm:"default:0" json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (EquipCarrier) TableName() string { return "equip_carriers" }

// EquipShipment envío de un pedido (un reenvío tras un retorno crea otro).
type EquipShipment struct {
	ID                    uint       `gorm:"primaryKey" json:"id"`
	OrderID               uint       `gorm:"not null;index" json:"order_id"`
	CarrierID             *uint      `gorm:"index" json:"carrier_id"`
	GuideNumber           string     `gorm:"size:40;index" json:"guide_number"`
	DestinationAgency     string     `gorm:"size:150" json:"destination_agency"`
	DestinationDepartment string     `gorm:"size:80" json:"destination_department"`
	DestinationProvince   string     `gorm:"size:80" json:"destination_province"`
	DestinationDistrict   string     `gorm:"size:80" json:"destination_district"`
	Ubigeo                string     `gorm:"size:6" json:"ubigeo"`
	DeliveryMode          string     `gorm:"size:20;default:'agencia'" json:"delivery_mode"` // agencia | oficina | pendiente_recojo
	ScheduledDispatchDate *time.Time `gorm:"type:date" json:"scheduled_dispatch_date"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	ArrivedAt             *time.Time `json:"arrived_at"`
	PickupDeadline        *time.Time `json:"pickup_deadline"`
	PickedUpAt            *time.Time `json:"picked_up_at"`
	Status                string     `gorm:"size:16;default:'pendiente_envio';index" json:"status"` // pendiente_envio | en_transito | en_agencia | entregado | retorno
	IsCurrent             bool       `gorm:"default:true" json:"is_current"`
	LabelPrintedAt        *time.Time `json:"label_printed_at"`
	// FreightCost costo del flete de este envío (opcional) para la utilidad.
	FreightCost float64   `gorm:"type:decimal(15,2);default:0" json:"freight_cost"`
	Notes       string    `gorm:"size:255" json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (EquipShipment) TableName() string { return "equip_shipments" }

// EquipReturn retorno de equipos no recogidos. Puede referir a pedidos de meses anteriores que no
// están cargados, por eso pedido y envío son opcionales y se guarda el nombre del cliente.
type EquipReturn struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ReturnNumber  int        `gorm:"index" json:"return_number"`
	OrderID       *uint      `gorm:"index" json:"order_id"`
	ShipmentID    *uint      `gorm:"index" json:"shipment_id"`
	CustomerName  string     `gorm:"size:200" json:"customer_name"`
	GuideNumber   string     `gorm:"size:40" json:"guide_number"`
	RequestedAt   *time.Time `gorm:"type:date" json:"requested_at"`
	ItemsJSON     string     `gorm:"type:text" json:"items_json"` // [{product_id, code, quantity}]
	ItemsText     string     `gorm:"size:255" json:"items_text"`
	UnpaidBalance float64    `gorm:"type:decimal(15,2);default:0" json:"unpaid_balance"`
	ReturnCost    float64    `gorm:"type:decimal(15,2);default:0" json:"return_cost"`
	ReceivedAt    *time.Time `gorm:"type:date" json:"received_at"`
	Status        string     `gorm:"size:24;default:'solicitado'" json:"status"`     // solicitado | en_camino | recibido | desechado_por_agencia
	Condition     string     `gorm:"size:12;default:'buen_estado'" json:"condition"` // buen_estado | danado
	ActionTaken   string     `gorm:"size:12;default:'retorno'" json:"action_taken"`  // retorno | reenvio | reembolso | perdida
	Notes         string     `gorm:"size:500" json:"notes"`
	ImportBatchID *uint      `gorm:"index" json:"import_batch_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (EquipReturn) TableName() string { return "equip_returns" }

// EquipStockMovement kardex: cada entrada/salida de stock. Stock actual = SUM(quantity).
type EquipStockMovement struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProductID    uint      `gorm:"not null;index:idx_equip_mov_product_date,priority:1" json:"product_id"`
	OccurredAt   time.Time `gorm:"not null;index:idx_equip_mov_product_date,priority:2" json:"occurred_at"`
	MovementType string    `gorm:"size:20;not null;index" json:"movement_type"`
	Quantity     int       `gorm:"not null" json:"quantity"` // con signo
	// UnitCost costo unitario de compra (solo ingresos; opcional) para el costo promedio y la utilidad.
	UnitCost    *float64 `gorm:"type:decimal(15,4)" json:"unit_cost"`
	OrderID     *uint    `gorm:"index" json:"order_id"`
	OrderItemID *uint    `json:"order_item_id"`
	ReturnID    *uint    `json:"return_id"`
	ViaComboID  *uint    `json:"via_combo_id"`
	// SaleTypeSnapshot tipo de salida del pedido al momento del movimiento (columnas del Stock Maestro).
	SaleTypeSnapshot string    `gorm:"size:20" json:"sale_type_snapshot"`
	Note             string    `gorm:"size:255" json:"note"`
	ImportBatchID    *uint     `gorm:"index" json:"import_batch_id"`
	CreatedBy        *uint     `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
}

func (EquipStockMovement) TableName() string { return "equip_stock_movements" }

// EquipStockPeriod cierre mensual por producto (stock inicial/final congelado).
type EquipStockPeriod struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Period    string    `gorm:"size:7;not null;uniqueIndex:ux_equip_period_product,priority:1" json:"period"` // YYYY-MM
	ProductID uint      `gorm:"not null;uniqueIndex:ux_equip_period_product,priority:2" json:"product_id"`
	Opening   int       `json:"opening"`
	Closing   int       `json:"closing"`
	ClosedAt  time.Time `json:"closed_at"`
	ClosedBy  *uint     `json:"closed_by"`
}

func (EquipStockPeriod) TableName() string { return "equip_stock_periods" }

// EquipSettings fila única (id=1).
type EquipSettings struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	DefaultCarrierID *uint     `json:"default_carrier_id"`
	StockManager     string    `gorm:"size:100" json:"stock_manager"` // responsable de stock (hoy «Rosymar»)
	AlertYellowDays  int       `gorm:"default:5" json:"alert_yellow_days"`
	AlertRedDays     int       `gorm:"default:12" json:"alert_red_days"`
	NextOrderNumber  int       `gorm:"default:1" json:"next_order_number"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (EquipSettings) TableName() string { return "equip_settings" }

// EquipImportBatch lote de importación desde el Excel mensual (evita importar dos veces el mismo período).
type EquipImportBatch struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Period    string    `gorm:"size:7;not null;uniqueIndex" json:"period"` // YYYY-MM
	FileName  string    `gorm:"size:255" json:"file_name"`
	Products  int       `json:"products"`
	Combos    int       `json:"combos"`
	Orders    int       `json:"orders"`
	Payments  int       `json:"payments"`
	Returns   int       `json:"returns"`
	Movements int       `json:"movements"`
	CreatedBy *uint     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func (EquipImportBatch) TableName() string { return "equip_import_batches" }
