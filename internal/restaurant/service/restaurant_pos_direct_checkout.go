package service

import (
	"strings"

	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/database"
	"tukifac/pkg/tax"
)

// checkoutDirect emite una venta rápida sin pasar por sesión/pedido/comandas.
//
// Una venta directa no gestiona mesa ni manda nada a cocina, así que todo ese andamiaje era
// coste puro: se creaba una sesión, un pedido y N comandas que BillTable borraba acto seguido.
// Aquí se llama al mismo servicio de ventas que usa el panel ERP, en una sola transacción.
//
// El flujo de mesas y el de takeaway/delivery (que sí generan comandas) NO pasan por aquí.
func (s *RestaurantPOSCheckoutService) checkoutDirect(
	in POSCheckoutInput,
	taxCfg tax.Config,
) (*database.TenantSale, error) {
	items, extraStock, err := s.resolveDirectSaleItems(in.Items)
	if err != nil {
		return nil, err
	}

	payments := make([]salesvc.PaymentInput, 0, len(in.Payments))
	for _, p := range in.Payments {
		payments = append(payments, salesvc.PaymentInput{
			Method:    p.Method,
			Amount:    p.Amount,
			Reference: p.Reference,
		})
	}

	// El POS manda el descuento global como modo+valor, o como importe suelto (legacy).
	discountMode := strings.TrimSpace(strings.ToLower(in.DiscountMode))
	discountValue := in.DiscountValue
	if discountValue <= 0 && in.DiscountAmount > 0 {
		discountMode = "amount"
		discountValue = in.DiscountAmount
	}

	return salesvc.NewSaleService(s.rs.db).Create(salesvc.CreateSaleInput{
		BranchID:            in.BranchID,
		ContactID:           in.ContactID,
		UserID:              in.UserID,
		CashSessionID:       in.CashSessionID,
		SeriesID:            in.SeriesID,
		DocType:             in.DocType,
		IssueDate:           in.IssueDate,
		Currency:            in.Currency,
		Notes:               in.Notes,
		Items:               items,
		Payments:            payments,
		GlobalDiscountMode:  discountMode,
		GlobalDiscountValue: discountValue,
		TaxConfig:           taxCfg,
		ExtraStockMovements: extraStock,
		CentralTenantID:     in.CentralTenantID,
	})
}

// resolveDirectSaleItems traduce el carrito a líneas de venta, resolviendo precio, IGV,
// modificadores y combos contra el catálogo. Devuelve además las salidas de kardex de los
// componentes de combo, que no son línea de venta pero sí salen del almacén.
func (s *RestaurantPOSCheckoutService) resolveDirectSaleItems(
	cart []NewOrderItem,
) ([]salesvc.SaleItemInput, []salesvc.ExtraStockMovement, error) {
	items := make([]salesvc.SaleItemInput, 0, len(cart))
	stockByProduct := map[uint]float64{}
	stockOrder := make([]uint, 0)

	for i := range cart {
		item := &cart[i]
		product, err := resolveRestaurantOrderItem(s.rs.db, item)
		if err != nil {
			return nil, nil, err
		}
		comboDrafts, err := resolveComboOrderItem(s.rs.db, item, product)
		if err != nil {
			return nil, nil, err
		}

		modifiersJSON := strings.TrimSpace(item.ModifiersJSON)
		if len(comboDrafts) > 0 {
			// El combo se factura como una línea con su precio fijo; los componentes viajan
			// como detalle y son los que mueven stock.
			modifiersJSON = comboDraftsSnapshot(comboDrafts)
			for _, d := range comboDrafts {
				if _, seen := stockByProduct[d.ProductID]; !seen {
					stockOrder = append(stockOrder, d.ProductID)
				}
				stockByProduct[d.ProductID] += item.Quantity * d.Quantity
			}
		}

		affType := strings.TrimSpace(item.IgvAffectationType)
		if affType == "" {
			affType = "10"
		}
		items = append(items, salesvc.SaleItemInput{
			ProductID:          item.ProductID,
			Code:               item.ProductCode,
			Description:        item.ProductName,
			Unit:               "NIU",
			Quantity:           item.Quantity,
			UnitPrice:          item.UnitPrice,
			IgvAffectationType: affType,
			PriceIncludesIgv:   item.PriceIncludesIgv,
			ModifiersJSON:      modifiersJSON,
		})
	}

	extra := make([]salesvc.ExtraStockMovement, 0, len(stockOrder))
	for _, productID := range stockOrder {
		extra = append(extra, salesvc.ExtraStockMovement{
			ProductID: productID,
			Quantity:  stockByProduct[productID],
		})
	}
	return items, extra, nil
}

// isDirectSaleCheckout: venta rápida sin sesión previa. Es el único caso que puede saltarse
// mesas y comandas; takeaway/delivery pueden mandar a cocina y dine_in gestiona la mesa.
func isDirectSaleCheckout(in POSCheckoutInput) bool {
	if in.SessionID != nil && *in.SessionID > 0 {
		return false
	}
	return normalizeOrderType(in.OrderType, nil) == OrderTypeQuickSale
}

