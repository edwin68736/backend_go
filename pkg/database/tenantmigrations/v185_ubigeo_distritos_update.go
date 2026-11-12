package tenantmigrations

import (
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V185UbigeoDistritosUpdate agrega a cada tenant los distritos del catálogo INEI que se crearon después de la carga inicial (17 distritos,
// entre ellos San Antonio — Mariscal Nieto, Moquegua, ubigeo 180107, Ley 31216). Sin esto, un contribuyente con domicilio fiscal en un
// distrito nuevo no podía configurarlo ni emitir: «ubigeo … no encontrado en catálogo». Solo inserta lo que falta; no toca los existentes.
type V185UbigeoDistritosUpdate struct{}

func (V185UbigeoDistritosUpdate) Version() int { return 185 }
func (V185UbigeoDistritosUpdate) Name() string { return "ubigeo_distritos_update" }

func (V185UbigeoDistritosUpdate) Up(db *gorm.DB) error {
	if err := database.SeedUbigeoRegionesProvincias(db); err != nil {
		return err
	}
	if err := database.UpsertUbigeoRegionesProvincias(db); err != nil { // p. ej. la provincia Calca (080400) faltaba
		return err
	}
	_, err := database.UpsertUbigeoDistritos(db)
	return err
}
