package handler

// Catálogo de INSUMOS para Tukichef (F1). Reutiliza los servicios de productos e inventario; las rutas viven bajo /restaurant para
// que el personal con PIN pueda usarlas según sus permisos de restaurante. El costo solo viaja (y solo se escribe) con
// recipes.view_cost, en un campo propio: unit_cost.

import (
	"errors"
	"math"
	"strconv"
	"strings"

	invsvc "tukifac/internal/inventory/service"
	productsvc "tukifac/internal/products/service"
	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/branch"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"
	"tukifac/pkg/restaurantperm"
	"tukifac/pkg/supplies"
	"tukifac/pkg/tax"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

const supplyPageSize = 50

type supplyView struct {
	ID            uint                        `json:"id"`
	Code          string                      `json:"code"`
	Name          string                      `json:"name"`
	Description   string                      `json:"description"`
	Unit          string                      `json:"unit"`
	UnitID        *uint                       `json:"unit_id"`
	MinStock      float64                     `json:"min_stock"`
	Active        bool                        `json:"active"`
	HasExpiryDate bool                        `json:"has_expiry_date"`
	ExpiryDate    string                      `json:"expiry_date,omitempty"`
	StockTotal    float64                     `json:"stock_total"`
	StockByBranch []productsvc.BranchStockRow `json:"stock_by_branch"`
	UnitCost      *float64                    `json:"unit_cost,omitempty"` // «último costo»; solo con recipes.view_cost
	AvgCost       *float64                    `json:"avg_cost,omitempty"`  // costo promedio de la sucursal consultada
}

type supplyBody struct {
	Code          *string  `json:"code"`
	Name          *string  `json:"name"`
	Description   *string  `json:"description"`
	Unit          *string  `json:"unit"`
	UnitID        *uint    `json:"unit_id"`
	MinStock      *float64 `json:"min_stock"`
	Active        *bool    `json:"active"`
	HasExpiryDate *bool    `json:"has_expiry_date"`
	ExpiryDate    *string  `json:"expiry_date"`
	UnitCost      *float64 `json:"unit_cost"`
}

// RequireRecipesEnabled corta las rutas de insumos mientras el superadmin no active el módulo (o la BD no esté migrada).
func (h *RestaurantHandler) RequireRecipesEnabled(c fiber.Ctx) error {
	if gdb := db(c); gdb != nil && database.RecipesEnabled(gdb) && supplies.Ready(gdb) {
		return c.Next()
	}
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "El módulo de insumos no está activado", "code": "recipes_disabled"})
}

func canViewSupplyCost(c fiber.Ctx) bool {
	return middleware.HasRestaurantPerm(c, restaurantperm.RecipesViewCost)
}

// supplyBranch: quien no ve todas las sucursales queda acotado a la suya.
func supplyBranch(c fiber.Ctx) uint {
	requested, _ := strconv.ParseUint(c.Query("branch_id"), 10, 32)
	if requested > 0 {
		return branch.ResolveReadBranchFilter(c, uint(requested))
	}
	if branch.IsBranchAdmin(c) {
		return 0
	}
	if active := branch.ActiveBranchID(c); active > 0 {
		return active
	}
	return branch.NoBranchFilter
}

func toSupplyView(item productsvc.ProductReportItem, withCost bool) supplyView {
	v := supplyView{
		ID: item.ID, Code: item.Code, Name: item.Name, Description: item.Description, Unit: item.Unit, UnitID: item.UnitID,
		MinStock: item.MinStock, Active: item.Active, HasExpiryDate: item.HasExpiryDate,
		StockTotal: item.StockTotal, StockByBranch: item.StockByBranch,
	}
	if item.ExpiryDate != nil {
		v.ExpiryDate = item.ExpiryDate.Format("2006-01-02")
	}
	if withCost {
		cost := item.PurchasePrice
		v.UnitCost = &cost
	}
	return v
}

func supplyByID(c fiber.Ctx) (*database.TenantProduct, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil || id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var p database.TenantProduct
	if err := db(c).Where("id = ? AND is_supply = ?", id, true).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func supplyNotFound(c fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "insumo no encontrado"})
	}
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
}

func supplyBadRequest(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": msg})
}

// supplyViewByID reconstruye la vista de un insumo con su stock.
func supplyViewByID(c fiber.Ctx, p *database.TenantProduct) (supplyView, error) {
	items, _, err := productsvc.NewProductService(db(c)).ListReport(productsvc.ProductListParams{
		SupplyMode: supplies.ModeOnly, Query: p.Code, BranchID: supplyBranch(c),
	})
	if err != nil {
		return supplyView{}, err
	}
	for _, it := range items {
		if it.ID == p.ID {
			view := toSupplyView(it, canViewSupplyCost(c))
			if canViewSupplyCost(c) {
				view.AvgCost = supplyAverages(db(c), []uint{p.ID}, supplyBranch(c))[p.ID]
			}
			return view, nil
		}
	}
	return supplyView{}, gorm.ErrRecordNotFound
}

func (h *RestaurantHandler) ListSupplies(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page"))
	perPage, _ := strconv.Atoi(c.Query("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = supplyPageSize
	}
	items, total, err := productsvc.NewProductService(db(c)).ListReport(productsvc.ProductListParams{
		SupplyMode:   supplies.ModeOnly,
		Query:        strings.TrimSpace(c.Query("q")),
		ActiveOnly:   c.Query("active_only") == "true" || c.Query("active_only") == "1",
		InactiveOnly: c.Query("inactive_only") == "true" || c.Query("inactive_only") == "1",
		BranchID:     supplyBranch(c),
		SortBy:       "name",
		SortDir:      "asc",
		Limit:        perPage,
		Offset:       (page - 1) * perPage,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	withCost := canViewSupplyCost(c)
	var averages map[uint]*float64
	if withCost {
		ids := make([]uint, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		averages = supplyAverages(db(c), ids, supplyBranch(c))
	}
	out := make([]supplyView, 0, len(items))
	for _, it := range items {
		v := toSupplyView(it, withCost)
		v.AvgCost = averages[it.ID]
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"data": out, "total": total})
}

func (h *RestaurantHandler) GetSupply(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	view, err := supplyViewByID(c, p)
	if err != nil {
		return supplyNotFound(c, err)
	}
	return c.JSON(fiber.Map{"data": view})
}

// applySupplyBody vuelca en input lo que venga en el cuerpo; lo que no venga conserva su valor.
func applySupplyBody(c fiber.Ctx, body supplyBody, input *productsvc.ProductInput) error {
	if body.Code != nil {
		input.Code = strings.TrimSpace(*body.Code)
	}
	if body.Name != nil {
		input.Name = strings.TrimSpace(*body.Name)
	}
	if body.Description != nil {
		input.Description = *body.Description
	}
	if body.Unit != nil {
		input.Unit = strings.TrimSpace(*body.Unit)
		input.UnitID = nil
	}
	if body.UnitID != nil && *body.UnitID > 0 {
		input.UnitID = body.UnitID
	}
	if body.MinStock != nil {
		if *body.MinStock < 0 {
			return errors.New("el stock mínimo no puede ser negativo")
		}
		input.MinStock = *body.MinStock
	}
	if body.Active != nil {
		input.Active, input.ActiveSet = *body.Active, true
	}
	if body.HasExpiryDate != nil {
		input.HasExpiryDate = *body.HasExpiryDate
		if !input.HasExpiryDate {
			input.ExpiryDate = nil
		}
	}
	if body.ExpiryDate != nil {
		expiry, err := productsvc.ParseProductExpiryDate(*body.ExpiryDate)
		if err != nil {
			return err
		}
		input.ExpiryDate = expiry
	}
	// Sin permiso de costos el valor enviado se ignora: ni se lee ni se pisa.
	if body.UnitCost != nil && canViewSupplyCost(c) {
		if *body.UnitCost < 0 {
			return errors.New("el costo no puede ser negativo")
		}
		input.PurchasePrice = *body.UnitCost
	}
	if input.Name == "" {
		return errors.New("el nombre del insumo es requerido")
	}
	return nil
}

func (h *RestaurantHandler) CreateSupply(c fiber.Ctx) error {
	var body supplyBody
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	input := productsvc.ProductInput{
		IsSupply: true, Type: "product", Unit: "NIU", Active: true, ActiveSet: true,
		IgvAffectationType: "10", PriceIncludesIgv: true, TaxRate: tax.LoadFromDB(db(c)).TaxRate,
	}
	if err := applySupplyBody(c, body, &input); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	p, _, err := productsvc.NewProductService(db(c)).Create(input)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	p, err = productsvc.NewProductService(db(c)).GetByID(p.ID)
	if err != nil {
		return supplyNotFound(c, err)
	}
	view, err := supplyViewByID(c, p)
	if err != nil {
		return supplyNotFound(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": view})
}

func (h *RestaurantHandler) UpdateSupply(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	var body supplyBody
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	input := productsvc.ProductInput{
		BrandID: p.BrandID, Code: p.Code, Name: p.Name, Description: p.Description, Type: p.Type, Unit: p.Unit, UnitID: p.UnitID,
		PurchasePrice: p.PurchasePrice, TaxRate: p.TaxRate, IgvAffectationType: p.IgvAffectationType,
		PriceIncludesIgv: p.PriceIncludesIgv, ManageStock: true, MinStock: p.MinStock, HasExpiryDate: p.HasExpiryDate,
		ExpiryDate: p.ExpiryDate, Active: p.Active,
	}
	if err := applySupplyBody(c, body, &input); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	// La unidad base da significado al stock ya registrado: con movimientos no se cambia.
	unitChanges := (body.UnitID != nil && p.UnitID != nil && *body.UnitID != *p.UnitID) ||
		(body.UnitID == nil && body.Unit != nil && !strings.EqualFold(strings.TrimSpace(*body.Unit), p.Unit))
	if unitChanges {
		// Las recetas guardan cantidades en la unidad base: cambiarla las falsearía.
		if recipesvc.UsedAsIngredient(db(c), p.ID) {
			return supplyBadRequest(c, "no se puede cambiar la unidad base: el insumo está en una receta")
		}
		var movements int64
		if err := db(c).Model(&database.TenantStockMovement{}).Where("product_id = ?", p.ID).Count(&movements).Error; err != nil {
			return supplyNotFound(c, err)
		}
		if movements > 0 {
			return supplyBadRequest(c, "no se puede cambiar la unidad base: el insumo ya tiene movimientos de inventario")
		}
	}
	if _, err := productsvc.NewProductService(db(c)).Update(p.ID, input); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	p, err = productsvc.NewProductService(db(c)).GetByID(p.ID)
	if err != nil {
		return supplyNotFound(c, err)
	}
	view, err := supplyViewByID(c, p)
	if err != nil {
		return supplyNotFound(c, err)
	}
	return c.JSON(fiber.Map{"data": view})
}

func (h *RestaurantHandler) SupplyStock(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	view, err := supplyViewByID(c, p)
	if err != nil {
		return supplyNotFound(c, err)
	}
	return c.JSON(fiber.Map{"data": view.StockByBranch, "total": view.StockTotal})
}

type supplyMovementView struct {
	ID        uint     `json:"id"`
	BranchID  uint     `json:"branch_id"`
	Type      string   `json:"type"`
	Quantity  float64  `json:"quantity"`
	Balance   float64  `json:"balance"`
	Reference string   `json:"reference"`
	Notes     string   `json:"notes"`
	CreatedAt   string   `json:"created_at"`
	UnitCost    *float64 `json:"unit_cost,omitempty"`
	CostUnknown bool     `json:"cost_unknown,omitempty"`
}

func (h *RestaurantHandler) SupplyMovements(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	rows, total, err := invsvc.NewInventoryService(db(c)).GetKardex(invsvc.KardexParams{
		ProductID: p.ID, BranchID: supplyBranch(c), Limit: supplyPageSize, Offset: (page - 1) * supplyPageSize,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	withCost := canViewSupplyCost(c)
	out := make([]supplyMovementView, 0, len(rows))
	for _, m := range rows {
		v := supplyMovementView{
			ID: m.ID, BranchID: m.BranchID, Type: m.Type, Quantity: m.Quantity, Balance: m.Balance,
			Reference: m.Reference, Notes: m.Notes, CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if withCost {
			cost := m.UnitCost
			v.UnitCost, v.CostUnknown = &cost, m.CostUnknown
		}
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"data": out, "total": total})
}

// supplyAverages: costo promedio por insumo. Con una sucursal, el suyo; con todas, ponderado por el saldo positivo de cada una
// (o el único conocido si no hay saldo). Sin promedio, el insumo no aparece en el mapa.
func supplyAverages(gdb *gorm.DB, ids []uint, branchID uint) map[uint]*float64 {
	out := map[uint]*float64{}
	if len(ids) == 0 || !invsvc.SupplyCostsReady(gdb) {
		return out
	}
	q := gdb.Table("tenant_product_stocks").Select("product_id, quantity, avg_unit_cost").
		Where("product_id IN ? AND avg_unit_cost IS NOT NULL", ids).Order("product_id, branch_id")
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	var rows []struct {
		ProductID   uint
		Quantity    float64
		AvgUnitCost float64
	}
	if err := q.Find(&rows).Error; err != nil {
		return out
	}
	value, qty := map[uint]float64{}, map[uint]float64{}
	for _, r := range rows {
		if r.Quantity > 0 {
			value[r.ProductID] += r.Quantity * r.AvgUnitCost
			qty[r.ProductID] += r.Quantity
		}
		if _, ok := out[r.ProductID]; !ok {
			cost := r.AvgUnitCost
			out[r.ProductID] = &cost
		}
	}
	for id, n := range qty {
		avg := math.Round(value[id]/n*1e6) / 1e6
		out[id] = &avg
	}
	return out
}

// SupplyUnitCatalog: unidades de medida disponibles para elegir la unidad base y las de compra.
func (h *RestaurantHandler) SupplyUnitCatalog(c fiber.Ctx) error {
	var units []database.TenantUnit
	if err := db(c).Where("active = ?", true).Order("name ASC").Find(&units).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": units})
}

// ── Unidades de compra (saco, caja, kg): unidades de venta del producto, sin precio ──

type supplyUnitBody struct {
	Name             string  `json:"name"`
	UnitID           *uint   `json:"unit_id"`
	ConversionFactor float64 `json:"conversion_factor"`
	IsBase           bool    `json:"is_base"`
	AllowFraction    bool    `json:"allow_fraction"`
	SortOrder        int     `json:"sort_order"`
	Active           *bool   `json:"active"`
}

func (b supplyUnitBody) input() productsvc.SaleUnitInput {
	active := b.Active == nil || *b.Active
	return productsvc.SaleUnitInput{
		Name: b.Name, UnitID: b.UnitID, ConversionFactor: b.ConversionFactor, IsBase: b.IsBase,
		AllowFraction: b.AllowFraction, SortOrder: b.SortOrder, Active: active,
	}
}

func (h *RestaurantHandler) ListSupplyUnits(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	units, err := productsvc.NewProductService(db(c)).ListAllSaleUnits(p.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": units})
}

func (h *RestaurantHandler) CreateSupplyUnit(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	var body supplyUnitBody
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	unit, err := productsvc.NewProductService(db(c)).CreateSaleUnit(p.ID, body.input())
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": unit})
}

func (h *RestaurantHandler) UpdateSupplyUnit(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	unitID, err := strconv.ParseUint(c.Params("unitId"), 10, 32)
	if err != nil {
		return supplyBadRequest(c, "ID inválido")
	}
	var body supplyUnitBody
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	unit, err := productsvc.NewProductService(db(c)).UpdateSaleUnit(p.ID, uint(unitID), body.input())
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"data": unit})
}

func (h *RestaurantHandler) DeleteSupplyUnit(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	unitID, err := strconv.ParseUint(c.Params("unitId"), 10, 32)
	if err != nil {
		return supplyBadRequest(c, "ID inválido")
	}
	if err := productsvc.NewProductService(db(c)).DeleteSaleUnit(p.ID, uint(unitID)); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"success": true})
}
