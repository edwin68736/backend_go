package service

// F5a: descuento de insumos al vender. Ver docs/recetas/F5_PROPUESTA.md.
//
// Con el descuento apagado nada de esto escribe: no se planifica y un *Plan nil responde «sin receta», así la venta queda exactamente
// como antes. El llamador planifica antes de su transacción y aplica dentro de ella.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

const (
	consumptionProbe = "table:tenant_recipe_consumptions"
	// consumptionSwitchEnv: interruptor de emergencia del servidor. RECIPES_CONSUMPTION=off deja de descontar en todas las empresas
	// sin migrar ni desplegar nada. Las reversas de lo ya consumido siguen funcionando.
	consumptionSwitchEnv = "RECIPES_CONSUMPTION"

	PolicyAllow = "allow" // vender y dejar el saldo del insumo en negativo
	PolicyBlock = "block" // no vender si falta insumo

	consumptionReferencePrefix = "RECETA/"
)

// ConsumptionReady: la BD del tenant tiene todo lo de V199, incluida la configuración. Sin alguna parte no se descuenta y las ventas
// siguen como antes, en vez de fallar.
func ConsumptionReady(db *gorm.DB) bool {
	return database.CachedSchemaProbe(db, consumptionProbe, func() bool {
		m := db.Migrator()
		return m.HasTable(&database.TenantRecipeConsumption{}) &&
			m.HasColumn("tenant_restaurant_settings", "recipes_enabled") && m.HasColumn("tenant_restaurant_settings", "recipes_consume")
	})
}

// ConsumptionSettings: lo que configura el administrador del restaurante.
type ConsumptionSettings struct {
	Consume     bool   `json:"consume"`
	StockPolicy string `json:"stock_policy"` // allow | block
}

type consumptionRow struct {
	RecipesEnabled     bool
	RecipesConsume     bool
	RecipesStockPolicy string
}

func readConsumptionRow(db *gorm.DB) (consumptionRow, error) {
	var row consumptionRow
	err := db.Table("tenant_restaurant_settings").Select("recipes_enabled, recipes_consume, recipes_stock_policy").
		Order("id").Limit(1).Scan(&row).Error
	return row, err
}

// ReadConsumptionSettings requiere ConsumptionReady.
func ReadConsumptionSettings(db *gorm.DB) (ConsumptionSettings, error) {
	row, err := readConsumptionRow(db)
	if err != nil {
		return ConsumptionSettings{}, err
	}
	out := ConsumptionSettings{Consume: row.RecipesConsume, StockPolicy: row.RecipesStockPolicy}
	if out.StockPolicy != PolicyBlock {
		out.StockPolicy = PolicyAllow
	}
	return out, nil
}

// SaveConsumptionSettings requiere ConsumptionReady.
func SaveConsumptionSettings(db *gorm.DB, in ConsumptionSettings) error {
	if in.StockPolicy != PolicyAllow && in.StockPolicy != PolicyBlock {
		return errors.New("política de stock no válida")
	}
	var cfg database.TenantRestaurantSetting
	if err := db.Order("id").Limit(1).Find(&cfg).Error; err != nil {
		return err
	}
	if cfg.ID == 0 {
		if err := db.Create(&cfg).Error; err != nil {
			return err
		}
	}
	return db.Table("tenant_restaurant_settings").Where("id = ?", cfg.ID).
		Updates(map[string]interface{}{"recipes_consume": in.Consume, "recipes_stock_policy": in.StockPolicy}).Error
}

// ConsumptionPolicy dice si hoy hay que descontar: módulo activado por el superadmin, descuento encendido por el restaurante, BD
// migrada y sin el interruptor de emergencia.
func ConsumptionPolicy(db *gorm.DB) (active, block bool, err error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(consumptionSwitchEnv)), "off") || !ConsumptionReady(db) {
		return false, false, nil
	}
	row, err := readConsumptionRow(db)
	if err != nil {
		return false, false, fmt.Errorf("leer la configuración de recetas: %w", err)
	}
	return row.RecipesEnabled && row.RecipesConsume, row.RecipesStockPolicy == PolicyBlock, nil
}

// Demand: algo vendido que puede tener receta.
type Demand struct {
	Line           int // posición de la línea de venta
	ProductID      uint
	PresentationID uint
	Quantity       float64 // unidades vendidas (en la unidad base del plato)
	Component      bool    // componente de un combo
}

type demandKey struct {
	line           int
	productID      uint
	presentationID uint
}

// Plan: qué insumos consume una venta. Un *Plan nil es válido y significa «no se descuenta nada».
type Plan struct {
	block      bool
	demands    []demandKey
	sold       map[demandKey]float64
	incomplete map[int]bool // líneas de combo con algún componente sin receta: su costo queda vacío
	recipes    map[[2]uint]database.TenantRecipe
	lines      map[uint][]database.TenantRecipeLine
	names      map[uint]string
}

// PlanConsumption carga las recetas de lo vendido. Los combos no tienen receta propia: sus componentes llegan como demandas.
func PlanConsumption(db *gorm.DB, block bool, demands []Demand) (*Plan, error) {
	ids := make([]uint, 0, len(demands))
	for _, d := range demands {
		if d.ProductID > 0 && d.Quantity > 0 {
			ids = append(ids, d.ProductID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	p := &Plan{block: block, sold: map[demandKey]float64{}, incomplete: map[int]bool{}, recipes: map[[2]uint]database.TenantRecipe{},
		lines: map[uint][]database.TenantRecipeLine{}, names: map[uint]string{}}
	var products []database.TenantProduct
	if err := db.Select("id", "name", "has_combo").Where("id IN ?", ids).Find(&products).Error; err != nil {
		return nil, err
	}
	dishes := make([]uint, 0, len(products))
	for _, pr := range products {
		if !pr.HasCombo {
			dishes = append(dishes, pr.ID)
			p.names[pr.ID] = pr.Name
		}
	}
	if len(dishes) == 0 {
		return nil, nil
	}
	var recipes []database.TenantRecipe
	if err := db.Where("product_id IN ?", dishes).Find(&recipes).Error; err != nil {
		return nil, err
	}
	if len(recipes) == 0 {
		return nil, nil
	}
	recipeIDs := make([]uint, 0, len(recipes))
	for _, r := range recipes {
		p.recipes[[2]uint{r.ProductID, r.PresentationID}] = r
		recipeIDs = append(recipeIDs, r.ID)
	}
	var lines []database.TenantRecipeLine
	if err := db.Where("recipe_id IN ?", recipeIDs).Order("recipe_id, sort_order, id").Find(&lines).Error; err != nil {
		return nil, err
	}
	for _, l := range lines {
		p.lines[l.RecipeID] = append(p.lines[l.RecipeID], l)
	}
	for _, d := range demands {
		if d.ProductID == 0 || d.Quantity <= 0 {
			continue
		}
		if !p.HasRecipe(d.ProductID, d.PresentationID) {
			if d.Component {
				p.incomplete[d.Line] = true
			}
			continue
		}
		k := demandKey{d.Line, d.ProductID, d.PresentationID}
		if _, seen := p.sold[k]; !seen {
			p.demands = append(p.demands, k)
		}
		p.sold[k] += d.Quantity
	}
	sort.SliceStable(p.demands, func(i, j int) bool {
		a, b := p.demands[i], p.demands[j]
		if a.line != b.line {
			return a.line < b.line
		}
		if a.productID != b.productID {
			return a.productID < b.productID
		}
		return a.presentationID < b.presentationID
	})
	return p, nil
}

// recipeFor: la receta de la presentación y, si no tiene propia, la del plato.
func (p *Plan) recipeFor(productID, presentationID uint) (database.TenantRecipe, bool) {
	r, ok := p.recipes[[2]uint{productID, presentationID}]
	if !ok && presentationID > 0 {
		r, ok = p.recipes[[2]uint{productID, 0}]
	}
	return r, ok && len(p.lines[r.ID]) > 0
}

// HasRecipe: se descuenta por receta, así que NO se descuenta su propio stock.
func (p *Plan) HasRecipe(productID, presentationID uint) bool {
	if p == nil {
		return false
	}
	_, ok := p.recipeFor(productID, presentationID)
	return ok
}

// SkipsOwnStock: para quien valida un solo plato (al agregar el pedido). Ante un error se valida como siempre.
func SkipsOwnStock(db *gorm.DB, productID, presentationID uint) bool {
	active, block, err := ConsumptionPolicy(db)
	if err != nil || !active {
		return false
	}
	p, err := PlanConsumption(db, block, []Demand{{ProductID: productID, PresentationID: presentationID, Quantity: 1}})
	return err == nil && p.HasRecipe(productID, presentationID)
}

// StockKeys: saldos de insumos que se van a mover, para bloquearlos junto con el resto del stock y en el mismo orden.
func (p *Plan) StockKeys(branchID uint) []invsvc.StockKey {
	if p == nil {
		return nil
	}
	var keys []invsvc.StockKey
	for _, k := range p.demands {
		r, _ := p.recipeFor(k.productID, k.presentationID)
		for _, l := range p.lines[r.ID] {
			keys = append(keys, invsvc.StockKey{ProductID: l.ComponentProductID, BranchID: branchID})
		}
	}
	return keys
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// consumedQty: vendido × cantidad servida ÷ (1 − merma) ÷ rendimiento, redondeado a los 3 decimales del stock.
func consumedQty(sold float64, line database.TenantRecipeLine, yield float64) float64 {
	if yield <= 0 || line.WastePct >= 100 {
		return 0
	}
	return round3(sold * line.QtyNet / (1 - line.WastePct/100) / yield)
}

// ConsumeInput: la venta ya creada. Items va en el mismo orden que las posiciones (Demand.Line).
type ConsumeInput struct {
	SaleID   uint
	Number   string
	BranchID uint
	UserID   uint
	Items    []database.TenantSaleItem
}

// ApplyTx saca los insumos, registra cada consumo y guarda el costo de receta en la línea. Usa solo la transacción recibida.
func (p *Plan) ApplyTx(tx *gorm.DB, in ConsumeInput) error {
	if p == nil || len(p.demands) == 0 {
		return nil
	}
	inv := invsvc.NewInventoryService(tx)
	cost := map[int]float64{}
	unknown := map[int]bool{}
	used := []int{}
	for _, k := range p.demands {
		if k.line < 0 || k.line >= len(in.Items) || in.Items[k.line].ID == 0 {
			return errors.New("no se pudo asociar el consumo de receta con la línea de venta")
		}
		item := in.Items[k.line]
		recipe, _ := p.recipeFor(k.productID, k.presentationID)
		if len(used) == 0 || used[len(used)-1] != k.line {
			used = append(used, k.line) // las demandas vienen ordenadas por línea
		}
		for _, l := range p.lines[recipe.ID] {
			row := database.TenantRecipeConsumption{
				SaleID: in.SaleID, SaleItemID: item.ID, SourceProductID: k.productID, SourcePresentationID: k.presentationID,
				SupplyID: l.ComponentProductID, BranchID: in.BranchID, RecipeID: recipe.ID, RecipeVersion: recipe.Version,
				Quantity: consumedQty(p.sold[k], l, recipe.YieldQty),
			}
			if row.Quantity > 0 {
				mv, err := inv.RecordMovementResultTx(tx, invsvc.MovementInput{
					ProductID: l.ComponentProductID, BranchID: in.BranchID, Type: "out", Quantity: row.Quantity,
					Reference: consumptionReferencePrefix + in.Number, Notes: p.names[k.productID], UserID: in.UserID,
					OperationCode: "RECIPE_CONSUMPTION", AllowNegative: !p.block,
				})
				if err != nil {
					return fmt.Errorf("ingrediente de «%s»: %w", p.names[k.productID], err)
				}
				row.MovementID = mv.ID
				if mv.CostUnknown {
					unknown[k.line] = true
				} else {
					unitCost := mv.UnitCost
					row.UnitCost = &unitCost
					cost[k.line] += row.Quantity * unitCost
				}
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
	}
	for _, line := range used {
		item := in.Items[line]
		if p.incomplete[line] || unknown[line] || item.Quantity <= 0 {
			continue
		}
		total := math.Round(cost[line]*1e6) / 1e6
		if err := tx.Table("tenant_sale_items").Where("id = ?", item.ID).Updates(map[string]interface{}{
			"recipe_cost": math.Round(total/item.Quantity*1e6) / 1e6, "recipe_cost_total": total,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// CopyLineCostTx: el comprobante emitido desde una nota de venta copia el costo de receta de la nota (no vuelve a consumir).
// items va en el mismo orden en que se crearon las líneas de la nota.
func CopyLineCostTx(tx *gorm.DB, fromSaleID uint, items []database.TenantSaleItem) error {
	if !ConsumptionReady(tx) {
		return nil
	}
	var source []struct {
		ProductID       *uint
		Quantity        float64
		RecipeCost      *float64
		RecipeCostTotal *float64
	}
	if err := tx.Table("tenant_sale_items").Select("product_id, quantity, recipe_cost, recipe_cost_total").
		Where("sale_id = ?", fromSaleID).Order("id").Scan(&source).Error; err != nil {
		return err
	}
	if len(source) != len(items) {
		return nil
	}
	same := func(a, b *uint) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	for i, src := range source {
		if src.RecipeCostTotal == nil || !same(src.ProductID, items[i].ProductID) || math.Abs(src.Quantity-items[i].Quantity) > 0.0005 {
			continue
		}
		if err := tx.Table("tenant_sale_items").Where("id = ?", items[i].ID).Updates(map[string]interface{}{
			"recipe_cost": src.RecipeCost, "recipe_cost_total": src.RecipeCostTotal,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
