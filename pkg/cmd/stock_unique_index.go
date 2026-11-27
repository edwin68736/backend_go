package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
)

// RunStockUniqueIndex (stock-unique-index): informa qué tenants tienen el índice UNIQUE de saldo (F0a-2) y por qué no lo tienen,
// y con --apply lo crea en los que ya no tienen duplicados. La migración V193 corre una sola vez: este es el camino para crear el
// índice después de que alguien limpie los duplicados.
//
// Sin --apply no escribe nada. Con --apply SOLO crea índices (DDL); nunca consolida, borra ni modifica un saldo, y donde haya
// duplicados no crea nada. Igual que audit-stock-reconcile, no pasa por InitDatabase y no corre contra una BD no local salvo
// --allow-remote (requiere autorización expresa).
func RunStockUniqueIndex(args []string) int {
	fs := flag.NewFlagSet("stock-unique-index", flag.ExitOnError)
	slug := fs.String("slug", "", "solo este tenant (slug)")
	limit := fs.Int("limit", 0, "máximo de tenants (0 = todos)")
	activeOnly := fs.Bool("active-only", true, "omitir tenants no activos")
	apply := fs.Bool("apply", false, "crear los índices que falten donde no haya duplicados")
	allowRemote := fs.Bool("allow-remote", false, "permitir una BD que no sea local (requiere autorización expresa)")
	_ = fs.Parse(args)

	if config.AppConfig == nil {
		fmt.Fprintln(os.Stderr, "configuración no cargada")
		return 1
	}
	host := strings.ToLower(strings.TrimSpace(config.AppConfig.DBHost))
	if (!isLocalHost(host) || config.AppConfig.IsProd()) && !*allowRemote {
		fmt.Fprintf(os.Stderr, "✗ DB_HOST=%q no es local o el entorno es de producción: requiere autorización expresa y --allow-remote.\n", host)
		return 1
	}
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

	mode := "SOLO CONSULTA"
	if *apply {
		mode = "CREAR ÍNDICES FALTANTES"
	}
	fmt.Printf("stock-unique-index  db_host=%s  tenants=%d  (%s)\n", host, len(tenants), mode)

	var present, created, blocked, pending, failed int
	for _, t := range tenants {
		statuses, err := stockUniqueForTenant(t.DBName, *apply)
		if err != nil {
			failed++
			fmt.Printf("  ✗ %-26s %v\n", t.Slug, err)
			continue
		}
		for _, s := range statuses {
			switch {
			case s.TableMissing:
				fmt.Printf("  - %-26s %-34s %s\n", t.Slug, s.Table, s.Reason)
			case s.Created:
				created++
				fmt.Printf("  + %-26s %-34s índice creado\n", t.Slug, s.Table)
			case s.Exists:
				present++
			case s.DuplicateGroups > 0:
				blocked++
				fmt.Printf("  ⚠ %-26s %-34s SIN ÍNDICE: %s\n", t.Slug, s.Table, s.Reason)
				for _, sample := range s.Samples {
					fmt.Printf("        %s\n", sample)
				}
			default:
				pending++
				fmt.Printf("  · %-26s %-34s %s\n", t.Slug, s.Table, s.Reason)
			}
		}
	}
	fmt.Printf("\ntablas con índice=%d  creados ahora=%d  bloqueadas por duplicados=%d  pendientes sin duplicados=%d  tenants con error=%d\n",
		present, created, blocked, pending, failed)
	if !*apply {
		fmt.Println("(solo consulta: no se escribió nada)")
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func stockUniqueForTenant(dbName string, apply bool) ([]tenantmigrations.StockUniqueStatus, error) {
	// Conexión de migración: con límites de espera de locks, para no dejar parado al tenant si la tabla está en uso.
	db, err := database.OpenTenantDBForMigration(dbName)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión: %w", err)
	}
	defer database.CloseTenantDB(db)
	return tenantmigrations.EnsureStockUniqueIndexes(db, apply)
}
