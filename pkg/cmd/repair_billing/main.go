// repair_billing reconcilia estados de facturación inconsistentes (dry-run por defecto).
//
// Uso:
//
//	go run ./pkg/cmd/repair_billing -central-dsn="user:pass@tcp(host:3306)/central"
//	go run ./pkg/cmd/repair_billing -central-dsn="..." -apply
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("central-dsn", os.Getenv("CENTRAL_DSN"), "DSN MySQL central (tukifac_central)")
	apply := flag.Bool("apply", false, "aplicar correcciones (sin flag = solo reporte)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("indique -central-dsn o CENTRAL_DSN")
	}

	cdb, err := gorm.Open(mysql.Open(*dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	var tenants []database.Tenant
	if err := cdb.Where("status = ?", "active").Find(&tenants).Error; err != nil {
		log.Fatal(err)
	}

	var total, fixed int
	for _, t := range tenants {
		tdb, err := gorm.Open(mysql.Open(buildTenantDSN(*dsn, t.DBName)), &gorm.Config{})
		if err != nil {
			log.Printf("tenant %s: connect err: %v", t.DBName, err)
			continue
		}
		var invoices []database.TenantInvoice
		_ = tdb.Find(&invoices).Error
		for i := range invoices {
			inv := &invoices[i]
			newP, reason := billingstate.ReconcileInconsistent(inv)
			if reason == "" {
				continue
			}
			total++
			fmt.Printf("[%s] sale_id=%d pipeline=%s job=%s sunat=%s -> %s (%s)\n",
				t.DBName, inv.SaleID, inv.PipelineStatus, inv.JobStatus, inv.SunatStatus, newP, reason)
			if *apply {
				patch := billingstate.BuildPatch(newP, inv, reason)
				billingstate.ApplyToInvoice(inv, patch)
				_ = tdb.Save(inv).Error
				_ = billingstate.SyncSaleBillingStatus(tdb, inv.SaleID, newP)
				fixed++
			}
		}
	}
	fmt.Printf("inconsistencias=%d aplicadas=%d dry_run=%v\n", total, fixed, !*apply)
}

func buildTenantDSN(centralDSN, dbName string) string {
	// Reemplazo simple: último segmento del path = dbName
	// user:pass@tcp(host:3306)/central -> /dbName
	i := len(centralDSN) - 1
	for i >= 0 && centralDSN[i] != '/' {
		i--
	}
	if i < 0 {
		return centralDSN
	}
	return centralDSN[:i+1] + dbName + "?charset=utf8mb4&parseTime=True&loc=Local"
}
