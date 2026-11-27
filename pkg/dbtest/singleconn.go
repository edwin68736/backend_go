// Package dbtest utilidades de prueba para el acceso a la base de datos.
package dbtest

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// WithSingleConnection ejecuta op con el pool de db limitado a UNA conexión y devuelve su error.
//
// Sirve para detectar operaciones que, con una transacción abierta, consultan por fuera de ella: necesitan una segunda
// conexión y aquí quedan trabadas. En producción cada empresa tiene pocas conexiones (DB_TENANT_MAX_OPEN), así que
// varios cobros a la vez se esperan unos a otros hasta fallar. Si op no termina, la prueba falla indicando dónde quedó.
func WithSingleConnection(t testing.TB, db *gorm.DB, op func() error) error {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("obtener el pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	done := make(chan error, 1)
	go func() { done <- op() }()
	select {
	case err := <-done:
		sqlDB.SetMaxOpenConns(0)
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("la operación pide una segunda conexión con la transacción abierta:\n%s", waitingForConnection())
		return nil
	}
}

// waitingForConnection las funciones del proyecto que están esperando una conexión del pool.
func waitingForConnection() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	var out []string
	for _, g := range strings.Split(string(buf[:n]), "\n\n") {
		if !strings.Contains(g, "database/sql.(*DB).conn(") {
			continue
		}
		for _, l := range strings.Split(g, "\n") {
			if strings.HasPrefix(l, "tukifac/") && !strings.HasPrefix(l, "tukifac/pkg/dbtest") {
				if cut := strings.LastIndex(l, "("); cut > 0 {
					out = append(out, "  "+l[:cut])
				}
			}
		}
	}
	return strings.Join(out, "\n")
}
