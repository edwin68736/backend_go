package tenantmigrations

import "gorm.io/gorm"

// V187ContactCountry agrega tenant_contacts.country (país ISO alfa-2 de un cliente extranjero; vacío = Perú). Solo agrega la columna: no toca datos
// existentes ni usa el struct completo del contacto (evita ALTER ... MODIFY sobre una tabla grande). Idempotente.
type V187ContactCountry struct{}

func (V187ContactCountry) Version() int { return 187 }
func (V187ContactCountry) Name() string { return "contact_country" }

func (V187ContactCountry) Up(db *gorm.DB) error {
	return db.AutoMigrate(&v187Contact{})
}
