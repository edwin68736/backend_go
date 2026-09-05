package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	cashbanksvc "tukifac/internal/cashbank/service"
	invsvc "tukifac/internal/inventory/service"
	"tukifac/pkg/database"
	"tukifac/pkg/money"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

type PurchaseService struct {
	db *gorm.DB
}

func NewPurchaseService(db *gorm.DB) *PurchaseService {
	return &PurchaseService{db: db}
}

type PurchaseItemInput struct {
	ProductID          *uint    `json:"product_id"`
	Code               string   `json:"code"`
	Description        string   `json:"description"`
	Unit               string   `json:"unit"`
	Quantity           float64  `json:"quantity"`
	UnitCost           float64  `json:"unit_cost"`
	TaxRate            float64  `json:"tax_rate"`             // referencial; se recalcula con IgvAffectationType
	IgvAffectationType string   `json:"igv_affectation_type"` // catálogo SUNAT N°07
	PriceIncludesIgv   bool     `json:"price_includes_igv"`
	Serials            []string `json:"serials"`
	// UpdateSalePrice: si true, actualiza sale_price del catálogo con NewSalePrice.
	UpdateSalePrice bool    `json:"update_sale_price"`
	NewSalePrice    float64 `json:"new_sale_price"`
}

type CreatePurchaseInput struct {
	BranchID      uint
	ContactID     *uint
	UserID        uint
	DocType       string
	Series        string
	Number        string
	IssueDate     time.Time
	DueDate       *time.Time
	Currency      string
	PaymentMethod string
	Status        string
	Notes         string
	// PriceIncludesIgv: criterio global elegido en el formulario. Se guarda como referencia
	// de cómo se registró la compra; el cálculo real usa el flag de cada ítem.
	PriceIncludesIgv bool
	Items            []PurchaseItemInput
	TaxConfig        tax.Config
}

// validatePurchaseItems valida reglas de negocio de ítems antes de persistir la compra.
func validatePurchaseItems(items []PurchaseItemInput) error {
	for _, item := range items {
		if item.UpdateSalePrice && item.NewSalePrice <= 0 {
			label := strings.TrimSpace(item.Description)
			if label == "" {
				label = "producto"
			}
			return fmt.Errorf("precio de venta debe ser mayor a cero para %s", label)
		}
	}
	return nil
}

// catalogPriceUpdates construye el mapa de actualización de precios de catálogo para un ítem.
// purchase_price solo se actualiza cuando unit_cost > 0 (bonificaciones o errores no sobrescriben).
func catalogPriceUpdates(item PurchaseItemInput) (map[string]interface{}, error) {
	if item.UpdateSalePrice && item.NewSalePrice <= 0 {
		label := strings.TrimSpace(item.Description)
		if label == "" {
			label = "producto"
		}
		return nil, fmt.Errorf("precio de venta debe ser mayor a cero para %s", label)
	}
	updates := map[string]interface{}{}
	if item.UnitCost > 0 {
		updates["purchase_price"] = money.RoundSunat(item.UnitCost)
	}
	if item.UpdateSalePrice {
		updates["sale_price"] = money.RoundSunat(item.NewSalePrice)
	}
	if len(updates) == 0 {
		return nil, nil
	}
	updates["updated_at"] = time.Now()
	return updates, nil
}

const errProductNotFound = "El producto seleccionado ya no existe o fue eliminado."

// validatePurchaseProducts verifica que cada product_id referenciado exista en el tenant.
func validatePurchaseProducts(db *gorm.DB, items []PurchaseItemInput) error {
	for _, item := range items {
		if item.ProductID == nil {
			continue
		}
		var product database.TenantProduct
		if err := db.First(&product, *item.ProductID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if label := strings.TrimSpace(item.Description); label != "" {
					return fmt.Errorf("%s (%s)", errProductNotFound, label)
				}
				return errors.New(errProductNotFound)
			}
			return err
		}
	}
	return nil
}

func (s *PurchaseService) Create(input CreatePurchaseInput) (*database.TenantPurchase, error) {
	if len(input.Items) == 0 {
		return nil, errors.New("la compra debe tener al menos un ítem")
	}
	if input.BranchID == 0 {
		return nil, errors.New("sucursal es requerida")
	}
	if input.Series == "" || input.Number == "" {
		return nil, errors.New("serie y número del documento son requeridos")
	}
	if err := validatePurchaseItems(input.Items); err != nil {
		return nil, err
	}
	if err := validatePurchaseProducts(s.db, input.Items); err != nil {
		return nil, err
	}

	taxCfg := input.TaxConfig
	if taxCfg.TaxRate == 0 {
		taxCfg = tax.LoadFromDB(s.db)
	}

	var subtotal, taxAmount, total float64
	purchaseItems := make([]database.TenantPurchaseItem, 0, len(input.Items))

	for _, item := range input.Items {
		affType := item.IgvAffectationType
		if affType == "" {
			affType = "10"
		}
		effectiveRate := taxCfg.EffectiveRate(affType)

		itemSub, itemTax, itemTotal := tax.CalcItem(
			item.UnitCost, item.Quantity, 0,
			affType, item.PriceIncludesIgv, taxCfg,
		)

		subtotal += itemSub
		taxAmount += itemTax
		total += itemTotal

		purchaseItems = append(purchaseItems, database.TenantPurchaseItem{
			ProductID:          item.ProductID,
			Code:               item.Code,
			Description:        item.Description,
			Unit:               item.Unit,
			Quantity:           item.Quantity,
			UnitCost:           item.UnitCost,
			TaxRate:            effectiveRate,
			IgvAffectationType: affType,
			PriceIncludesIgv:   item.PriceIncludesIgv,
			Subtotal:           itemSub,
			TaxAmount:          itemTax,
			Total:              itemTotal,
		})
	}

	currency := input.Currency
	if currency == "" {
		currency = "PEN"
	}

	status := input.Status
	if status == "" {
		status = "received"
	}

	// Resolver sesión de caja: se exige para cualquier compra que registre un pago inmediato,
	// sin importar el método (efectivo, Yape, Plin, transferencia, tarjeta, etc.) — igual que ya
	// exige ResolveCashSessionForSale para ventas, y con el mismo criterio de resolución
	// determinística (sesión abierta del propio usuario en la sucursal, GetOpenSession). Antes
	// solo se resolvía/exigía cuando el método era efectivo, y una compra no-efectivo quedaba con
	// cash_session_id NULL sin ninguna forma determinística de saber a qué sesión pertenecía.
	//
	// Si input.PaymentMethod está vacío (compra a crédito/por cobrar, sin pago inmediato) no hay
	// ninguna operación financiera que trazar todavía — no se exige sesión, igual que antes;
	// tampoco se llama a RecordExpensePayment más abajo en ese caso.
	var cashSessionID *uint
	if input.PaymentMethod != "" {
		cbSvcResolve := cashbanksvc.NewCashBankService(s.db)
		resolved, err := cbSvcResolve.ResolveCashSessionForPurchase(
			input.BranchID, input.UserID, nil,
			[]cashbanksvc.PaymentLineInput{{Method: input.PaymentMethod, Amount: total}},
		)
		if err != nil {
			return nil, err
		}
		cashSessionID = resolved
	}

	purchase := &database.TenantPurchase{
		BranchID:         input.BranchID,
		ContactID:        input.ContactID,
		UserID:           input.UserID,
		DocType:          input.DocType,
		Series:           input.Series,
		Number:           input.Number,
		IssueDate:        input.IssueDate,
		DueDate:          input.DueDate,
		Subtotal:         subtotal,
		TaxAmount:        taxAmount,
		Total:            total,
		Currency:         currency,
		PaymentMethod:    input.PaymentMethod,
		Notes:            input.Notes,
		PriceIncludesIgv: input.PriceIncludesIgv,
		Status:           status,
		CashSessionID:    cashSessionID,
	}
	docNumber := input.Series + "-" + input.Number

	return purchase, s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(purchase).Error; err != nil {
			return err
		}
		for i := range purchaseItems {
			purchaseItems[i].PurchaseID = purchase.ID
		}
		if err := tx.Create(&purchaseItems).Error; err != nil {
			return err
		}
		// Egreso por el método de pago: a caja física (efectivo) o a la cuenta bancaria/
		// billetera correspondiente — mismo enrutamiento que ventas, en sentido egreso.
		if input.PaymentMethod != "" {
			cbSvc := cashbanksvc.NewCashBankService(tx)
			purchaseID := purchase.ID
			if err := cbSvc.RecordExpensePayment(tx, input.PaymentMethod, total, cashSessionID, docNumber, "Compra "+docNumber, &purchaseID, input.UserID); err != nil {
				return err
			}
		}
		// Actualizar precios de catálogo, ingresar stock y registrar seriales por cada ítem
		inv := invsvc.NewInventoryService(tx)
		for i, item := range input.Items {
			if item.ProductID == nil {
				continue
			}
			var product database.TenantProduct
			if err := tx.First(&product, *item.ProductID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if label := strings.TrimSpace(item.Description); label != "" {
						return fmt.Errorf("%s (%s)", errProductNotFound, label)
					}
					return errors.New(errProductNotFound)
				}
				return err
			}

			priceUpdates, err := catalogPriceUpdates(item)
			if err != nil {
				return err
			}
			if priceUpdates != nil {
				if err := tx.Model(&product).Updates(priceUpdates).Error; err != nil {
					return err
				}
			}

			if !product.ManageStock {
				continue
			}

			if err := inv.RecordMovementTx(tx, invsvc.MovementInput{
				ProductID:     *item.ProductID,
				BranchID:      input.BranchID,
				Type:          "in",
				Quantity:      item.Quantity,
				UnitCost:      item.UnitCost,
				Reference:     "COMPRA/" + input.Series + "-" + input.Number,
				UserID:        input.UserID,
				OperationCode: "PURCHASE",
			}); err != nil {
				return err
			}

			// Registrar números de serie individuales
			if product.ManageSeries {
				purchaseItemID := purchaseItems[i].ID
				for _, serial := range item.Serials {
					if serial == "" {
						continue
					}
					if err := tx.Create(&database.TenantProductSerial{
						ProductID:      *item.ProductID,
						BranchID:       input.BranchID,
						Serial:         serial,
						Status:         "available",
						PurchaseItemID: &purchaseItemID,
					}).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

type PurchaseListParams struct {
	BranchID  uint
	ContactID uint
	DocType   string
	DateFrom  *time.Time
	DateTo    *time.Time
	Query     string // número/serie de comprobante o texto en proveedor (nombre, razón social, documento)
	Limit     int
	Offset    int
}

func (s *PurchaseService) List(params PurchaseListParams) ([]database.TenantPurchase, int64, error) {
	var purchases []database.TenantPurchase
	q := s.db.Model(&database.TenantPurchase{})
	if params.BranchID > 0 {
		q = q.Where("tenant_purchases.branch_id = ?", params.BranchID)
	}
	if params.ContactID > 0 {
		q = q.Where("tenant_purchases.contact_id = ?", params.ContactID)
	}
	if params.DocType != "" {
		q = q.Where("tenant_purchases.doc_type = ?", params.DocType)
	}
	if trim := strings.TrimSpace(params.Query); trim != "" {
		term := "%" + trim + "%"
		q = q.Joins("LEFT JOIN tenant_contacts ct ON ct.id = tenant_purchases.contact_id").
			Where(`tenant_purchases.number LIKE ? OR tenant_purchases.series LIKE ? OR ct.business_name LIKE ? OR ct.trade_name LIKE ? OR ct.doc_number LIKE ?`,
				term, term, term, term, term)
	}
	if params.DateFrom != nil {
		q = q.Where("tenant_purchases.issue_date >= ?", params.DateFrom)
	}
	if params.DateTo != nil {
		q = q.Where("tenant_purchases.issue_date <= ?", params.DateTo)
	}

	var total int64
	if params.Limit > 0 {
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		q = q.Offset(params.Offset).Limit(params.Limit)
	}

	err := q.Order("tenant_purchases.issue_date DESC, tenant_purchases.id DESC").Find(&purchases).Error
	if err != nil {
		return nil, 0, err
	}
	if params.Limit == 0 {
		total = int64(len(purchases))
	}
	return purchases, total, nil
}

func (s *PurchaseService) GetByID(id uint) (*database.TenantPurchase, error) {
	var p database.TenantPurchase
	err := s.db.First(&p, id).Error
	return &p, err
}

func (s *PurchaseService) GetItems(purchaseID uint) ([]database.TenantPurchaseItem, error) {
	var items []database.TenantPurchaseItem
	err := s.db.Where("purchase_id = ?", purchaseID).Find(&items).Error
	return items, err
}

const StatusCancelled = "cancelled"

// Void anula la compra: revierte stock, registra salida en kardex y elimina seriales vinculados.
func (s *PurchaseService) Void(purchaseID, userID uint) error {
	var p database.TenantPurchase
	if err := s.db.First(&p, purchaseID).Error; err != nil {
		return err
	}
	if p.Status == StatusCancelled {
		return errors.New("la compra ya está anulada")
	}

	items, err := s.GetItems(purchaseID)
	if err != nil {
		return err
	}

	ref := "ANULACION COMPRA/" + p.Series + "-" + p.Number
	docNumber := p.Series + "-" + p.Number

	return s.db.Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(p.PaymentMethod) != "" && p.Total > 0 {
			cbSvc := cashbanksvc.NewCashBankService(tx)
			desc := "Reversión por anulación de compra"

			// Efectivo (Fase 3): revertir el egreso de tenant_cash_movements de esta compra,
			// si la sesión donde se registró sigue abierta — mismo criterio que usa la
			// reversión de ventas (CreateCashReversal no reabre ni reasigna sesión). Revertir
			// en una sesión ya cerrada necesitaría el mismo mecanismo de "devolución
			// pendiente" que ya existe para ventas (sale_cancel_cash.go) — deliberadamente
			// fuera de esta fase; si se da el caso, se avisa en vez de fallar en silencio y
			// dejar la compra anulada sin haber revertido el efectivo.
			var cashMovs []database.TenantCashMovement
			if err := tx.Where("purchase_id = ? AND type = ?", purchaseID, "expense").Find(&cashMovs).Error; err != nil {
				return err
			}
			for _, cm := range cashMovs {
				var sess database.TenantCashSession
				if err := tx.First(&sess, cm.CashSessionID).Error; err != nil {
					return err
				}
				if sess.Status != "open" {
					return fmt.Errorf("no se puede anular: la sesión de caja donde se registró el pago en efectivo de esta compra ya está cerrada (sesión %d)", sess.ID)
				}
				if err := cbSvc.CreateCashReversal(tx, cm, "Anulación compra", ref, desc, userID); err != nil {
					return err
				}
			}

			// Yape/Plin/transferencia/tarjeta: no dependen de ninguna sesión, se revierten
			// siempre (igual que en ventas).
			if err := cbSvc.ReverseBankMovementsByReference(tx, docNumber, "debit", desc, ref, userID); err != nil {
				return err
			}
		}

		inv := invsvc.NewInventoryService(tx)
		for _, item := range items {
			if item.ProductID == nil {
				continue
			}
			var product database.TenantProduct
			if tx.First(&product, *item.ProductID).Error != nil {
				continue
			}

			if product.ManageStock {
				if err := inv.RecordMovementTx(tx, invsvc.MovementInput{
					ProductID:     *item.ProductID,
					BranchID:      p.BranchID,
					Type:          "out",
					Quantity:      item.Quantity,
					UnitCost:      item.UnitCost,
					Reference:     ref,
					UserID:        userID,
					OperationCode: "PURCHASE",
				}); err != nil {
					return err
				}
			}

			if product.ManageSeries {
				tx.Where("purchase_item_id = ?", item.ID).Delete(&database.TenantProductSerial{})
			}
		}

		return tx.Model(&database.TenantPurchase{}).Where("id = ?", purchaseID).Update("status", StatusCancelled).Error
	})
}
