package service

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	cashbanksvc "tukifac/internal/cashbank/service"
	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// P0-2: el reporte de cierre de caja (GetSessionReport) contaba dos veces una nota de venta convertida: la NV y el comprobante emitido desde
// ella heredan la misma sesión de caja. Estos tests usan el flujo real de conversión (IssueElectronicFromNota), que es el que copia los pagos
// y asigna la sesión, y leen el reporte real. Por defecto corren en SQLite; con TUKIFAC_TEST_MYSQL_DSN (una BD de pruebas desechable, modo
// estricto recomendado: &sql_mode='STRICT_ALL_TABLES') corren contra MySQL/MariaDB, que es la validación que cuenta.

var sessionReportModels = func() []interface{} {
	return []interface{}{
		&database.TenantCompanyConfig{}, &database.TenantDocumentSeries{}, &database.TenantSale{}, &database.TenantSaleItem{},
		&database.TenantSalePayment{}, &database.TenantSaleCreditInstallment{}, &database.TenantSaleDetraccion{},
		&database.TenantCashSession{}, &database.TenantCashMovement{}, &database.TenantPaymentMethod{}, &database.TenantBankAccount{},
		&database.TenantBankMovement{}, &database.TenantPurchase{}, &database.TenantBranch{}, &database.TenantUser{},
		&database.TenantAccountTransfer{}, &database.TenantBank{},
	}
}

func openSessionReportDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN"); dsn != "" {
		db, err = gorm.Open(mysqldrv.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range sessionReportModels() { // BD desechable: arranca limpia en cada test
			if err := db.Migrator().DropTable(m); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		db, err = gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range sessionReportModels() {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&database.TenantCompanyConfig{ID: 1, SunatEnabled: true, TaxRate: 18}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantPaymentMethod{Code: "cash", Name: "Efectivo", IsSystem: true, Active: true, DestinationType: "cash"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

type nvSpec struct {
	total     float64 // total de la NV (la cuota/abono se calcula contra él)
	paid      float64 // abono o pago real recibido en efectivo en la sesión de la NV (0 = nada)
	credit    bool    // venta a crédito
	recargo   float64 // importe de recargo al consumo ya cobrado en la NV (0 = sin recargo)
	noPayRows bool    // NV histórica sin ninguna fila de pago
}

type nvFixture struct {
	db        *gorm.DB
	cb        *cashbanksvc.CashBankService
	session   uint
	nvID      uint
	nvNumber  string
	seriesID  uint // serie de la boleta destino
	converted *database.TenantSale
}

// newNVInSession deja una NV real dentro de una sesión de caja abierta: sus pagos llevan la sesión y su efectivo ya está en los movimientos
// (igual que SaleService.Create en producción). Todavía no se convierte.
func newNVInSession(t *testing.T, spec nvSpec) *nvFixture {
	t.Helper()
	db := openSessionReportDB(t)
	cb := cashbanksvc.NewCashBankService(db)
	sess := &database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpenedAt: time.Now()}
	if err := db.Create(sess).Error; err != nil {
		t.Fatal(err)
	}
	nvSeries := database.TenantDocumentSeries{BranchID: 1, DocType: "NOTA DE VENTA", SunatCode: "00", Category: "venta", Series: "NV001", Correlative: 1, Active: true}
	boleta := database.TenantDocumentSeries{BranchID: 1, DocType: "BOLETA", SunatCode: "03", Category: "venta", Series: "B001", Correlative: 1, Active: true}
	if err := db.Create(&nvSeries).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&boleta).Error; err != nil {
		t.Fatal(err)
	}
	sid := sess.ID
	nv := database.TenantSale{
		BranchID: 1, UserID: 1, SeriesID: nvSeries.ID, DocType: "NOTA DE VENTA", Series: "NV001", Correlative: 1, Number: "NV001-00000001",
		IssueDate: time.Now().AddDate(0, 0, -1), Subtotal: spec.total / 1.18, TaxAmount: spec.total - spec.total/1.18, Total: spec.total,
		Currency: "PEN", CashSessionID: &sid, CreatedAt: time.Now(),
		Status: "paid", PaymentConditionCode: "cash", PaymentMethod: "cash",
	}
	if spec.credit {
		nv.Status, nv.PaymentConditionCode, nv.PaymentMethod = "credit", "credit", "credit"
	}
	if spec.recargo > 0 {
		nv.ServiceChargeRate, nv.ServiceChargeAmount = 10, spec.recargo
	}
	if err := db.Create(&nv).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantSaleItem{SaleID: nv.ID, Code: "P1", Description: "Producto", Unit: "NIU", Quantity: 1,
		UnitPrice: nv.Subtotal, Subtotal: nv.Subtotal, TaxAmount: nv.TaxAmount, Total: nv.Total, IgvAffectationType: "10"}).Error; err != nil {
		t.Fatal(err)
	}
	if spec.credit {
		if err := db.Create(&database.TenantSaleCreditInstallment{SaleID: nv.ID, InstallmentNo: 1, DueDate: time.Now().AddDate(0, 0, 15),
			Amount: spec.total, PaidAmount: spec.paid, Currency: "PEN", Status: "pending"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	paid := spec.paid
	if !spec.credit && paid == 0 {
		paid = spec.total
	}
	if paid > 0 && !spec.noPayRows {
		if err := db.Create(&database.TenantSalePayment{SaleID: nv.ID, Method: "cash", Amount: paid, CashSessionID: &sid, CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
		if err := cb.RecordPayment(db, "cash", paid, &sid, nv.Number, "Venta "+nv.Number, &nv.ID, 1); err != nil {
			t.Fatal(err)
		}
	}
	return &nvFixture{db: db, cb: cb, session: sess.ID, nvID: nv.ID, nvNumber: nv.Number, seriesID: boleta.ID}
}

func (f *nvFixture) convert(t *testing.T) *database.TenantSale {
	t.Helper()
	child, err := NewSaleService(f.db).IssueElectronicFromNota(f.nvID, f.seriesID, 1, "", 0, nil)
	if err != nil {
		t.Fatalf("conversión real: %v", err)
	}
	f.converted = child
	return child
}

func (f *nvFixture) report(t *testing.T, session uint) *cashbanksvc.SessionReport {
	t.Helper()
	r, err := f.cb.GetSessionReport(session)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var docNumberRe = regexp.MustCompile(`"doc_number":"[^"]*"`)

// moneyView lo que el reporte dice del dinero: no debe cambiar por convertir la NV (ver T2.4). El rótulo del documento SÍ cambia a propósito
// (la NV se muestra como "B001-… (desde NV001-…)", commit 5c9ae8f), por eso se compara sin él.
func moneyView(t *testing.T, r *cashbanksvc.SessionReport) string {
	t.Helper()
	b, err := json.Marshal(map[string]interface{}{
		"total_sales": r.Totals.TotalSales, "totals": r.Totals, "by_method": r.TotalsByMethod, "income": r.IncomeDetail, "physical": r.CashPhysical,
	})
	if err != nil {
		t.Fatal(err)
	}
	return docNumberRe.ReplaceAllString(string(b), `"doc_number":""`)
}

// T2.1: la NV cobró recargo al consumo; el comprobante lo copia y lo recalcula. El cierre debe informarlo una sola vez.
func TestSessionReportConverted_T21_recargoUnaSolaVez(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, recargo: 10})
	f.convert(t)
	r := f.report(t, f.session)
	if r.ServiceCharge.Sales != 1 || r.ServiceCharge.Total != 10 {
		t.Fatalf("recargo = %+v, quería Total=10 Sales=1 (la NV una sola vez)", r.ServiceCharge)
	}
}

// T2.2: NV a crédito de 118 con un abono real de 40: el crédito generado es 78 en una fila (antes: 156 en dos).
func TestSessionReportConverted_T22_creditoConAbono(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, credit: true, paid: 40})
	f.convert(t)
	r := f.report(t, f.session)
	if len(r.CreditGenerated.Sales) != 1 || r.CreditGenerated.Total != 78 {
		t.Fatalf("crédito generado = %.2f en %d fila(s), quería 78.00 en 1: %+v", r.CreditGenerated.Total, len(r.CreditGenerated.Sales), r.CreditGenerated.Sales)
	}
}

// T2.3: NV a crédito sin abono: una fila de 118, rotulada con el comprobante vigente y la NV de origen.
func TestSessionReportConverted_T23_creditoSinAbono(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, credit: true})
	child := f.convert(t)
	r := f.report(t, f.session)
	if len(r.CreditGenerated.Sales) != 1 || r.CreditGenerated.Total != 118 {
		t.Fatalf("crédito generado = %.2f en %d fila(s), quería 118.00 en 1", r.CreditGenerated.Total, len(r.CreditGenerated.Sales))
	}
	want := "(desde " + f.nvNumber + ")"
	if got := r.CreditGenerated.Sales[0].DocNumber; !strings.Contains(got, want) || !strings.Contains(got, child.Number) {
		t.Fatalf("rótulo = %q, quería el comprobante %q y %q", got, child.Number, want)
	}
}

// T2.4: invariantes de dinero. Convertir la NV no cambia lo cobrado ni el efectivo del reporte (contado, crédito con abono y con recargo).
func TestSessionReportConverted_T24_convertirNoCambiaElDinero(t *testing.T) {
	cases := map[string]nvSpec{
		"contado":             {total: 118},
		"credito con abono":   {total: 118, credit: true, paid: 40},
		"contado con recargo": {total: 118, recargo: 10},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			f := newNVInSession(t, spec)
			before := moneyView(t, f.report(t, f.session))
			f.convert(t)
			if after := moneyView(t, f.report(t, f.session)); after != before {
				t.Fatalf("convertir cambió el dinero del reporte:\nantes:   %s\ndespués: %s", before, after)
			}
		})
	}
}

// T2.5: comprobante anulado con la NV viva: la NV cuenta una sola vez.
func TestSessionReportConverted_T25_hijaAnuladaNVViva(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, credit: true, paid: 40})
	child := f.convert(t)
	if err := f.db.Model(&database.TenantSale{}).Where("id = ?", child.ID).Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	r := f.report(t, f.session)
	if len(r.CreditGenerated.Sales) != 1 || r.CreditGenerated.Total != 78 {
		t.Fatalf("crédito generado = %.2f en %d fila(s), quería 78.00 en 1", r.CreditGenerated.Total, len(r.CreditGenerated.Sales))
	}
}

// T2.6: cobro posterior sobre el comprobante en OTRA caja: la caja de origen informa el crédito de la NV una vez; la caja que cobró informa
// una sola fila de cobro de CxC, sin duplicados.
func TestSessionReportConverted_T26_cobroPosteriorEnOtraCaja(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, credit: true, paid: 40})
	child := f.convert(t)
	b := &database.TenantCashSession{BranchID: 1, UserID: 2, OpenedBy: 2, Status: "open", OpenedAt: time.Now()}
	if err := f.db.Create(b).Error; err != nil {
		t.Fatal(err)
	}
	bid := b.ID
	if err := f.db.Create(&database.TenantSalePayment{SaleID: child.ID, Method: "cash", Amount: 30, CashSessionID: &bid, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.cb.RecordPayment(f.db, "cash", 30, &bid, child.Number, "Cobro "+child.Number, &child.ID, 2); err != nil {
		t.Fatal(err)
	}

	a := f.report(t, f.session)
	if len(a.CreditGenerated.Sales) != 1 || a.CreditGenerated.Total != 78 {
		t.Fatalf("caja de origen: crédito generado = %.2f en %d fila(s), quería 78.00 en 1 (el cobro sobre la hija sigue sin descontarse: límite conocido)",
			a.CreditGenerated.Total, len(a.CreditGenerated.Sales))
	}
	rb := f.report(t, b.ID)
	var cobros int
	var total float64
	for _, row := range rb.IncomeDetail {
		if row.Type == "cobro_cxc" {
			cobros++
			total += row.Amount
		}
	}
	if cobros != 1 || total != 30 || rb.Totals.TotalSales != 30 || len(rb.CreditGenerated.Sales) != 0 {
		t.Fatalf("caja que cobró: %d cobro(s) por %.2f, TotalSales=%.2f, crédito=%d (quería 1 cobro_cxc por 30, sin crédito generado)",
			cobros, total, rb.Totals.TotalSales, len(rb.CreditGenerated.Sales))
	}
}

// T2.7: NV histórica sin ninguna fila de pago: se atribuye una sola vez como venta (bloque legacy), no también por su comprobante.
func TestSessionReportConverted_T27_nvLegacySinPagos(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 100, noPayRows: true})
	f.convert(t)
	r := f.report(t, f.session)
	var ventas int
	var total float64
	for _, row := range r.IncomeDetail {
		if row.Type == "venta" {
			ventas++
			total += row.Amount
		}
	}
	if ventas > 1 || total > 100.001 {
		t.Fatalf("la venta sin pagos se cuenta %d vez(es) por %.2f, quería a lo sumo 1 por 100: %+v", ventas, total, r.IncomeDetail)
	}
}

// T2.8 (equivalencia): una sesión sin ninguna conversión da el mismo reporte que antes. El cambio solo afecta a ventas con
// issued_from_nota_sale_id; una venta directa a crédito sigue informando su crédito generado.
func TestSessionReportConverted_T28_sinConversionesNoCambia(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118, credit: true, paid: 40}) // NV sin convertir
	r := f.report(t, f.session)
	if len(r.CreditGenerated.Sales) != 1 || r.CreditGenerated.Total != 78 || r.Totals.TotalSales != 40 {
		t.Fatalf("sin conversión: crédito=%.2f (%d fila), TotalSales=%.2f, quería 78.00 (1) y 40.00", r.CreditGenerated.Total, len(r.CreditGenerated.Sales), r.Totals.TotalSales)
	}
}
