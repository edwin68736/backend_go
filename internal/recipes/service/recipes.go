package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/productbranch"
	"tukifac/pkg/tax"
	"tukifac/pkg/taxregime"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableProbe = "table:tenant_recipes"

// Ready: la BD del tenant tiene las tablas de recetas (V198).
func Ready(db *gorm.DB) bool {
	return database.CachedSchemaProbe(db, tableProbe, func() bool { return db.Migrator().HasTable(&database.TenantRecipe{}) })
}

// UsedAsIngredient: el insumo está en alguna receta de un plato vigente.
func UsedAsIngredient(db *gorm.DB, supplyID uint) bool {
	return len(UsedIngredients(db, []uint{supplyID})) > 0
}

// UsedIngredients: cuáles de esos productos son ingrediente de una receta vigente (en una sola consulta).
func UsedIngredients(db *gorm.DB, ids []uint) []uint {
	if len(ids) == 0 || !Ready(db) {
		return nil
	}
	var used []uint
	err := db.Table("tenant_recipe_lines AS l").
		Joins("JOIN tenant_recipes r ON r.id = l.recipe_id").
		Joins("JOIN tenant_products p ON p.id = r.product_id AND p.deleted_at IS NULL").
		Where("l.component_product_id IN ?", ids).
		Where("r.presentation_id = 0 OR EXISTS (SELECT 1 FROM tenant_product_presentations pr WHERE pr.id = r.presentation_id AND pr.deleted_at IS NULL)").
		Distinct().Pluck("l.component_product_id", &used).Error
	if err != nil {
		return nil
	}
	return used
}

// PurgeProduct borra las recetas de un plato que se elimina físicamente.
func PurgeProduct(tx *gorm.DB, productID uint) error {
	if err := tx.Where("recipe_id IN (?)", tx.Model(&database.TenantRecipe{}).Select("id").Where("product_id = ?", productID)).
		Delete(&database.TenantRecipeLine{}).Error; err != nil {
		return err
	}
	return tx.Where("product_id = ?", productID).Delete(&database.TenantRecipe{}).Error
}

type Service struct{ db *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{db: db} }

// ── Configuración ──

func (s *Service) Settings() Settings {
	out := DefaultSettings()
	var row struct {
		TargetFoodCostPct float64
		PriceRounding     string
		RoundingDirection string
		RecipeCostBasis   string
	}
	err := s.db.Table("tenant_restaurant_settings").
		Select("target_food_cost_pct, price_rounding, rounding_direction, recipe_cost_basis").Order("id").Limit(1).Scan(&row).Error
	if err != nil || row.TargetFoodCostPct <= 0 {
		return out
	}
	return Settings{TargetFoodCostPct: row.TargetFoodCostPct, PriceRounding: row.PriceRounding, RoundingDirection: row.RoundingDirection,
		CostBasis: row.RecipeCostBasis}
}

func (s *Service) SaveSettings(in Settings) error {
	if in.TargetFoodCostPct <= 0 || in.TargetFoodCostPct >= 100 {
		return errors.New("el food cost objetivo debe estar entre 0 y 100")
	}
	if !map[string]bool{"none": true, "0.10": true, "0.50": true, "1.00": true, "x.90": true}[in.PriceRounding] {
		return errors.New("redondeo no válido")
	}
	if in.RoundingDirection != "up" && in.RoundingDirection != "nearest" {
		return errors.New("dirección de redondeo no válida")
	}
	if in.CostBasis != "avg" && in.CostBasis != "last" {
		return errors.New("base de costo no válida")
	}
	var cfg database.TenantRestaurantSetting
	if err := s.db.Order("id").Limit(1).Find(&cfg).Error; err != nil {
		return err
	}
	if cfg.ID == 0 {
		if err := s.db.Create(&cfg).Error; err != nil {
			return err
		}
	}
	return s.db.Table("tenant_restaurant_settings").Where("id = ?", cfg.ID).Updates(map[string]interface{}{
		"target_food_cost_pct": in.TargetFoodCostPct, "price_rounding": in.PriceRounding,
		"rounding_direction": in.RoundingDirection, "recipe_cost_basis": in.CostBasis,
	}).Error
}

// ── Guardar y borrar ──

type LineInput struct {
	SupplyID uint    `json:"supply_id"`
	Quantity float64 `json:"quantity"` // cantidad servida (neta), en Unit
	Unit     string  `json:"unit"`     // vacío = unidad base del insumo; GRM/MLT/KGM/LTR si convierte
	WastePct float64 `json:"waste_pct"`
}

type SaveInput struct {
	ProductID         uint
	PresentationID    uint
	YieldQty          float64
	TargetFoodCostPct *float64
	Lines             []LineInput
	UserID            uint
}

// dish devuelve el plato que puede tener receta.
func (s *Service) dish(productID, presentationID uint) (*database.TenantProduct, error) {
	var p database.TenantProduct
	if err := s.db.First(&p, productID).Error; err != nil {
		return nil, errors.New("plato no encontrado")
	}
	if p.IsSupply || !p.IsRestaurant || strings.EqualFold(p.Type, "service") {
		return nil, errors.New("las recetas son para platos de la carta")
	}
	if p.HasCombo {
		return nil, errors.New("un combo no tiene receta propia: su costo sale de sus componentes")
	}
	if presentationID > 0 {
		var n int64
		if err := s.db.Model(&database.TenantProductPresentation{}).Where("id = ? AND product_id = ?", presentationID, productID).Count(&n).Error; err != nil || n == 0 {
			return nil, errors.New("la presentación no pertenece al plato")
		}
	}
	return &p, nil
}

func (s *Service) Save(in SaveInput) error {
	if _, err := s.dish(in.ProductID, in.PresentationID); err != nil {
		return err
	}
	if in.YieldQty <= 0 {
		return errors.New("indica cuántas porciones rinde")
	}
	if t := in.TargetFoodCostPct; t != nil && (*t <= 0 || *t >= 100) {
		return errors.New("el food cost objetivo debe estar entre 0 y 100")
	}
	if len(in.Lines) == 0 {
		return errors.New("agrega al menos un ingrediente")
	}
	lines := make([]database.TenantRecipeLine, 0, len(in.Lines))
	seen := map[uint]bool{}
	for i, l := range in.Lines {
		var supply database.TenantProduct
		if err := s.db.Where("id = ? AND is_supply = ?", l.SupplyID, true).First(&supply).Error; err != nil {
			return errors.New("solo se pueden usar insumos como ingredientes")
		}
		if seen[supply.ID] {
			return fmt.Errorf("«%s» está repetido", supply.Name)
		}
		seen[supply.ID] = true
		if l.Quantity <= 0 {
			return fmt.Errorf("indica la cantidad de «%s»", supply.Name)
		}
		if l.WastePct < 0 || l.WastePct >= 100 {
			return fmt.Errorf("la merma de «%s» debe estar entre 0 y 99", supply.Name)
		}
		factor, ok := unitFactor(supply.Unit, l.Unit)
		if !ok {
			return fmt.Errorf("«%s» se mide en %s: no se puede escribir en %s", supply.Name, supply.Unit, l.Unit)
		}
		qty := round6(l.Quantity * factor)
		if qty <= 0 {
			return fmt.Errorf("la cantidad de «%s» es demasiado pequeña", supply.Name)
		}
		lines = append(lines, database.TenantRecipeLine{ComponentProductID: supply.ID, QtyNet: qty,
			DisplayUnit: strings.ToUpper(strings.TrimSpace(l.Unit)), WastePct: l.WastePct, SortOrder: i})
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var recipe database.TenantRecipe
		if err := tx.Where("product_id = ? AND presentation_id = ?", in.ProductID, in.PresentationID).Limit(1).Find(&recipe).Error; err != nil {
			return err
		}
		if recipe.ID == 0 {
			recipe = database.TenantRecipe{ProductID: in.ProductID, PresentationID: in.PresentationID, Version: 1}
		} else {
			recipe.Version++
		}
		recipe.YieldQty, recipe.TargetFoodCostPct, recipe.UpdatedBy = in.YieldQty, in.TargetFoodCostPct, in.UserID
		if err := tx.Save(&recipe).Error; err != nil {
			return err
		}
		if err := tx.Where("recipe_id = ?", recipe.ID).Delete(&database.TenantRecipeLine{}).Error; err != nil {
			return err
		}
		for i := range lines {
			lines[i].RecipeID = recipe.ID
		}
		return tx.Create(&lines).Error
	})
}

func (s *Service) Delete(productID, presentationID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var recipe database.TenantRecipe
		if err := tx.Where("product_id = ? AND presentation_id = ?", productID, presentationID).Limit(1).Find(&recipe).Error; err != nil {
			return err
		}
		if recipe.ID == 0 {
			return errors.New("ese plato no tiene receta")
		}
		if err := tx.Where("recipe_id = ?", recipe.ID).Delete(&database.TenantRecipeLine{}).Error; err != nil {
			return err
		}
		return tx.Delete(&recipe).Error
	})
}

// ── Lectura: costo, precio y disponibilidad ──

type Line struct {
	SupplyID    uint     `json:"supply_id"`
	Name        string   `json:"name"`
	BaseUnit    string   `json:"base_unit"`
	Quantity    float64  `json:"quantity"` // en DisplayUnit
	Unit        string   `json:"unit"`
	QtyNet      float64  `json:"qty_net"` // en unidad base
	WastePct    float64  `json:"waste_pct"`
	QtyGross    float64  `json:"qty_gross"`
	Stock       float64  `json:"stock"`
	SupplyCost  *float64 `json:"supply_cost,omitempty"` // por unidad base
	LineCost    *float64 `json:"line_cost,omitempty"`
	CostMissing bool     `json:"cost_missing,omitempty"`
}

// Dish un plato (o una de sus presentaciones) con lo que cuesta y para cuánto alcanza.
type Dish struct {
	ProductID        uint     `json:"product_id"`
	PresentationID   uint     `json:"presentation_id"`
	Name             string   `json:"name"`
	PresentationName string   `json:"presentation_name,omitempty"`
	CategoryID       uint     `json:"category_id,omitempty"`
	CategoryName     string   `json:"category_name,omitempty"`
	Price            float64  `json:"price"`
	BranchPrice      bool     `json:"branch_price"` // el precio es propio de la sucursal
	IsCombo          bool     `json:"is_combo"`
	HasRecipe        bool     `json:"has_recipe"`
	Inherited        bool     `json:"inherited,omitempty"` // la presentación usa la receta del plato
	Incomplete       bool     `json:"incomplete"`
	Missing          []string `json:"missing,omitempty"` // ingredientes sin costo o componentes sin receta
	Available        *int     `json:"available,omitempty"`
	Limiting         string   `json:"limiting,omitempty"`
	YieldQty         float64  `json:"yield_qty,omitempty"`
	TargetPct        *float64 `json:"recipe_target_food_cost_pct,omitempty"`
	Costing          *Costing `json:"costing,omitempty"`
	Lines            []Line   `json:"lines,omitempty"`
}

type snapshot struct {
	settings Settings
	taxCfg   tax.Config
	nrus     bool
	recipes  map[[2]uint]database.TenantRecipe
	lines    map[uint][]database.TenantRecipeLine
	supplies map[uint]database.TenantProduct
	stock    map[uint]float64
	avg      map[uint]float64
	groups   map[uint][]database.TenantComboGroup     // por combo
	items    map[uint][]database.TenantComboGroupItem // por grupo
	names    map[uint]string
}

// load reúne en pocas consultas todo lo que se necesita para la sucursal.
func (s *Service) load(branchID uint) (*snapshot, error) {
	snap := &snapshot{settings: s.Settings(), taxCfg: tax.LoadFromDB(s.db), recipes: map[[2]uint]database.TenantRecipe{},
		lines: map[uint][]database.TenantRecipeLine{}, supplies: map[uint]database.TenantProduct{}, stock: map[uint]float64{}, avg: map[uint]float64{}}
	var company database.TenantCompanyConfig
	if err := s.db.Select("taxpayer_regime").Limit(1).Find(&company).Error; err == nil {
		snap.nrus = taxregime.Normalize(company.TaxpayerRegime) == taxregime.NRUS
	}
	var recipes []database.TenantRecipe
	if err := s.db.Find(&recipes).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(recipes))
	for _, r := range recipes {
		snap.recipes[[2]uint{r.ProductID, r.PresentationID}] = r
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		return snap, nil
	}
	var lines []database.TenantRecipeLine
	if err := s.db.Where("recipe_id IN ?", ids).Order("recipe_id, sort_order, id").Find(&lines).Error; err != nil {
		return nil, err
	}
	supplyIDs := make([]uint, 0, len(lines))
	for _, l := range lines {
		snap.lines[l.RecipeID] = append(snap.lines[l.RecipeID], l)
		supplyIDs = append(supplyIDs, l.ComponentProductID)
	}
	var supplies []database.TenantProduct
	if err := s.db.Unscoped().Where("id IN ?", supplyIDs).Find(&supplies).Error; err != nil {
		return nil, err
	}
	for _, p := range supplies {
		snap.supplies[p.ID] = p
	}
	var stocks []struct {
		ProductID   uint
		Quantity    float64
		AvgUnitCost *float64
	}
	if err := s.db.Table("tenant_product_stocks").Select("product_id, quantity, avg_unit_cost").
		Where("product_id IN ? AND branch_id = ?", supplyIDs, branchID).Find(&stocks).Error; err != nil {
		return nil, err
	}
	for _, st := range stocks {
		snap.stock[st.ProductID] += st.Quantity
		if st.AvgUnitCost != nil {
			snap.avg[st.ProductID] = *st.AvgUnitCost
		}
	}
	return snap, nil
}

// supplyCost: promedio de la sucursal o último costo, según la configuración; si falta uno se usa el otro.
func (snap *snapshot) supplyCost(id uint) (float64, bool) {
	avg, last := snap.avg[id], snap.supplies[id].PurchasePrice
	first, second := avg, last
	if snap.settings.CostBasis == "last" {
		first, second = last, avg
	}
	if first > 0 {
		return first, true
	}
	if second > 0 {
		return second, true
	}
	return 0, false
}

// portion: costo por porción y cantidad bruta de cada insumo que consume UNA porción.
type portion struct {
	cost    float64
	gross   map[uint]float64
	missing []string
}

func (snap *snapshot) portionOf(r database.TenantRecipe) (portion, []Line) {
	p := portion{gross: map[uint]float64{}}
	lines := make([]Line, 0, len(snap.lines[r.ID]))
	for _, l := range snap.lines[r.ID] {
		supply := snap.supplies[l.ComponentProductID]
		gross := l.QtyNet / (1 - l.WastePct/100)
		factor, ok := unitFactor(supply.Unit, l.DisplayUnit)
		if !ok {
			factor = 1
		}
		view := Line{SupplyID: supply.ID, Name: supply.Name, BaseUnit: supply.Unit, Quantity: round6(l.QtyNet / factor), Unit: l.DisplayUnit,
			QtyNet: l.QtyNet, WastePct: l.WastePct, QtyGross: round6(gross), Stock: snap.stock[supply.ID]}
		if cost, ok := snap.supplyCost(supply.ID); ok {
			lineCost := round6(gross * cost)
			view.SupplyCost, view.LineCost = &cost, &lineCost
			p.cost += gross * cost / r.YieldQty
		} else {
			view.CostMissing = true
			p.missing = append(p.missing, supply.Name)
		}
		p.gross[supply.ID] += gross / r.YieldQty
		lines = append(lines, view)
	}
	return p, lines
}

// available: porciones que alcanzan con el stock de la sucursal; el saldo negativo cuenta como cero.
func (snap *snapshot) available(gross map[uint]float64) (int, string) {
	best, limiting := math.MaxInt32, ""
	ids := make([]uint, 0, len(gross))
	for id := range gross {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		need := gross[id]
		if need <= 0 {
			continue
		}
		n := int(math.Floor(math.Max(snap.stock[id], 0)/need + 1e-9))
		if n < best {
			best, limiting = n, snap.supplies[id].Name
		}
	}
	if best == math.MaxInt32 {
		return 0, ""
	}
	return best, limiting
}

func (snap *snapshot) priceTax(p database.TenantProduct) priceTax {
	rate := snap.taxCfg.EffectiveRate(p.IgvAffectationType) / 100
	if snap.nrus {
		rate = 0
	}
	return priceTax{rate: rate, includes: p.PriceIncludesIgv}
}

// carta: los platos que se venden en la sucursal, con la misma regla que el POS (asignación por sucursal o branch_id).
func (s *Service) carta(branchID uint) *gorm.DB {
	q := s.db.Model(&database.TenantProduct{}).Where("tenant_products.is_restaurant = ?", true).
		Where("(tenant_products.is_supply IS NULL OR tenant_products.is_supply = ?)", false)
	own := "(tenant_products.branch_id IS NULL OR tenant_products.branch_id = 0 OR tenant_products.branch_id = ?)"
	if productbranch.Ready(s.db) {
		return q.Where(productbranch.ListFilterSQL("tenant_products.id", own), branchID, branchID)
	}
	return q.Where(own, branchID)
}

// Visible: el plato se vende en la sucursal (aunque esté inactivo).
func (s *Service) Visible(branchID, productID uint) bool {
	var n int64
	return s.carta(branchID).Where("tenant_products.id = ?", productID).Count(&n).Error == nil && n > 0
}

func (s *Service) hasBranchPrices() bool {
	return database.CachedSchemaProbe(s.db, "table:tenant_product_branch_prices", func() bool {
		return s.db.Migrator().HasTable(&database.TenantProductBranchPrice{})
	})
}

// Query qué platos leer. ProductID 0 = toda la carta de la sucursal.
type Query struct {
	BranchID  uint
	ProductID uint
	WithCost  bool
	WithLines bool
}

func (s *Service) Dishes(q Query) ([]Dish, error) {
	snap, err := s.load(q.BranchID)
	if err != nil {
		return nil, err
	}
	return s.dishes(snap, q)
}

// Detail un plato y, si se vende por presentaciones, además su receta común (la que heredan las que no tienen propia).
func (s *Service) Detail(q Query) ([]Dish, *Dish, error) {
	snap, err := s.load(q.BranchID)
	if err != nil {
		return nil, nil, err
	}
	dishes, err := s.dishes(snap, q)
	if err != nil || len(dishes) == 0 || dishes[0].PresentationID == 0 {
		return dishes, nil, err
	}
	var p database.TenantProduct
	if err := s.db.First(&p, q.ProductID).Error; err != nil {
		return nil, nil, err
	}
	base := Dish{ProductID: p.ID, Name: p.Name}
	snap.fill(p, &base, q)
	if !base.HasRecipe {
		return dishes, nil, nil
	}
	base.Costing = nil // no tiene precio propio
	return dishes, &base, nil
}

func (s *Service) dishes(snap *snapshot, q Query) ([]Dish, error) {
	var products []database.TenantProduct
	pq := s.carta(q.BranchID).Order("tenant_products.name, tenant_products.id")
	if q.ProductID > 0 {
		pq = pq.Where("tenant_products.id = ?", q.ProductID)
	} else {
		pq = pq.Where("tenant_products.active = ?", true)
	}
	if err := pq.Find(&products).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	presentations := map[uint][]database.TenantProductPresentation{}
	branchPrices := map[uint]float64{}
	if len(ids) > 0 {
		var pres []database.TenantProductPresentation
		if err := s.db.Where("product_id IN ? AND active = ?", ids, true).Order("product_id, sort_order, id").Find(&pres).Error; err != nil {
			return nil, err
		}
		for _, pr := range pres {
			presentations[pr.ProductID] = append(presentations[pr.ProductID], pr)
		}
		if s.hasBranchPrices() {
			var prices []database.TenantProductBranchPrice
			if err := s.db.Where("product_id IN ? AND branch_id = ? AND active = ?", ids, q.BranchID, true).Find(&prices).Error; err != nil {
				return nil, err
			}
			for _, bp := range prices {
				branchPrices[bp.ProductID] = bp.SalePrice
			}
		}
	}
	var comboIDs []uint
	for _, p := range products {
		if p.HasCombo {
			comboIDs = append(comboIDs, p.ID)
		}
	}
	if err := s.loadCombos(snap, comboIDs); err != nil {
		return nil, err
	}
	categories, err := s.categoryNames(products)
	if err != nil {
		return nil, err
	}
	out := make([]Dish, 0, len(products))
	for _, p := range products {
		var categoryID uint
		if p.CategoryID != nil {
			categoryID = *p.CategoryID
		}
		variants := presentations[p.ID]
		if p.HasVariants && len(variants) > 0 {
			for _, pr := range variants {
				d := Dish{ProductID: p.ID, PresentationID: pr.ID, Name: p.Name, PresentationName: pr.Name, Price: pr.SalePrice,
					CategoryID: categoryID, CategoryName: categories[categoryID]}
				snap.fill(p, &d, q)
				out = append(out, d)
			}
			continue
		}
		d := Dish{ProductID: p.ID, Name: p.Name, Price: p.SalePrice, IsCombo: p.HasCombo, CategoryID: categoryID, CategoryName: categories[categoryID]}
		if bp, ok := branchPrices[p.ID]; ok {
			d.Price, d.BranchPrice = bp, true
		}
		snap.fill(p, &d, q)
		out = append(out, d)
	}
	return out, nil
}

// categoryNames: nombre de la categoría de cada plato, para filtrar la lista.
func (s *Service) categoryNames(products []database.TenantProduct) (map[uint]string, error) {
	var ids []uint
	for _, p := range products {
		if p.CategoryID != nil && *p.CategoryID > 0 {
			ids = append(ids, *p.CategoryID)
		}
	}
	out := map[uint]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   uint
		Name string
	}
	if err := s.db.Table("tenant_categories").Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

// loadCombos trae grupos, opciones y nombres de componentes de todos los combos de una vez.
func (s *Service) loadCombos(snap *snapshot, comboIDs []uint) error {
	snap.groups, snap.items, snap.names = map[uint][]database.TenantComboGroup{}, map[uint][]database.TenantComboGroupItem{}, map[uint]string{}
	if len(comboIDs) == 0 {
		return nil
	}
	var groups []database.TenantComboGroup
	if err := s.db.Where("product_id IN ? AND active = ?", comboIDs, true).Order("sort_order, id").Find(&groups).Error; err != nil {
		return err
	}
	groupIDs := make([]uint, 0, len(groups))
	for _, g := range groups {
		snap.groups[g.ProductID] = append(snap.groups[g.ProductID], g)
		groupIDs = append(groupIDs, g.ID)
	}
	if len(groupIDs) == 0 {
		return nil
	}
	var items []database.TenantComboGroupItem
	if err := s.db.Where("group_id IN ? AND active = ?", groupIDs, true).Order("sort_order, id").Find(&items).Error; err != nil {
		return err
	}
	componentIDs := make([]uint, 0, len(items))
	for _, it := range items {
		snap.items[it.GroupID] = append(snap.items[it.GroupID], it)
		componentIDs = append(componentIDs, it.ProductID)
	}
	if len(componentIDs) == 0 {
		return nil
	}
	var components []database.TenantProduct
	if err := s.db.Unscoped().Select("id", "name").Where("id IN ?", componentIDs).Find(&components).Error; err != nil {
		return err
	}
	for _, c := range components {
		snap.names[c.ID] = c.Name
	}
	return nil
}

func (snap *snapshot) fill(p database.TenantProduct, d *Dish, q Query) {
	var (
		cost   float64
		gross  map[uint]float64
		target = snap.settings.TargetFoodCostPct
	)
	if p.HasCombo {
		total, need, noRecipe, noCost := snap.combo(p.ID)
		d.HasRecipe = len(need) > 0 && len(noRecipe) == 0
		d.Missing = append(noRecipe, noCost...)
		d.Incomplete = len(d.Missing) > 0
		cost = total
		// Sin la receta de un componente no se sabe qué consume el combo: tampoco para cuántos alcanza.
		if len(noRecipe) == 0 {
			gross = need
		}
	} else {
		recipe, ok := snap.recipes[[2]uint{p.ID, d.PresentationID}]
		if !ok && d.PresentationID > 0 {
			recipe, ok = snap.recipes[[2]uint{p.ID, 0}]
			d.Inherited = ok
		}
		if !ok {
			return
		}
		por, lines := snap.portionOf(recipe)
		d.HasRecipe, d.YieldQty = true, recipe.YieldQty
		if q.WithCost {
			d.TargetPct = recipe.TargetFoodCostPct
		}
		d.Missing, d.Incomplete = por.missing, len(por.missing) > 0
		cost, gross = por.cost, por.gross
		if recipe.TargetFoodCostPct != nil {
			target = *recipe.TargetFoodCostPct
		}
		if q.WithLines {
			if !q.WithCost {
				for i := range lines {
					lines[i].SupplyCost, lines[i].LineCost, lines[i].CostMissing = nil, nil, false
				}
			}
			d.Lines = lines
		}
	}
	if len(gross) > 0 {
		n, limiting := snap.available(gross)
		d.Available, d.Limiting = &n, limiting
	}
	if !q.WithCost {
		d.Missing, d.Incomplete = nil, false
		return
	}
	// Con un ingrediente sin costo no se muestra food cost ni precio sugerido: serían falsos.
	if d.HasRecipe && !d.Incomplete {
		c := costing(cost, d.Price, snap.priceTax(p), target, snap.settings)
		d.Costing = &c
	}
}

// combo: costo y consumo de los componentes por defecto de cada grupo.
func (snap *snapshot) combo(comboID uint) (total float64, gross map[uint]float64, noRecipe, noCost []string) {
	gross = map[uint]float64{}
	for _, g := range snap.groups[comboID] {
		items := snap.items[g.ID]
		if len(items) == 0 {
			continue
		}
		chosen := items[:0:0]
		for _, it := range items {
			if it.IsDefault {
				chosen = append(chosen, it)
			}
		}
		if len(chosen) == 0 {
			if g.SelectionType == "fixed" {
				chosen = items
			} else {
				chosen = items[:1]
			}
		}
		for _, it := range chosen {
			qty := float64(it.DefaultQuantity)
			if qty <= 0 {
				qty = 1
			}
			recipe, ok := snap.recipes[[2]uint{it.ProductID, 0}]
			if !ok {
				noRecipe = append(noRecipe, snap.names[it.ProductID]+" (sin receta)")
				continue
			}
			por, _ := snap.portionOf(recipe)
			noCost = append(noCost, por.missing...)
			total += por.cost * qty
			for id, need := range por.gross {
				gross[id] += need * qty
			}
		}
	}
	return total, gross, noRecipe, noCost
}

// ── Aplicar precio ──

// ErrPriceChanged: el precio ya no es el que el usuario vio al confirmar.
var ErrPriceChanged = errors.New("el precio cambió mientras lo revisabas: vuelve a abrir el plato")

type ApplyInput struct {
	ProductID      uint
	PresentationID uint
	BranchID       uint
	CurrentPrice   float64 // el que se mostró al confirmar
	NewPrice       float64
	UserID         uint
}

// ApplyPrice cambia el precio publicado y lo registra. Si la sucursal tiene precio propio se cambia ese; si no, el general (o el de
// la presentación). Devuelve la sucursal afectada (0 = precio general).
func (s *Service) ApplyPrice(in ApplyInput) (uint, error) {
	if in.NewPrice <= 0 {
		return 0, errors.New("el precio debe ser mayor a cero")
	}
	p, err := s.dish(in.ProductID, in.PresentationID)
	if err != nil {
		// Un combo no tiene receta, pero sí precio.
		var combo database.TenantProduct
		if e := s.db.Where("id = ? AND has_combo = ? AND is_restaurant = ?", in.ProductID, true, true).First(&combo).Error; e != nil || in.PresentationID > 0 {
			return 0, err
		}
		p = &combo
	}
	price := round2(in.NewPrice)
	overrides := s.hasBranchPrices()
	var branchID uint
	err = s.db.Transaction(func(tx *gorm.DB) error {
		branchID = 0
		var before float64
		var update *gorm.DB
		switch {
		case in.PresentationID > 0:
			var pr database.TenantProductPresentation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pr, in.PresentationID).Error; err != nil {
				return err
			}
			before = pr.SalePrice
			update = tx.Model(&database.TenantProductPresentation{}).Where("id = ?", pr.ID)
		default:
			if p.HasVariants {
				return errors.New("este plato se vende por presentación: elige una")
			}
			var override database.TenantProductBranchPrice
			if overrides {
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_id = ? AND branch_id = ? AND active = ?", p.ID, in.BranchID, true).Limit(1).Find(&override).Error; err != nil {
					return err
				}
			}
			if override.ID > 0 {
				branchID, before = in.BranchID, override.SalePrice
				update = tx.Model(&database.TenantProductBranchPrice{}).Where("id = ?", override.ID)
			} else {
				var current database.TenantProduct
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, p.ID).Error; err != nil {
					return err
				}
				before = current.SalePrice
				update = tx.Model(&database.TenantProduct{}).Where("id = ?", p.ID)
			}
		}
		if math.Abs(before-in.CurrentPrice) > 0.005 {
			return ErrPriceChanged
		}
		if err := update.Update("sale_price", price).Error; err != nil {
			return err
		}
		return tx.Create(&database.TenantPriceChange{ProductID: p.ID, PresentationID: in.PresentationID, BranchID: branchID,
			PriceBefore: before, PriceAfter: price, Reason: "precio sugerido por receta", UserID: in.UserID}).Error
	})
	return branchID, err
}
