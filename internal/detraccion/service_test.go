package detraccion

import (
	"strings"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
	sunatdet "tukifac/pkg/sunat/detraccion"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupDetraccionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantSaleDetraccion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// transporteSaleInput reproduce el caso oficial de SUNAT (Lima-Casma, S/2,000 incluido IGV,
// valor referencial S/1,232.28) usado también en pkg/sunat/detraccion/calculator_test.go.
func transporteSaleInput() *SaleInput {
	return &SaleInput{
		GoodCode:               "027",
		ValorReferencialPen:    1232.28,
		MtcRegistro:            "15X123CNG",
		ConfiguracionVehicular: "C3",
		PuntoOrigen:            "AV. INDUSTRIAL 578 - LIMA - LIMA - LIMA",
		PuntoDestino:           "JR. BUENAVISTA 234 - ANCASH - CASMA - CASMA",
		CargaEfectivaTm:        12,
		CargaUtilTm:            15,
		TripDetail:             "Transporte de cemento en bolsas, viaje directo sin trasbordo",
	}
}

func TestPersist1004_GuardaCamposDeTransporte(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)

	row, err := svc.Persist(PersistInput{
		SaleID:            1,
		OperationTypeCode: sunatdet.OpDetraccionTransporte,
		SunatDocCode:      "01",
		Currency:          "PEN",
		SaleTotal:         2000,
		BankAccount:       "0004-1234567890",
		Detraccion:        transporteSaleInput(),
	})
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if row.OperationTypeCode != sunatdet.OpDetraccionTransporte {
		t.Errorf("operation_type_code = %q, want %q", row.OperationTypeCode, sunatdet.OpDetraccionTransporte)
	}
	if row.DetractionAmountPen != 80 {
		t.Errorf("detraction_amount_pen = %v, want 80 (caso oficial SUNAT)", row.DetractionAmountPen)
	}
	if row.ValorReferencialPen == nil || *row.ValorReferencialPen != 1232.28 {
		t.Errorf("valor_referencial_pen = %v, want 1232.28", row.ValorReferencialPen)
	}
	if row.MtcRegistro != "15X123CNG" || row.ConfiguracionVehicular != "C3" {
		t.Errorf("datos de transporte no persistidos: mtc=%q config=%q", row.MtcRegistro, row.ConfiguracionVehicular)
	}
	if row.CargaEfectivaTm == nil || *row.CargaEfectivaTm != 12 {
		t.Errorf("carga_efectiva_tm = %v, want 12", row.CargaEfectivaTm)
	}
	if row.TripDetail != transporteSaleInput().TripDetail {
		t.Errorf("trip_detail no persistido: %q", row.TripDetail)
	}

	// Releer de la BD, no solo el valor en memoria devuelto por Persist.
	var reloaded database.TenantSaleDetraccion
	if err := db.First(&reloaded, "sale_id = ?", 1).Error; err != nil {
		t.Fatalf("releer fila: %v", err)
	}
	if reloaded.PuntoOrigen != transporteSaleInput().PuntoOrigen {
		t.Errorf("punto_origen no persistió correctamente: %q", reloaded.PuntoOrigen)
	}
}

// Decisión de negocio (ver evaluate): el valor referencial es una captura manual sin tabla oficial
// que la respalde en este sistema, y NUNCA debe usarse para calcular la detracción — ni siquiera
// cuando el usuario lo ingresa y es mayor al importe de la venta. Solo es informativo/impreso. Esto
// difiere a propósito de calculator.go (que sí implementa la comparación "el mayor" de la R.S.
// 073-2006-SUNAT Art. 4 como primitiva reutilizable, ver TestEvaluateDetraccion1004_ValorReferencialMayor)
// — a nivel de servicio, deliberadamente no se invoca esa comparación.
func TestPersist1004_ValorReferencialNuncaAumentaLaDetraccion(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)

	in := transporteSaleInput()
	in.ValorReferencialPen = 999999 // muy por encima del importe de la venta (2000)

	row, err := svc.Persist(PersistInput{
		SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionTransporte, SunatDocCode: "01",
		Currency: "PEN", SaleTotal: 2000, BankAccount: "0004-123",
		Detraccion: in,
	})
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if row.DetractionAmountPen != 80 {
		t.Errorf("un valor referencial alto no debe aumentar la detracción: detraction_amount_pen = %v, want 80 (4%% de la venta real)", row.DetractionAmountPen)
	}
	if row.ValorReferencialPen == nil || *row.ValorReferencialPen != 999999 {
		t.Errorf("el valor referencial sí debe persistirse para mostrarse en el PDF, aunque no afecte el cálculo: %v", row.ValorReferencialPen)
	}
}

// Origen y destino son los únicos campos obligatorios de 1004: lo mínimo para identificar el
// servicio. Verifica cada uno por separado para no depender de qué validación corre primero.
func TestPersist1004_RechazaSinCamposObligatorios(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)

	base := func() PersistInput {
		return PersistInput{
			SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionTransporte, SunatDocCode: "01",
			Currency: "PEN", SaleTotal: 2000, BankAccount: "0004-123",
		}
	}

	cases := []struct {
		name   string
		mutate func(*SaleInput)
	}{
		{"sin punto origen", func(s *SaleInput) { s.PuntoOrigen = "" }},
		{"sin punto destino", func(s *SaleInput) { s.PuntoDestino = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base()
			s := transporteSaleInput()
			c.mutate(s)
			in.Detraccion = s
			if _, err := svc.Persist(in); err == nil {
				t.Fatalf("%s: esperaba error", c.name)
			}
		})
	}
}

// Registro MTC, configuración vehicular, carga efectiva, carga útil, detalle del viaje y valor
// referencial son opcionales: ninguno es obligatorio en el esquema UBL 2.1 de SUNAT
// (cac:AdditionalItemProperty tiene cardinalidad 0..n), y calcular el valor referencial real
// exige la tabla oficial del MTC (D.S. 020-2021-MTC) que este sistema no tiene integrada — la
// propia norma permite usar solo el importe de la operación cuando no hay valor referencial
// determinable (ver validateTransporteFields). Persist debe aceptar la venta sin ellos, calcular
// la detracción sobre el importe de la venta, y el comprobante no debe llevar AdditionalItemProperty
// vacíos ni en "0.00" para los códigos correspondientes.
func TestPersist1004_CamposDescriptivosSonOpcionales(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)

	in := transporteSaleInput()
	in.MtcRegistro = ""
	in.ConfiguracionVehicular = ""
	in.CargaEfectivaTm = 0
	in.CargaUtilTm = 0
	in.TripDetail = ""
	in.ValorReferencialPen = 0

	row, err := svc.Persist(PersistInput{
		SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionTransporte, SunatDocCode: "01",
		Currency: "PEN", SaleTotal: 2000, BankAccount: "0004-123",
		Detraccion: in,
	})
	if err != nil {
		t.Fatalf("Persist no debía rechazar la venta sin los campos descriptivos opcionales: %v", err)
	}
	if row.CargaEfectivaTm != nil || row.CargaUtilTm != nil || row.ValorReferencialPen != nil {
		t.Errorf("valor referencial/carga efectiva/útil sin completar deben quedar nil, got vr=%v ce=%v cu=%v", row.ValorReferencialPen, row.CargaEfectivaTm, row.CargaUtilTm)
	}
	if row.DetractionAmountPen != 80 {
		t.Errorf("sin valor referencial, la detracción debe calcularse sobre el importe de la venta: detraction_amount_pen = %v, want 80 (4%% de 2000)", row.DetractionAmountPen)
	}

	payload := &facturador.InvoicePayload{
		Details: []facturador.InvoiceDetail{{Descripcion: "Servicio de transporte de carga"}},
	}
	ApplyToInvoicePayload(payload, row)
	for _, a := range payload.Details[0].Atributos {
		if a.Code == "3006" || a.Code == "3007" || a.Code == "3010" || a.Code == "3013" || a.Code == "3014" {
			t.Errorf("no debía enviarse AdditionalItemProperty %s vacío, got %+v", a.Code, a)
		}
	}
	if len(payload.Details[0].Atributos) != 2 {
		t.Errorf("esperaba 2 AdditionalItemProperty (origen, destino), got %d", len(payload.Details[0].Atributos))
	}
}

// El código de bien debe ser 027 — igual que ya cubre calculator_test.go, pero acá verificando
// que el rechazo se propague hasta Persist (no solo Evaluate).
func TestPersist1004_RechazaCodigoDistintoDe027(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)
	in := PersistInput{
		SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionTransporte, SunatDocCode: "01",
		Currency: "PEN", SaleTotal: 2000, BankAccount: "0004-123",
	}
	s := transporteSaleInput()
	s.GoodCode = "014"
	in.Detraccion = s
	if _, err := svc.Persist(in); err == nil {
		t.Fatal("esperaba error: 1004 exige el código 027")
	}
}

func TestApplyToInvoicePayload_1004(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)
	if _, err := svc.Persist(PersistInput{
		SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionTransporte, SunatDocCode: "01",
		Currency: "PEN", SaleTotal: 2000, BankAccount: "0004-1234567890",
		Detraccion: transporteSaleInput(),
	}); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	row, err := svc.LoadBySaleID(1)
	if err != nil || row == nil {
		t.Fatalf("LoadBySaleID: %v", err)
	}

	payload := &facturador.InvoicePayload{
		Details: []facturador.InvoiceDetail{
			{Descripcion: "Servicio de transporte de carga Lima-Casma"},
		},
	}
	ApplyToInvoicePayload(payload, row)

	if payload.TipoOperacion != sunatdet.OpDetraccionTransporte {
		t.Errorf("tipoOperacion = %q, want %q (no debe quedar hardcodeado a 1001)", payload.TipoOperacion, sunatdet.OpDetraccionTransporte)
	}
	if payload.Detraccion == nil || payload.Detraccion.CodBienDetraccion != "027" {
		t.Fatalf("detraccion no aplicada correctamente: %+v", payload.Detraccion)
	}

	var legendFound bool
	for _, l := range payload.Legends {
		if l.Code == "2006" {
			legendFound = true
			if l.Value != sunatdet.Legend2006TextTransporte {
				t.Errorf("leyenda 2006 = %q, want el texto de transporte", l.Value)
			}
		}
	}
	if !legendFound {
		t.Fatal("falta la leyenda 2006")
	}

	if len(payload.Details) != 1 || len(payload.Details[0].Atributos) != 7 {
		t.Fatalf("esperaba 7 AdditionalItemProperty en el ítem, got %d", len(payload.Details[0].Atributos))
	}
	codes := map[string]string{}
	for _, a := range payload.Details[0].Atributos {
		codes[a.Code] = a.Value
	}
	want := map[string]string{
		"3006": "15X123CNG",
		"3007": "C3",
		"3008": "AV. INDUSTRIAL 578 - LIMA - LIMA - LIMA",
		"3009": "JR. BUENAVISTA 234 - ANCASH - CASMA - CASMA",
		"3010": "1232.28",
		"3013": "12.00",
		"3014": "15.00",
	}
	for code, val := range want {
		if codes[code] != val {
			t.Errorf("AdditionalItemProperty %s = %q, want %q", code, codes[code], val)
		}
	}
	// Detalle del viaje es solo referencia interna (impresión) — no existe un nodo UBL equivalente
	// en el payload de factura de este sistema (ver TripDetail en migrations.go), así que no debe
	// aparecer entre los AdditionalItemProperty enviados a SUNAT.
	for _, a := range payload.Details[0].Atributos {
		if strings.Contains(a.Value, "Transporte de cemento") {
			t.Errorf("el detalle del viaje no debe enviarse como AdditionalItemProperty: %+v", a)
		}
	}
}

// 1001 no debe verse afectado: sin Atributos, leyenda estándar, tipoOperacion=1001.
func TestApplyToInvoicePayload_1001_SinCambios(t *testing.T) {
	db := setupDetraccionTestDB(t)
	svc := NewService(db)
	if _, err := svc.Persist(PersistInput{
		SaleID: 1, OperationTypeCode: sunatdet.OpDetraccionGeneral, SunatDocCode: "01",
		Currency: "PEN", GravadoTotal: 1180, SaleTotal: 1180, BankAccount: "0004-1234567890",
		Detraccion: &SaleInput{GoodCode: "014"},
	}); err != nil {
		t.Fatalf("Persist 1001: %v", err)
	}
	row, err := svc.LoadBySaleID(1)
	if err != nil || row == nil {
		t.Fatalf("LoadBySaleID: %v", err)
	}

	payload := &facturador.InvoicePayload{
		Details: []facturador.InvoiceDetail{{Descripcion: "Servicio administrativo"}},
	}
	ApplyToInvoicePayload(payload, row)

	if payload.TipoOperacion != sunatdet.OpDetraccionGeneral {
		t.Errorf("tipoOperacion = %q, want %q", payload.TipoOperacion, sunatdet.OpDetraccionGeneral)
	}
	if payload.Details[0].Atributos != nil {
		t.Errorf("1001 no debe agregar AdditionalItemProperty, got %+v", payload.Details[0].Atributos)
	}
	var legendValue string
	for _, l := range payload.Legends {
		if l.Code == "2006" {
			legendValue = l.Value
		}
	}
	if legendValue != sunatdet.Legend2006Text {
		t.Errorf("leyenda 2006 = %q, want el texto general (sin cambios respecto a antes)", legendValue)
	}
}

// Regresión de compatibilidad: filas guardadas ANTES de esta migración no tienen
// operation_type_code (columna nueva, default '1001' a nivel de esquema, pero el struct en Go
// vía Save() escribiría ” si no se setea explícitamente — este test simula justamente ese caso
// leyendo una fila con el campo vacío) y deben seguir tratándose como 1001.
func TestApplyToInvoicePayload_FilaAntiguaSinOperationTypeCode(t *testing.T) {
	db := setupDetraccionTestDB(t)
	old := database.TenantSaleDetraccion{
		SaleID: 1, GoodCode: "014", PaymentMethodCode: "001", BankAccount: "0004-123",
		RatePercent: 4, BaseAmountPen: 1000, DetractionAmountPen: 40, InvoiceTotalPen: 1000,
		NetPayablePen: 960, BnConfirmationStatus: "pending",
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	// Forzar operation_type_code vacío directamente en la BD, simulando una fila pre-migración
	// (el default de columna solo aplica en el INSERT del motor real; en SQLite de test el
	// struct ya trae el default de Go 'not null;default:1001' vía GORM al crear, así que hay que
	// pisarlo a mano para simular el escenario real).
	if err := db.Model(&database.TenantSaleDetraccion{}).Where("sale_id = ?", 1).
		Update("operation_type_code", "").Error; err != nil {
		t.Fatal(err)
	}

	var row database.TenantSaleDetraccion
	if err := db.First(&row, "sale_id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.OperationTypeCode != "" {
		t.Fatalf("precondición: esperaba operation_type_code vacío, got %q", row.OperationTypeCode)
	}

	payload := &facturador.InvoicePayload{Details: []facturador.InvoiceDetail{{}}}
	ApplyToInvoicePayload(payload, &row)
	if payload.TipoOperacion != sunatdet.OpDetraccionGeneral {
		t.Errorf("fila sin operation_type_code debe caer a 1001 (compat), got %q", payload.TipoOperacion)
	}
	if strings.TrimSpace(payload.Detraccion.CodBienDetraccion) != "014" {
		t.Errorf("detraccion no aplicada: %+v", payload.Detraccion)
	}
}
