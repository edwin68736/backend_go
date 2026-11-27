package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"tukifac/config"
	"tukifac/internal/inventory/reconcile"
	"tukifac/pkg/branchscope"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// RunAuditStockReconcile (audit-stock-reconcile): auditoría de SOLO LECTURA de la coherencia entre el saldo de stock y el kardex
// de cada tenant (F0a-1, docs/recetas/F0a_DIAGNOSTICO_Y_PROPUESTA.md). Detecta filas de saldo duplicadas, diferencias saldo ↔ kardex,
// cadenas de saldo rotas (actualizaciones perdidas), saldos negativos y filas con clave nula. NO consolida, NO corrige, NO migra.
//
// Garantías de solo lectura:
//   - Solo ejecuta SELECT.
//   - En MySQL/MariaDB la conexión se fija a una sola sesión con SET SESSION TRANSACTION READ ONLY: el servidor rechazaría cualquier escritura.
//   - No pasa por InitDatabase (que ejecuta AutoMigrate de las tablas de flota de la BD central): se conecta solo a lo necesario.
//   - Se niega a correr contra una BD que no sea local salvo --allow-remote (la consulta a producción requiere autorización expresa).
func RunAuditStockReconcile(args []string) int {
	fs := flag.NewFlagSet("audit-stock-reconcile", flag.ExitOnError)
	slug := fs.String("slug", "", "auditar solo este tenant (slug)")
	limit := fs.Int("limit", 0, "máximo de tenants a auditar (0 = todos)")
	activeOnly := fs.Bool("active-only", true, "omitir tenants no activos")
	productID := fs.Uint("product", 0, "limitar a un producto (id)")
	branchID := fs.Uint("branch", 0, "limitar a una sucursal (id)")
	maxDetail := fs.Int("max-detail", 20, "filas de detalle por categoría en pantalla y en el JSON (0 = todas)")
	jsonOut := fs.String("json", "", "escribir además el informe completo en este archivo JSON")
	failOnDiff := fs.Bool("fail-on-diff", false, "código de salida 2 si se encuentra alguna diferencia o duplicado")
	allowRemote := fs.Bool("allow-remote", false, "permitir una BD que no sea local (requiere autorización expresa)")
	_ = fs.Parse(args)

	if config.AppConfig == nil {
		fmt.Fprintln(os.Stderr, "configuración no cargada")
		return 1
	}
	host := strings.ToLower(strings.TrimSpace(config.AppConfig.DBHost))
	if !isLocalHost(host) && !*allowRemote {
		fmt.Fprintf(os.Stderr, "✗ DB_HOST=%q no es local: este comando consulta solo la BD local por defecto.\n  Consultar otra BD (p. ej. producción) requiere autorización expresa y --allow-remote.\n", host)
		return 1
	}
	if config.AppConfig.IsProd() && !*allowRemote {
		fmt.Fprintln(os.Stderr, "✗ entorno de producción: requiere autorización expresa y --allow-remote.")
		return 1
	}

	// Solo la BD central (para listar tenants): sin EnsureCentralFleetSchema ni migraciones.
	if err := database.ConnectCentral(); err != nil {
		fmt.Fprintf(os.Stderr, "✗ BD central: %v\n", err)
		return 1
	}
	tenants, err := database.ListTenantsForMigration(*activeOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ no se pudo listar tenants: %v\n", err)
		return 1
	}
	if s := strings.TrimSpace(*slug); s != "" {
		var only []database.Tenant
		for _, t := range tenants {
			if t.Slug == s {
				only = append(only, t)
			}
		}
		tenants = only
	}
	if *limit > 0 && len(tenants) > *limit {
		tenants = tenants[:*limit]
	}

	opts := reconcile.Options{ProductID: *productID, BranchID: *branchID, MaxDetail: *maxDetail}
	fmt.Printf("audit-stock-reconcile  db_host=%s  tenants=%d  (SOLO LECTURA)\n", host, len(tenants))

	type tenantReport struct {
		Slug   string            `json:"slug"`
		DBName string            `json:"db_name"`
		Error  string            `json:"error,omitempty"`
		Report *reconcile.Report `json:"report,omitempty"`
	}
	out := struct {
		GeneratedAt time.Time      `json:"generated_at"`
		DBHost      string         `json:"db_host"`
		Options     map[string]any `json:"options"`
		Tenants     []tenantReport `json:"tenants"`
		Totals      map[string]int `json:"totals"`
	}{
		GeneratedAt: time.Now(),
		DBHost:      host,
		Options:     map[string]any{"product": *productID, "branch": *branchID, "max_detail": *maxDetail},
		Totals:      map[string]int{},
	}

	found := 0
	for _, t := range tenants {
		tr := tenantReport{Slug: t.Slug, DBName: t.DBName}
		rep, err := auditOneTenant(t.DBName, opts)
		if err != nil {
			tr.Error = err.Error()
			fmt.Printf("  ✗ %-26s %v\n", t.Slug, err)
			out.Totals["tenants_con_error"]++
			out.Tenants = append(out.Tenants, tr)
			continue
		}
		tr.Report = rep
		out.Tenants = append(out.Tenants, tr)
		out.Totals["tenants_auditados"]++
		if rep.Skipped != "" {
			fmt.Printf("  - %-26s omitido: %s\n", t.Slug, rep.Skipped)
			out.Totals["tenants_omitidos"]++
			continue
		}
		s := rep.Summary
		anomalies := s.DuplicateGroups + s.Mismatches + s.ChainBreaks + s.NegativeBalances + s.ZeroOrNullKeyRows + s.KardexWithoutStock
		if anomalies > 0 {
			found++
		}
		out.Totals["filas_saldo_producto"] += s.ProductStockRows
		out.Totals["filas_saldo_presentacion"] += s.PresentationStockRows
		out.Totals["movimientos"] += s.Movements
		out.Totals["grupos_duplicados"] += s.DuplicateGroups
		out.Totals["filas_duplicadas_sobrantes"] += s.DuplicateExtraRows
		out.Totals["diferencias_saldo_vs_kardex"] += s.Mismatches
		out.Totals["cadenas_rotas"] += s.ChainBreaks
		out.Totals["saldos_negativos"] += s.NegativeBalances
		out.Totals["claves_nulas_o_cero"] += s.ZeroOrNullKeyRows
		out.Totals["stock_sin_kardex"] += s.StockWithoutKardex
		out.Totals["kardex_sin_stock"] += s.KardexWithoutStock
		out.Totals["inicios_sin_saldo_previo"] += s.InitialOffsets
		printTenantReport(t.Slug, rep)
	}
	out.Totals["tenants_con_anomalias"] = found

	fmt.Println("\n=== TOTALES ===")
	for _, k := range []string{"tenants_auditados", "tenants_omitidos", "tenants_con_error", "tenants_con_anomalias",
		"filas_saldo_producto", "filas_saldo_presentacion", "movimientos", "grupos_duplicados", "filas_duplicadas_sobrantes",
		"diferencias_saldo_vs_kardex", "cadenas_rotas", "saldos_negativos", "claves_nulas_o_cero", "stock_sin_kardex",
		"kardex_sin_stock", "inicios_sin_saldo_previo"} {
		fmt.Printf("  %-30s %d\n", k, out.Totals[k])
	}
	fmt.Println("\n(solo lectura: no se escribió nada, no se consolidó ni se corrigió ningún dato)")

	if p := strings.TrimSpace(*jsonOut); p != "" {
		b, err := json.MarshalIndent(out, "", "  ")
		if err == nil {
			err = os.WriteFile(p, b, 0o600)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ no se pudo escribir %s: %v\n", p, err)
			return 1
		}
		fmt.Printf("informe JSON: %s\n", p)
	}
	if *failOnDiff && found > 0 {
		return 2
	}
	return 0
}

// auditOneTenant abre la BD del tenant, fija UNA conexión de solo lectura y corre el reconciliador.
func auditOneTenant(dbName string, opts reconcile.Options) (*reconcile.Report, error) {
	db, err := database.OpenTenantDBForMigration(dbName)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión: %w", err)
	}
	defer database.CloseTenantDB(db)

	var rep *reconcile.Report
	err = db.Connection(func(conn *gorm.DB) error {
		// El servidor rechazará cualquier escritura de esta sesión.
		if err := conn.Exec("SET SESSION TRANSACTION READ ONLY").Error; err != nil {
			return fmt.Errorf("no se pudo fijar la sesión en solo lectura: %w", err)
		}
		// Una sola foto consistente para las dos lecturas (saldos y kardex): sin ella, una venta que se confirme entre ambas
		// produciría diferencias falsas en un tenant en uso. No bloquea a nadie (lectura consistente, sin locks).
		if err := conn.Exec("START TRANSACTION WITH CONSISTENT SNAPSHOT").Error; err != nil {
			return fmt.Errorf("no se pudo abrir la lectura consistente: %w", err)
		}
		defer conn.Exec("ROLLBACK")
		r, err := reconcile.Run(branchscope.System(conn, "audit-stock-reconcile (solo lectura)"), opts)
		rep = r
		return err
	})
	return rep, err
}

func printTenantReport(slug string, rep *reconcile.Report) {
	s := rep.Summary
	mark := "✓"
	if s.DuplicateGroups+s.Mismatches+s.ChainBreaks+s.NegativeBalances+s.ZeroOrNullKeyRows+s.KardexWithoutStock > 0 {
		mark = "⚠"
	}
	fmt.Printf("  %s %-26s saldos(prod=%d pres=%d) mov=%d | duplicados=%d (+%d filas) | dif=%d | cadena_rota=%d | negativos=%d | clave_nula=%d | stock_sin_kardex=%d | kardex_sin_stock=%d | inicio_sin_saldo_previo=%d\n",
		mark, slug, s.ProductStockRows, s.PresentationStockRows, s.Movements, s.DuplicateGroups, s.DuplicateExtraRows,
		s.Mismatches, s.ChainBreaks, s.NegativeBalances, s.ZeroOrNullKeyRows, s.StockWithoutKardex, s.KardexWithoutStock, s.InitialOffsets)
	for _, d := range rep.Duplicates {
		fmt.Printf("      DUPLICADO %s=%d sucursal=%d filas=%v suma=%.3f fila_que_escribe=%d kardex=%s\n",
			d.Kind, d.EntityID, d.BranchID, d.Rows, d.Sum, d.WriterRowID, fmtPtr(d.KardexBalance))
	}
	for _, d := range rep.Diffs {
		fmt.Printf("      %-17s %s=%d sucursal=%d saldo=%s kardex=%s delta=%.3f mov=%d último_mov=%d\n",
			d.Category, d.Kind, d.EntityID, d.BranchID, fmtPtr(d.Stock), fmtPtr(d.KardexBalance), d.Delta, d.Movements, d.LastMovementID)
	}
	for _, b := range rep.ChainBreaks {
		fmt.Printf("      CADENA_ROTA %s=%d sucursal=%d mov=%d tipo=%s cant=%.3f previo=%.3f esperado=%.3f guardado=%.3f\n",
			b.Kind, b.EntityID, b.BranchID, b.MovementID, b.Type, b.Quantity, b.Previous, b.Expected, b.Stored)
	}
	if rep.Truncated {
		fmt.Println("      … detalle truncado (--max-detail); los conteos del resumen son exactos")
	}
}

func fmtPtr(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f", *v)
}

func isLocalHost(h string) bool {
	switch h {
	case "", "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}
