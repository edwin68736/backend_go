package handler

// Recetas para Tukichef (F4a): costo, precio sugerido y disponibilidad. Los costos solo viajan con recipes.view_cost y el precio
// de venta solo cambia con «aplicar precio», confirmado y registrado. No interviene en la venta.

import (
	"errors"
	"strconv"

	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/branch"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RequireRecipeTables: la BD del tenant ya tiene las tablas de recetas.
func (h *RestaurantHandler) RequireRecipeTables(c fiber.Ctx) error {
	if gdb := db(c); gdb != nil && recipesvc.Ready(gdb) {
		return c.Next()
	}
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Las recetas aún no están disponibles", "code": "recipes_unavailable"})
}

// recipeBranch: costos, stock y precio siempre son de UNA sucursal (la pedida si puede verla; si no, la activa).
func recipeBranch(c fiber.Ctx) uint {
	if b := supplyBranch(c); b > 0 && b != branch.NoBranchFilter {
		return b
	}
	return branch.ActiveBranchID(c)
}

// recipeProduct valida que el plato de la ruta se venda en la sucursal.
func recipeProduct(c fiber.Ctx, svc *recipesvc.Service) (uint, uint, bool) {
	id, err := strconv.ParseUint(c.Params("productId"), 10, 32)
	branchID := recipeBranch(c)
	if err != nil || id == 0 || !svc.Visible(branchID, uint(id)) {
		return 0, 0, false
	}
	return uint(id), branchID, true
}

func dishNotFound(c fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plato no encontrado"})
}

func (h *RestaurantHandler) ListRecipes(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	withCost := middleware.CanViewSupplyCost(c)
	branchID := recipeBranch(c)
	dishes, err := svc.Dishes(recipesvc.Query{BranchID: branchID, WithCost: withCost})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	out := fiber.Map{"data": dishes, "branch_id": branchID, "can_view_cost": withCost}
	if withCost {
		out["settings"] = svc.Settings()
	}
	return c.JSON(out)
}

func recipeDetail(c fiber.Ctx, svc *recipesvc.Service, productID, branchID uint) error {
	withCost := middleware.CanViewSupplyCost(c)
	dishes, base, err := svc.Detail(recipesvc.Query{BranchID: branchID, ProductID: productID, WithCost: withCost, WithLines: true})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": dishes, "base_recipe": base, "branch_id": branchID, "can_view_cost": withCost})
}

func (h *RestaurantHandler) GetRecipe(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	productID, branchID, ok := recipeProduct(c, svc)
	if !ok {
		return dishNotFound(c)
	}
	return recipeDetail(c, svc, productID, branchID)
}

func (h *RestaurantHandler) SaveRecipe(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	productID, branchID, ok := recipeProduct(c, svc)
	if !ok {
		return dishNotFound(c)
	}
	var body struct {
		PresentationID    uint                  `json:"presentation_id"`
		YieldQty          float64               `json:"yield_qty"`
		TargetFoodCostPct *float64              `json:"target_food_cost_pct"`
		Lines             []recipesvc.LineInput `json:"lines"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	in := recipesvc.SaveInput{ProductID: productID, PresentationID: body.PresentationID, YieldQty: body.YieldQty,
		TargetFoodCostPct: body.TargetFoodCostPct, Lines: body.Lines, UserID: uid(c)}
	// Quien no ve costos no puede fijar ni borrar el objetivo propio de la receta: se conserva el que había.
	if !middleware.CanViewSupplyCost(c) {
		var current database.TenantRecipe
		if err := db(c).Where("product_id = ? AND presentation_id = ?", productID, body.PresentationID).Limit(1).Find(&current).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		in.TargetFoodCostPct = current.TargetFoodCostPct
	}
	if err := svc.Save(in); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return recipeDetail(c, svc, productID, branchID)
}

func (h *RestaurantHandler) DeleteRecipe(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	productID, branchID, ok := recipeProduct(c, svc)
	if !ok {
		return dishNotFound(c)
	}
	presentationID, _ := strconv.ParseUint(c.Query("presentation_id"), 10, 32)
	if err := svc.Delete(productID, uint(presentationID)); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return recipeDetail(c, svc, productID, branchID)
}

func (h *RestaurantHandler) GetRecipeSettings(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": recipesvc.New(db(c)).Settings()})
}

func (h *RestaurantHandler) UpdateRecipeSettings(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	body := svc.Settings() // lo que no se envía se conserva
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	if err := svc.SaveSettings(body); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"data": svc.Settings()})
}

// RequireConsumptionTables: la BD del tenant ya tiene lo del descuento de insumos (V199).
func (h *RestaurantHandler) RequireConsumptionTables(c fiber.Ctx) error {
	if gdb := db(c); gdb != nil && recipesvc.ConsumptionReady(gdb) {
		return c.Next()
	}
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "El descuento de insumos aún no está disponible", "code": "recipes_unavailable"})
}

func (h *RestaurantHandler) GetRecipeConsumption(c fiber.Ctx) error {
	cfg, err := recipesvc.ReadConsumptionSettings(db(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": cfg})
}

func (h *RestaurantHandler) UpdateRecipeConsumption(c fiber.Ctx) error {
	cfg, err := recipesvc.ReadConsumptionSettings(db(c)) // lo que no se envía se conserva
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if err := c.Bind().JSON(&cfg); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	if err := recipesvc.SaveConsumptionSettings(db(c), cfg); err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return h.GetRecipeConsumption(c)
}

func (h *RestaurantHandler) ApplyRecipePrice(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	productID, branchID, ok := recipeProduct(c, svc)
	if !ok {
		return dishNotFound(c)
	}
	var body struct {
		PresentationID uint    `json:"presentation_id"`
		CurrentPrice   float64 `json:"current_price"`
		NewPrice       float64 `json:"new_price"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	applied, err := svc.ApplyPrice(recipesvc.ApplyInput{ProductID: productID, PresentationID: body.PresentationID, BranchID: branchID,
		CurrentPrice: body.CurrentPrice, NewPrice: body.NewPrice, UserID: uid(c)})
	if errors.Is(err, recipesvc.ErrPriceChanged) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error(), "code": "price_changed"})
	}
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	// applied_branch_id 0 = se cambió el precio general (todas las sucursales sin precio propio).
	return c.JSON(fiber.Map{"applied_branch_id": applied})
}

func (h *RestaurantHandler) RecipePriceHistory(c fiber.Ctx) error {
	svc := recipesvc.New(db(c))
	productID, branchID, ok := recipeProduct(c, svc)
	if !ok {
		return dishNotFound(c)
	}
	var rows []struct {
		database.TenantPriceChange
		UserName string `json:"user_name"`
	}
	err := db(c).Table("tenant_price_changes AS pc").Select("pc.*, u.name AS user_name").
		Joins("LEFT JOIN tenant_users u ON u.id = pc.user_id").
		Where("pc.product_id = ? AND pc.branch_id IN ?", productID, []uint{0, branchID}).Order("pc.id DESC").Limit(50).Scan(&rows).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": rows})
}
