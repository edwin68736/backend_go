package restaurant

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"

	productsvc "tukifac/internal/products/service"
	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
)

// F4a: recetas por las rutas de Tukichef, con stock y costo cargados como lo hace la pantalla (carga inicial). SQLite y MariaDB.

type recipeFixture struct {
	*buyFixture
	meat, bread, cheese, burger uint
}

func (f *recipeFixture) newSupply(code, name, unit string, qty, cost float64) uint {
	f.t.Helper()
	p, _, err := productsvc.NewProductService(f.db).Create(productsvc.ProductInput{IsSupply: true, Code: code, Name: name, Unit: unit,
		IgvAffectationType: "10", Active: true})
	ok(f.t, err)
	if qty > 0 {
		s, b := f.call("supervisor", "POST", "/supplies/"+u(p.ID)+"/initial-stock", map[string]any{"quantity": qty, "unit_cost": cost})
		wantStatus(f.t, s, b, 201)
	}
	return p.ID
}

func (f *recipeFixture) newDish(code, name string, price float64, mut func(*productsvc.ProductInput)) uint {
	f.t.Helper()
	in := productsvc.ProductInput{Code: code, Name: name, Unit: "NIU", SalePrice: price, IsRestaurant: true, Active: true,
		PriceIncludesIgv: true, IgvAffectationType: "10"}
	if mut != nil {
		mut(&in)
	}
	p, _, err := productsvc.NewProductService(f.db).Create(in)
	ok(f.t, err)
	return p.ID
}

func u(v uint) string { b, _ := json.Marshal(v); return string(b) }

func line(supply uint, qty float64, unit string, waste float64) map[string]any {
	return map[string]any{"supply_id": supply, "quantity": qty, "unit": unit, "waste_pct": waste}
}

func (f *recipeFixture) save(user string, dish, presentation uint, yield float64, lines ...map[string]any) (int, string) {
	f.t.Helper()
	return f.call(user, "PUT", "/recipes/"+u(dish), map[string]any{"presentation_id": presentation, "yield_qty": yield, "lines": lines})
}

func (f *recipeFixture) dishes(user, path string) []recipesvc.Dish {
	f.t.Helper()
	s, b := f.call(user, "GET", path, nil)
	wantStatus(f.t, s, b, 200)
	var out struct{ Data []recipesvc.Dish }
	ok(f.t, json.Unmarshal([]byte(b), &out))
	return out.Data
}

func (f *recipeFixture) dish(id uint) recipesvc.Dish {
	f.t.Helper()
	list := f.dishes("supervisor", "/recipes/"+u(id))
	if len(list) == 0 {
		f.t.Fatalf("plato %d sin datos", id)
	}
	return list[0]
}

func (f *recipeFixture) price(id uint) float64 {
	var p database.TenantProduct
	ok(f.t, f.db.First(&p, id).Error)
	return p.SalePrice
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func setupRecipes(t *testing.T) *recipeFixture {
	t.Helper()
	b := setupBuy(t)
	ok(t, b.db.AutoMigrate(&database.TenantProductPresentation{}, &database.TenantProductPresentationStock{}, &database.TenantProductBranchPrice{}))
	ok(t, (tenantmigrations.V198Recipes{}).Up(b.db))
	ok(t, (tenantmigrations.V198Recipes{}).Up(b.db)) // repetible
	f := &recipeFixture{buyFixture: b}
	f.meat = f.newSupply("CARNE", "Carne", "KGM", 2, 20)    // 150 g = 3.00 → alcanza para 13
	f.bread = f.newSupply("PAN", "Pan", "NIU", 30, 0.80)    // 0.80 → 30
	f.cheese = f.newSupply("QUESO", "Queso", "KGM", 1, 35) // 10 g = 0.35 → 100
	f.burger = f.newDish("BURGER", "Hamburguesa", 15, nil)
	return f
}

func (f *recipeFixture) burgerRecipe() {
	f.t.Helper()
	s, b := f.save("supervisor", f.burger, 0, 1, line(f.meat, 150, "GRM", 0), line(f.bread, 1, "", 0), line(f.cheese, 10, "GRM", 0))
	wantStatus(f.t, s, b, 200)
}

func TestRecipeCostPriceAndAvailability(t *testing.T) {
	f := setupRecipes(t)
	if d := f.dish(f.burger); d.HasRecipe || d.Costing != nil || d.Available != nil {
		t.Fatalf("sin receta no hay números: %+v", d)
	}
	f.burgerRecipe()
	if f.price(f.burger) != 15 {
		t.Fatal("guardar una receta no cambia el precio")
	}
	d := f.dish(f.burger)
	c := d.Costing
	if c == nil || !near(c.PortionCost, 4.15) || !near(c.SuggestedPrice, 16.40) || !near(c.SuggestedFoodCost, 29.86) || !near(c.FoodCostPct, 32.65) || !c.OverTarget {
		t.Fatalf("costo: %+v", c)
	}
	if d.Available == nil || *d.Available != 13 || d.Limiting != "Carne" {
		t.Fatalf("disponibilidad: %+v %s", d.Available, d.Limiting)
	}
	if len(d.Lines) != 3 || !near(d.Lines[0].Quantity, 150) || d.Lines[0].Unit != "GRM" || d.Lines[0].QtyNet != 0.15 {
		t.Fatalf("líneas: %+v", d.Lines)
	}

	// Merma y rendimiento: 100 g servidos con 20 % de merma = 125 g brutos; la receta rinde 5 porciones.
	s, b := f.save("supervisor", f.burger, 0, 5, line(f.cheese, 100, "GRM", 20))
	wantStatus(t, s, b, 200)
	d = f.dish(f.burger)
	if !near(d.Lines[0].QtyGross*1000, 125) || !near(d.Costing.PortionCost, 0.125*35/5) || *d.Available != 40 {
		t.Fatalf("merma: %+v %+v disp %d", d.Lines[0], d.Costing, *d.Available)
	}
	var recipe database.TenantRecipe
	ok(t, f.db.Where("product_id = ?", f.burger).First(&recipe).Error)
	if recipe.Version != 2 || f.count(&database.TenantRecipeLine{}, "recipe_id = ?", recipe.ID) != 1 {
		t.Fatalf("al guardar de nuevo se reemplazan las líneas: v%d", recipe.Version)
	}

	// Costo muy pequeño por gramo (0.0035).
	salt := f.newSupply("SAL", "Sal", "GRM", 1000, 0.0035)
	s, b = f.save("supervisor", f.burger, 0, 1, line(salt, 300, "", 0))
	wantStatus(t, s, b, 200)
	if d = f.dish(f.burger); !near(d.Costing.PortionCost, 1.05) || *d.Available != 3 {
		t.Fatalf("costo pequeño: %+v disp %d", d.Costing, *d.Available)
	}

	// Un ingrediente sin costo ni stock: incompleta, sin food cost ni sugerido, y no alcanza para ninguna.
	sauce := f.newSupply("SALSA", "Salsa", "KGM", 0, 0)
	s, b = f.save("supervisor", f.burger, 0, 1, line(f.meat, 150, "GRM", 0), line(sauce, 20, "GRM", 0))
	wantStatus(t, s, b, 200)
	d = f.dish(f.burger)
	if !d.Incomplete || d.Costing != nil || *d.Available != 0 || d.Limiting != "Salsa" || len(d.Missing) != 1 {
		t.Fatalf("incompleta: %+v", d)
	}

	// Exonerado y Nuevo RUS: el precio publicado es el neto.
	f.burgerRecipe()
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", f.burger).Update("igv_affectation_type", "20").Error)
	if d = f.dish(f.burger); !near(d.Costing.NetPrice, 15) || !near(d.Costing.SuggestedPrice, 13.90) {
		t.Fatalf("exonerado: %+v", d.Costing)
	}
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", f.burger).Update("igv_affectation_type", "10").Error)
	ok(t, f.db.Table("tenant_company_configs").Where("1 = 1").Update("taxpayer_regime", "NRUS").Error)
	if d = f.dish(f.burger); !near(d.Costing.NetPrice, 15) || !near(d.Costing.SuggestedPrice, 13.90) {
		t.Fatalf("nuevo RUS: %+v", d.Costing)
	}
}

func TestRecipeSettingsAndPermissions(t *testing.T) {
	f := setupRecipes(t)
	f.burgerRecipe()

	s, b := f.call("supervisor", "PUT", "/recipe-settings", map[string]any{"target_food_cost_pct": 25, "price_rounding": "0.50"})
	wantStatus(t, s, b, 200)
	// 4.15 / 25 % = 16.60 neto → 19.59 → 20.00
	if d := f.dish(f.burger); !near(d.Costing.SuggestedPrice, 20) || d.Costing.TargetFoodCostPct != 25 {
		t.Fatalf("configuración: %+v", d.Costing)
	}
	for _, bad := range []map[string]any{{"target_food_cost_pct": 0}, {"target_food_cost_pct": 100}, {"price_rounding": "5"}, {"recipe_cost_basis": "x"}} {
		s, b = f.call("supervisor", "PUT", "/recipe-settings", bad)
		wantStatus(t, s, b, 400)
	}
	// Objetivo propio de la receta.
	s, b = f.call("supervisor", "PUT", "/recipes/"+u(f.burger), map[string]any{"yield_qty": 1, "target_food_cost_pct": 40,
		"lines": []map[string]any{line(f.meat, 150, "GRM", 0)}})
	wantStatus(t, s, b, 200)
	if d := f.dish(f.burger); d.Costing.TargetFoodCostPct != 40 {
		t.Fatalf("objetivo propio: %+v", d.Costing)
	}
	// Base «último costo»: el promedio sigue en 20, el último pasa a 30.
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", f.meat).Update("purchase_price", 30).Error)
	if d := f.dish(f.burger); !near(d.Costing.PortionCost, 3) {
		t.Fatalf("promedio: %+v", d.Costing)
	}
	s, b = f.call("supervisor", "PUT", "/recipe-settings", map[string]any{"recipe_cost_basis": "last"})
	wantStatus(t, s, b, 200)
	if d := f.dish(f.burger); !near(d.Costing.PortionCost, 4.5) {
		t.Fatalf("último: %+v", d.Costing)
	}

	// Cocina: ve recetas y disponibilidad, nunca costos; no edita.
	for _, path := range []string{"/recipes", "/recipes/" + u(f.burger)} {
		s, b = f.call("cocinero", "GET", path, nil)
		wantStatus(t, s, b, 200)
		for _, key := range []string{`"costing"`, `"supply_cost"`, `"line_cost"`, `"portion_cost"`, `"settings"`, `"suggested_price"`,
			`"cost_missing"`, `"recipe_target_food_cost_pct"`} {
			if strings.Contains(b, key) {
				t.Fatalf("%s expone %s al cocinero: %s", path, key, b)
			}
		}
		if !strings.Contains(b, `"available"`) || !strings.Contains(b, `"can_view_cost":false`) {
			t.Fatal(b)
		}
	}
	denied := []struct{ user, method, path string }{
		{"cocinero", "PUT", "/recipes/" + u(f.burger)}, {"cocinero", "DELETE", "/recipes/" + u(f.burger)},
		{"cocinero", "GET", "/recipe-settings"}, {"cocinero", "PUT", "/recipe-settings"},
		{"cocinero", "POST", "/recipes/" + u(f.burger) + "/apply-price"}, {"cocinero", "GET", "/recipes/" + u(f.burger) + "/price-history"},
		{"mozo", "GET", "/recipes"}, {"mozo", "GET", "/recipes/" + u(f.burger)}, {"mozo", "PUT", "/recipes/" + u(f.burger)},
	}
	for _, d := range denied {
		s, b = f.call(d.user, d.method, d.path, map[string]any{})
		if s != 403 {
			t.Fatalf("%s %s %s: %d %s", d.user, d.method, d.path, s, b)
		}
	}

	// Validaciones de la receta.
	combo := f.newDish("COMBO", "Combo", 20, nil)
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", combo).Update("has_combo", true).Error)
	for name, lines := range map[string][]map[string]any{
		"sin líneas":          {},
		"no es insumo":        {line(f.dish0(), 1, "", 0)},
		"unidad incompatible": {line(f.bread, 1, "GRM", 0)},
		"cantidad cero":       {line(f.meat, 0, "", 0)},
		"merma 100":           {line(f.meat, 1, "", 100)},
		"repetido":            {line(f.meat, 1, "", 0), line(f.meat, 2, "", 0)},
	} {
		s, b = f.save("supervisor", f.burger, 0, 1, lines...)
		if s != 400 {
			t.Fatalf("%s: %d %s", name, s, b)
		}
	}
	s, b = f.save("supervisor", combo, 0, 1, line(f.meat, 1, "", 0))
	wantStatus(t, s, b, 400)
	s, b = f.save("supervisor", f.meat, 0, 1, line(f.bread, 1, "", 0))
	wantStatus(t, s, b, 404)
	s, b = f.save("supervisor", f.burger, 999, 1, line(f.bread, 1, "", 0))
	wantStatus(t, s, b, 400)

	// Plato exclusivo de otra sucursal: no existe aquí.
	other := f.newDish("OTRA", "De otra sucursal", 10, nil)
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", other).Update("branch_id", 2).Error)
	s, b = f.call("supervisor", "GET", "/recipes/"+u(other), nil)
	wantStatus(t, s, b, 404)
	for _, d := range f.dishes("supervisor", "/recipes") {
		if d.ProductID == other || d.ProductID == f.meat {
			t.Fatalf("la lista no debe traer %s", d.Name)
		}
	}

	// Módulo apagado.
	ok(t, database.SetRecipesEnabled(f.db, false))
	s, b = f.call("supervisor", "GET", "/recipes", nil)
	wantStatus(t, s, b, 403)
}

// dish0 un producto que no es insumo, para intentar usarlo como ingrediente.
func (f *recipeFixture) dish0() uint { return f.buyFixture.dish }

func TestRecipeApplyPrice(t *testing.T) {
	f := setupRecipes(t)
	f.burgerRecipe()
	path := "/recipes/" + u(f.burger) + "/apply-price"

	s, b := f.call("supervisor", "POST", path, map[string]any{"current_price": 14, "new_price": 16.40})
	wantStatus(t, s, b, 409)
	s, b = f.call("supervisor", "POST", path, map[string]any{"current_price": 15, "new_price": 0})
	wantStatus(t, s, b, 400)
	if f.price(f.burger) != 15 || f.count(&database.TenantPriceChange{}, "1 = 1") != 0 {
		t.Fatal("nada debía cambiar")
	}
	s, b = f.call("supervisor", "POST", path, map[string]any{"current_price": 15, "new_price": 16.40})
	wantStatus(t, s, b, 200)
	var change database.TenantPriceChange
	ok(t, f.db.First(&change).Error)
	if !near(f.price(f.burger), 16.40) || change.PriceBefore != 15 || !near(change.PriceAfter, 16.40) || change.UserID != 2 || change.BranchID != 0 {
		t.Fatalf("precio general: %v %+v", f.price(f.burger), change)
	}

	// La sucursal con precio propio cambia el suyo; el general no se toca.
	ok(t, f.db.Create(&database.TenantProductBranchPrice{ProductID: f.burger, BranchID: 1, SalePrice: 18, Active: true}).Error)
	if d := f.dish(f.burger); !d.BranchPrice || d.Price != 18 {
		t.Fatalf("precio de sucursal: %+v", d)
	}
	s, b = f.call("supervisor", "POST", path, map[string]any{"current_price": 18, "new_price": 19})
	wantStatus(t, s, b, 200)
	var override database.TenantProductBranchPrice
	ok(t, f.db.Where("product_id = ?", f.burger).First(&override).Error)
	if override.SalePrice != 19 || !near(f.price(f.burger), 16.40) || !strings.Contains(b, `"applied_branch_id":1`) {
		t.Fatalf("precio de sucursal: %v general %v %s", override.SalePrice, f.price(f.burger), b)
	}
	s, b = f.call("supervisor", "GET", "/recipes/"+u(f.burger)+"/price-history", nil)
	wantStatus(t, s, b, 200)
	var hist struct{ Data []database.TenantPriceChange }
	ok(t, json.Unmarshal([]byte(b), &hist))
	if len(hist.Data) != 2 || hist.Data[0].BranchID != 1 {
		t.Fatalf("historial: %+v", hist.Data)
	}

	// Dos personas confirman a la vez sobre el mismo precio: solo una lo cambia (MariaDB; SQLite no bloquea filas).
	if f.mysql {
		soup := f.newDish("SOPA", "Sopa", 10, nil)
		const n = 6
		codes := make([]int, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				codes[i], _ = f.call("supervisor", "POST", "/recipes/"+u(soup)+"/apply-price", map[string]any{"current_price": 10, "new_price": 11})
			}(i)
		}
		close(start)
		wg.Wait()
		got := map[int]int{}
		for _, c := range codes {
			got[c]++
		}
		if got[200] != 1 || got[409] != n-1 {
			t.Fatalf("concurrencia: %v", got)
		}
		if f.count(&database.TenantPriceChange{}, "product_id = ?", soup) != 1 {
			t.Fatal("un solo registro de cambio")
		}
	}
}

func TestRecipePresentationsAndCombos(t *testing.T) {
	f := setupRecipes(t)
	pizza := f.newDish("PIZZA", "Pizza", 0, nil)
	small := database.TenantProductPresentation{ProductID: pizza, Name: "Personal", SalePrice: 20, Active: true}
	big := database.TenantProductPresentation{ProductID: pizza, Name: "Familiar", SalePrice: 45, SortOrder: 1, Active: true}
	ok(t, f.db.Create(&small).Error)
	ok(t, f.db.Create(&big).Error)
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", pizza).Update("has_variants", true).Error)

	s, b := f.save("supervisor", pizza, 0, 1, line(f.cheese, 100, "GRM", 0)) // común: 3.50
	wantStatus(t, s, b, 200)
	s, b = f.save("supervisor", pizza, big.ID, 1, line(f.cheese, 300, "GRM", 0)) // familiar: 10.50
	wantStatus(t, s, b, 200)
	list := f.dishes("supervisor", "/recipes/"+u(pizza))
	if len(list) != 2 || !list[0].Inherited || !near(list[0].Costing.PortionCost, 3.5) || list[1].Inherited || !near(list[1].Costing.PortionCost, 10.5) {
		t.Fatalf("presentaciones: %+v", list)
	}
	if *list[0].Available != 10 || *list[1].Available != 3 || !strings.Contains(b, `"base_recipe":{`) {
		t.Fatalf("disponibilidad/base: %d %d %s", *list[0].Available, *list[1].Available, b)
	}
	// Aplicar precio a una presentación cambia solo esa; sin elegir presentación no se permite.
	path := "/recipes/" + u(pizza) + "/apply-price"
	s, b = f.call("supervisor", "POST", path, map[string]any{"current_price": 0, "new_price": 30})
	wantStatus(t, s, b, 400)
	s, b = f.call("supervisor", "POST", path, map[string]any{"presentation_id": big.ID, "current_price": 45, "new_price": 48})
	wantStatus(t, s, b, 200)
	var p1, p2 database.TenantProductPresentation
	ok(t, f.db.First(&p1, small.ID).Error)
	ok(t, f.db.First(&p2, big.ID).Error)
	if p1.SalePrice != 20 || p2.SalePrice != 48 {
		t.Fatalf("precios: %v %v", p1.SalePrice, p2.SalePrice)
	}
	// Al quitar la receta propia vuelve a heredar la común.
	s, b = f.call("supervisor", "DELETE", "/recipes/"+u(pizza)+"?presentation_id="+u(big.ID), nil)
	wantStatus(t, s, b, 200)
	if list = f.dishes("supervisor", "/recipes/"+u(pizza)); !list[1].Inherited || !near(list[1].Costing.PortionCost, 3.5) {
		t.Fatalf("herencia: %+v", list[1])
	}

	// Combo: suma de componentes por defecto; si uno no tiene receta, incompleto.
	f.burgerRecipe()
	drink := f.newDish("CHICHA", "Chicha", 5, nil)
	other := f.newDish("GASEOSA", "Gaseosa", 5, nil)
	combo := f.newDish("COMBO", "Combo", 25, nil)
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", combo).Update("has_combo", true).Error)
	fixed := database.TenantComboGroup{ProductID: combo, Name: "Plato", SelectionType: "fixed", Active: true}
	pick := database.TenantComboGroup{ProductID: combo, Name: "Bebida", SelectionType: "single", SortOrder: 1, Active: true}
	ok(t, f.db.Create(&fixed).Error)
	ok(t, f.db.Create(&pick).Error)
	ok(t, f.db.Create(&database.TenantComboGroupItem{GroupID: fixed.ID, ProductID: f.burger, DefaultQuantity: 2, Active: true}).Error)
	ok(t, f.db.Create(&database.TenantComboGroupItem{GroupID: pick.ID, ProductID: drink, DefaultQuantity: 1, IsDefault: true, Active: true}).Error)
	ok(t, f.db.Create(&database.TenantComboGroupItem{GroupID: pick.ID, ProductID: other, DefaultQuantity: 1, SortOrder: 1, Active: true}).Error)
	d := f.dish(combo)
	if !d.IsCombo || !d.Incomplete || d.Costing != nil || len(d.Missing) != 1 || !strings.Contains(d.Missing[0], "Chicha") {
		t.Fatalf("combo incompleto: %+v", d)
	}
	lemon := f.newSupply("LIMON", "Limón", "NIU", 8, 0.50)
	s, b = f.save("supervisor", drink, 0, 1, line(lemon, 2, "", 0))
	wantStatus(t, s, b, 200)
	d = f.dish(combo)
	// 2 hamburguesas (8.30) + chicha (1.00); la carne alcanza para 6 combos y el limón para 4.
	if d.Incomplete || !near(d.Costing.PortionCost, 9.30) || *d.Available != 4 || d.Limiting != "Limón" {
		t.Fatalf("combo: %+v %+v", d, d.Costing)
	}
	// Un ingrediente sin costo en un componente: se sabe para cuántos alcanza, pero no cuánto cuesta.
	ok(t, f.db.Table("tenant_product_stocks").Where("product_id = ?", lemon).Update("avg_unit_cost", nil).Error)
	ok(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", lemon).Update("purchase_price", 0).Error)
	d = f.dish(combo)
	if !d.Incomplete || d.Costing != nil || !d.HasRecipe || d.Available == nil || *d.Available != 4 {
		t.Fatalf("combo sin costo: %+v", d)
	}
}

func TestRecipeProtectsSupplies(t *testing.T) {
	f := setupRecipes(t)
	ok(t, f.db.AutoMigrate(&database.TenantSaleItem{}))
	oil := f.newSupply("ACEITE", "Aceite", "LTR", 0, 0) // sin movimientos: solo la receta lo protege
	s, b := f.save("supervisor", f.burger, 0, 1, line(oil, 10, "MLT", 0))
	wantStatus(t, s, b, 200)
	ps := productsvc.NewProductService(f.db)
	if err := ps.Delete(oil); err == nil || !strings.Contains(err.Error(), "receta") {
		t.Fatalf("no debía borrarse: %v", err)
	}
	res, err := ps.BulkDeleteCatalog(productsvc.BulkDeleteCatalogInput{ProductIDs: []uint{oil}, Reason: "limpieza", UserID: 2, BranchID: 1})
	ok(t, err)
	if len(res.Deleted) != 0 || len(res.Blocked) != 1 || !strings.Contains(strings.Join(res.Blocked[0].Reasons, " "), "receta") {
		t.Fatalf("borrado masivo: %+v", res)
	}
	s, b = f.call("supervisor", "PUT", "/supplies/"+u(oil), map[string]any{"unit": "KGM"})
	wantStatus(t, s, b, 400)
	if !strings.Contains(b, "receta") {
		t.Fatal(b)
	}
	s, b = f.call("supervisor", "DELETE", "/recipes/"+u(f.burger), nil)
	wantStatus(t, s, b, 200)
	ok(t, ps.Delete(oil))

	// Al eliminar físicamente el plato se va su receta.
	f.burgerRecipe()
	res, err = ps.BulkDeleteCatalog(productsvc.BulkDeleteCatalogInput{ProductIDs: []uint{f.burger}, Reason: "limpieza", UserID: 2, BranchID: 1})
	ok(t, err)
	if len(res.Deleted) != 1 || f.count(&database.TenantRecipe{}, "1 = 1") != 0 || f.count(&database.TenantRecipeLine{}, "1 = 1") != 0 {
		t.Fatalf("receta huérfana: %+v", res)
	}
}
