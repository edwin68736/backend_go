package database

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openAuditLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AuditLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// La app corre con ProxyHeader = X-Forwarded-For, así que c.IP() entrega la cadena completa del
// header. Detrás de Cloudflare y con cliente IPv6 supera los 45 de la columna, MySQL estricto
// devolvía 1406 y la auditoría se perdía porque los llamadores descartan el error.
func TestAuditLogNormalizaIPDeCadenaProxy(t *testing.T) {
	casos := []struct {
		nombre   string
		entrada  string
		esperado string
	}{
		{
			// El caso real que destapó el bug: 53 caracteres.
			nombre:   "IPv6 de cliente más edge de Cloudflare",
			entrada:  "2001:1388:49e6:6666:4309:f270:aac1:3c05, 172.71.11.91",
			esperado: "2001:1388:49e6:6666:4309:f270:aac1:3c05",
		},
		{
			nombre:   "IPv4 con varios saltos",
			entrada:  "190.42.184.39, 172.71.11.91, 10.0.0.3",
			esperado: "190.42.184.39",
		},
		{
			nombre:   "IP suelta se guarda igual",
			entrada:  "200.121.141.16",
			esperado: "200.121.141.16",
		},
		{
			nombre:   "vacía no rompe",
			entrada:  "",
			esperado: "",
		},
		{
			// Cinturón: si llegara algo que no es una IP, se trunca en vez de fallar el INSERT.
			nombre:   "basura larga sin comas se trunca a 45",
			entrada:  strings.Repeat("x", 80),
			esperado: strings.Repeat("x", 45),
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			db := openAuditLogTestDB(t)
			log := AuditLog{TenantID: 52, UserID: 1, Action: "subscription_validity_adjusted",
				Entity: "saas_subscription", EntityID: 52, IPAddress: c.entrada}
			if err := db.Create(&log).Error; err != nil {
				t.Fatalf("crear audit log: %v", err)
			}

			var guardado AuditLog
			if err := db.First(&guardado, log.ID).Error; err != nil {
				t.Fatalf("releer audit log: %v", err)
			}
			if guardado.IPAddress != c.esperado {
				t.Errorf("ip_address = %q, se esperaba %q", guardado.IPAddress, c.esperado)
			}
			if len(guardado.IPAddress) > 45 {
				t.Errorf("ip_address mide %d, no entra en varchar(45)", len(guardado.IPAddress))
			}
		})
	}
}
