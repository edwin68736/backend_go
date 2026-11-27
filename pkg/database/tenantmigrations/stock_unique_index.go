package tenantmigrations

import (
	"errors"
	"fmt"
	"log"
	"strings"

	mysqlerr "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// Índice UNIQUE de las tablas de saldo (F0a-2): una sola fila por (producto, sucursal) y por (presentación, sucursal).
//
// Deliberadamente NO se declara en los tags de los modelos: cualquier AutoMigrate sobre un tenant con duplicados fallaría. Se crea
// solo aquí, y solo si el tenant no tiene duplicados; si los tiene, se omite y se registra el motivo. NUNCA se consolida ni se
// modifica un saldo. El código de la aplicación no debe depender de que el índice exista.
const (
	idxProductStockUnique      = "uk_tenant_product_stocks_product_branch"
	idxPresentationStockUnique = "uk_tenant_pres_stocks_presentation_branch"
	duplicateSampleLimit       = 10
)

type stockUniqueTarget struct {
	table  string
	column string
	index  string
}

func stockUniqueTargets() []stockUniqueTarget {
	return []stockUniqueTarget{
		{table: "tenant_product_stocks", column: "product_id", index: idxProductStockUnique},
		{table: "tenant_product_presentation_stocks", column: "presentation_id", index: idxPresentationStockUnique},
	}
}

// StockUniqueStatus describe cómo quedó el índice de una tabla de saldo.
type StockUniqueStatus struct {
	Table           string   `json:"table"`
	Index           string   `json:"index"`
	TableMissing    bool     `json:"table_missing,omitempty"`
	Exists          bool     `json:"exists"`  // el índice ya estaba o quedó creado
	Created         bool     `json:"created"` // se creó en esta ejecución
	DuplicateGroups int64    `json:"duplicate_groups"`
	Samples         []string `json:"samples,omitempty"` // hasta 10 grupos duplicados, "producto=1 sucursal=2 filas=3 ids=4,5,6"
	Reason          string   `json:"reason,omitempty"`  // por qué no se creó
}

// EnsureStockUniqueIndexes revisa (y con apply=true, crea) los índices UNIQUE de saldo. Con duplicados NO crea el índice y no
// devuelve error: el tenant sigue funcionando sin él. Solo devuelve error ante fallas reales de la BD.
func EnsureStockUniqueIndexes(db *gorm.DB, apply bool) ([]StockUniqueStatus, error) {
	var out []StockUniqueStatus
	for _, t := range stockUniqueTargets() {
		st, err := ensureStockUniqueIndex(db, t, apply)
		if err != nil {
			return out, err
		}
		out = append(out, st)
	}
	return out, nil
}

func ensureStockUniqueIndex(db *gorm.DB, t stockUniqueTarget, apply bool) (StockUniqueStatus, error) {
	st := StockUniqueStatus{Table: t.table, Index: t.index}
	mig := db.Migrator()
	if !mig.HasTable(t.table) {
		st.TableMissing = true
		st.Reason = "la tabla no existe"
		return st, nil
	}
	if mig.HasIndex(t.table, t.index) {
		st.Exists = true
		return st, nil
	}

	groups, err := countDuplicateGroups(db, fmt.Sprintf(
		`SELECT COUNT(*) FROM (SELECT %s, branch_id FROM %s GROUP BY %s, branch_id HAVING COUNT(*) > 1) dup`,
		t.column, t.table, t.column))
	if err != nil {
		return st, fmt.Errorf("auditoría de duplicados en %s: %w", t.table, err)
	}
	st.DuplicateGroups = groups
	if groups > 0 {
		st.Samples, err = duplicateStockSamples(db, t)
		if err != nil {
			return st, fmt.Errorf("muestra de duplicados en %s: %w", t.table, err)
		}
		st.Reason = fmt.Sprintf("hay %d grupos duplicados de (%s, branch_id); no se consolida automáticamente", groups, t.column)
		return st, nil
	}
	if !apply {
		st.Reason = "sin duplicados; el índice aún no está creado"
		return st, nil
	}

	if err := db.Exec(fmt.Sprintf(`CREATE UNIQUE INDEX %s ON %s (%s, branch_id)`, t.index, t.table, t.column)).Error; err != nil {
		// Entre la auditoría y la creación pudo insertarse un duplicado (tenant en uso): se omite como cualquier otro duplicado.
		if isDuplicateKeyError(err) {
			st.Reason = "se insertó un duplicado mientras se creaba el índice; se omite"
			return st, nil
		}
		return st, fmt.Errorf("crear %s: %w", t.index, err)
	}
	st.Exists = true
	st.Created = true
	return st, nil
}

func duplicateStockSamples(db *gorm.DB, t stockUniqueTarget) ([]string, error) {
	var groups []struct {
		Entity   uint
		BranchID uint
		N        int64
	}
	if err := db.Raw(fmt.Sprintf(
		`SELECT %s AS entity, branch_id, COUNT(*) AS n FROM %s GROUP BY %s, branch_id HAVING COUNT(*) > 1 ORDER BY %s, branch_id LIMIT %d`,
		t.column, t.table, t.column, t.column, duplicateSampleLimit)).Scan(&groups).Error; err != nil {
		return nil, err
	}
	label := "producto"
	if t.column == "presentation_id" {
		label = "presentación"
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		var ids []uint
		if err := db.Raw(fmt.Sprintf(`SELECT id FROM %s WHERE %s = ? AND branch_id = ? ORDER BY id`, t.table, t.column),
			g.Entity, g.BranchID).Scan(&ids).Error; err != nil {
			return nil, err
		}
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprint(id)
		}
		out = append(out, fmt.Sprintf("%s=%d sucursal=%d filas=%d ids=%s", label, g.Entity, g.BranchID, g.N, strings.Join(parts, ",")))
	}
	return out, nil
}

func isDuplicateKeyError(err error) bool {
	var me *mysqlerr.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") // SQLite (pruebas)
}

// logStockUniqueStatus deja en el log por qué un tenant quedó sin índice.
func logStockUniqueStatus(db *gorm.DB, statuses []StockUniqueStatus) {
	var schema string
	if db.Dialector.Name() == "mysql" {
		_ = db.Raw("SELECT DATABASE()").Scan(&schema).Error
	}
	for _, s := range statuses {
		if s.Exists || s.TableMissing {
			continue
		}
		log.Printf("[v193] bd=%s tabla=%s ÍNDICE OMITIDO %s: %s", schema, s.Table, s.Index, s.Reason)
		for _, sample := range s.Samples {
			log.Printf("[v193] bd=%s tabla=%s duplicado: %s", schema, s.Table, sample)
		}
	}
}
