package middleware

import (
	"bytes"
	"encoding/json"
	"strings"

	"tukifac/pkg/restaurantperm"
	"tukifac/pkg/supplies"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Costos de INSUMOS (F2b). Solo los ve quien tiene products.view_cost (Tukifac) o recipes.view_cost (Tukichef); gestionar la carta
// (g.p) no basta. En cualquier respuesta JSON del grupo /api, para los demás se ponen en 0 las claves de costo de las filas que
// son de un insumo: kardex, documentos de inventario, compras, órdenes de compra y listados de productos.

var supplyCostKeys = []string{"unit_cost", "purchase_price", "line_total", "total_cost", "avg_cost"}

// CanViewSupplyCost indica si el usuario puede ver costos de insumos.
func CanViewSupplyCost(c fiber.Ctx) bool {
	claims, ok := c.Locals("tenant_claims").(*TenantClaims)
	if !ok || claims == nil {
		return false
	}
	if tenantHasPermission(claims.Permissions, PermProductsViewCost) {
		return true
	}
	ensureRestaurantPermissions(c)
	return HasRestaurantPerm(c, restaurantperm.RecipesViewCost)
}

// StripSupplyCost filtra las respuestas. Quien sí puede ver costos no paga nada; el resto solo cuando la respuesta lleva alguno.
func StripSupplyCost() fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := c.Next(); err != nil {
			return err
		}
		status := c.Response().StatusCode()
		if status < 200 || status >= 300 || !strings.Contains(strings.ToLower(string(c.Response().Header.ContentType())), "json") {
			return nil
		}
		body := c.Response().Body()
		if !bytes.Contains(body, []byte(`"unit_cost"`)) && !bytes.Contains(body, []byte(`"purchase_price"`)) {
			return nil
		}
		db, _ := c.Locals("tenantDB").(*gorm.DB)
		if db == nil || CanViewSupplyCost(c) || !supplies.Ready(db) {
			return nil
		}
		if out, changed := stripSupplyCosts(db, body); changed {
			c.Response().SetBodyRaw(out)
		}
		return nil
	}
}

func stripSupplyCosts(db *gorm.DB, body []byte) ([]byte, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, false
	}
	// 1) filas con costo y la referencia al producto que traen; 2) cuáles de esos productos son insumos.
	type row struct {
		obj       map[string]any
		productID uint
		isSupply  bool
	}
	var rows []row
	ids := map[uint]struct{}{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			hasCost := false
			for _, k := range supplyCostKeys {
				if _, ok := t[k]; ok {
					hasCost = true
					break
				}
			}
			if hasCost {
				r := row{obj: t}
				if flag, _ := t["is_supply"].(bool); flag {
					r.isSupply = true
				} else if n, ok := t["product_id"].(json.Number); ok {
					if id, err := n.Int64(); err == nil && id > 0 {
						r.productID = uint(id)
						ids[r.productID] = struct{}{}
					}
				}
				if r.isSupply || r.productID > 0 {
					rows = append(rows, r)
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(doc)
	if len(rows) == 0 {
		return nil, false
	}
	supplyIDs := map[uint]bool{}
	if len(ids) > 0 {
		list := make([]uint, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		var found []uint
		if err := db.Table("tenant_products").Where("id IN ? AND is_supply = ?", list, true).Pluck("id", &found).Error; err != nil {
			// Sin poder comprobarlo, se ocultan todos: es preferible a mostrar un costo restringido.
			found = list
		}
		for _, id := range found {
			supplyIDs[id] = true
		}
	}
	changed := false
	for _, r := range rows {
		if !r.isSupply && !supplyIDs[r.productID] {
			continue
		}
		for _, k := range supplyCostKeys {
			if val, ok := r.obj[k]; ok && val != nil {
				r.obj[k] = 0
				changed = true
			}
		}
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, false
	}
	return out, true
}
