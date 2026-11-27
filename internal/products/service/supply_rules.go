package service

import (
	"errors"
	"fmt"
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/supplies"
)

// Reglas del INSUMO dentro del servicio de productos (F1). Un insumo comparte tabla, inventario y compras con los productos, pero
// nada de lo que sirve para venderlo.

// forceSupplyFields fija los campos que un insumo no puede tener, venga lo que venga en la petición.
func forceSupplyFields(input *ProductInput) {
	input.Type = "product"
	input.SalePrice = 0
	input.ManageStock = true
	input.ManageSeries = false
	input.HasVariants = false
	input.HasModifiers = false
	input.IsRestaurant = false
	input.ShowInDigitalCatalog = false
	input.CategoryID = nil
	input.BranchID = 0 // visible en todas las sucursales; el stock sigue siendo por sucursal
	input.PreparationAreaID = nil
	input.PreparationArea = ""
	input.ModifierGroupIDs = nil
	input.Presentations = nil
	input.ComboGroups = nil
}

func (s *ProductService) prepareSupplyInput(input *ProductInput) error {
	if !supplies.Ready(s.db) {
		return errors.New("los insumos aún no están disponibles para esta empresa")
	}
	if strings.EqualFold(strings.TrimSpace(input.Type), "service") || strings.EqualFold(strings.TrimSpace(input.Unit), "ZZ") {
		return errors.New("un insumo es un bien con unidad de medida, no un servicio")
	}
	forceSupplyFields(input)
	return nil
}

func errSupplyClassCode(code string, usedBySupply bool) error {
	owner := "un producto"
	if usedBySupply {
		owner = "un insumo"
	}
	return fmt.Errorf("el código '%s' ya lo usa %s: elige otro", code, owner)
}

// ensureCodeNotCrossingSupplies: el código no se repite ENTRE insumos y productos, tampoco de otra sucursal (el lector y las
// compras buscan por código). Entre productos siguen las reglas de siempre.
func (s *ProductService) ensureCodeNotCrossingSupplies(code string, isSupply bool, selfID uint) error {
	if strings.TrimSpace(code) == "" || !supplies.Ready(s.db) {
		return nil
	}
	q := s.db.Model(&database.TenantProduct{}).Where("code = ? AND id <> ?", code, selfID)
	if isSupply {
		q = q.Where(supplies.NotSupplySQL("tenant_products")) // choca con un producto vivo
	} else {
		q = q.Where("is_supply = ?", true) // choca con un insumo vivo
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errSupplyClassCode(code, !isSupply)
	}
	return nil
}
