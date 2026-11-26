package service

import (
	"fmt"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"
)

// Muchos cobros a la vez con el límite de conexiones por empresa de producción (DB_TENANT_MAX_OPEN=3). Si el cobro pidiera
// una segunda conexión con la transacción abierta, se esperarían unos a otros y fallarían por tiempo de espera.
func TestMySQL_cobrosSimultaneosConTresConexiones(t *testing.T) {
	db := exactnessMySQL(t)
	series := database.TenantDocumentSeries{BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: "B001", Correlative: 1, Active: true}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	const tables = 12
	sessions := make([]uint, tables)
	for i := range sessions {
		sessions[i], _ = mysqlTable(t, db, fmt.Sprintf("P%02d", i), 2)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(3)

	start := make(chan struct{})
	errs := make(chan error, tables)
	for i := range sessions {
		go func(i int) {
			<-start
			_, err := New(db).BillTable(BillInput{SessionID: sessions[i], UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
				IdempotencyKey: fmt.Sprintf("pool-%d", i), Payments: []PaymentInput{{Method: "cash", Amount: 20}}}, tax.DefaultConfig())
			errs <- err
		}(i)
	}
	began := time.Now()
	close(start)
	deadline := time.After(60 * time.Second)
	failed := 0
	var first error
	for range sessions {
		select {
		case err := <-errs:
			if err != nil {
				failed++
				if first == nil {
					first = err
				}
			}
		case <-deadline:
			t.Fatalf("los cobros no terminaron en 60 s: se esperan unos a otros por falta de conexiones")
		}
	}
	t.Logf("%d cobros simultáneos con 3 conexiones: %d fallidos en %s", tables, failed, time.Since(began).Round(time.Millisecond))
	if failed > 0 {
		t.Fatalf("fallaron %d de %d cobros; primer error: %v", failed, tables, first)
	}
}
