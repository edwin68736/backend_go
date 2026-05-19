package service

import (
	"errors"
	"strings"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"
	cashbanksvc "tukifac/internal/cashbank/service"

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
}

type CreatePurchaseInput struct {
	BranchID       uint
	ContactID      *uint
	UserID         uint
	DocType        string
	Series         string
	Number         string
	IssueDate      time.Time
	DueDate        *time.Time
	Currency       string
	PaymentMethod  string
	Status         string
	Notes          string
	Items          []PurchaseItemInput
	TaxConfig      tax.Config
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
	purchase := &database.TenantPurchase{
		BranchID:      input.BranchID,
		ContactID:     input.ContactID,
		UserID:        input.UserID,
		DocType:       input.DocType,
		Series:        input.Series,
		Number:        input.Number,
		IssueDate:     input.IssueDate,
		DueDate:       input.DueDate,
		Subtotal:      subtotal,
		TaxAmount:     taxAmount,
		Total:         total,
		Currency:      currency,
		PaymentMethod: input.PaymentMethod,
		Notes:         input.Notes,
		Status:        status,
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
		// Descontar de la cuenta asociada al método de pago (egreso)
		if input.PaymentMethod != "" {
			_ = cashbanksvc.NewCashBankService(s.db).RecordPaymentToAccount(tx, input.PaymentMethod, total, false, docNumber, "Compra "+docNumber, input.UserID)
		}
		// Ingresar stock y registrar seriales por cada ítem
		for i, item := range input.Items {
			if item.ProductID == nil {
				continue
			}
			var product database.TenantProduct
			if tx.First(&product, *item.ProductID).Error != nil {
				continue
			}
			if !product.ManageStock {
				continue
			}

			var stock database.TenantProductStock
			tx.Where("product_id = ? AND branch_id = ?", *item.ProductID, input.BranchID).First(&stock)

			newQty := stock.Quantity + item.Quantity
			if stock.ID == 0 {
				tx.Create(&database.TenantProductStock{
					ProductID: *item.ProductID,
					BranchID:  input.BranchID,
					Quantity:  newQty,
				})
			} else {
				tx.Model(&stock).Updates(map[string]interface{}{
					"quantity":   newQty,
					"updated_at": time.Now(),
				})
			}

			// Kardex
			tx.Create(&database.TenantStockMovement{
				ProductID: *item.ProductID,
				BranchID:  input.BranchID,
				Type:      "in",
				Quantity:  item.Quantity,
				UnitCost:  item.UnitCost,
				Balance:   newQty,
				Reference: "COMPRA/" + input.Series + "-" + input.Number,
				UserID:    input.UserID,
				CreatedAt: time.Now(),
			})

			// Registrar números de serie individuales
			if product.ManageSeries {
				purchaseItemID := purchaseItems[i].ID
				for _, serial := range item.Serials {
					if serial == "" {
						continue
					}
					tx.Create(&database.TenantProductSerial{
						ProductID:      *item.ProductID,
						BranchID:       input.BranchID,
						Serial:         serial,
						Status:         "available",
						PurchaseItemID: &purchaseItemID,
					})
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

	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			if item.ProductID == nil {
				continue
			}
			var product database.TenantProduct
			if tx.First(&product, *item.ProductID).Error != nil {
				continue
			}

			if product.ManageStock {
				var stock database.TenantProductStock
				tx.Where("product_id = ? AND branch_id = ?", *item.ProductID, p.BranchID).First(&stock)
				newQty := stock.Quantity - item.Quantity
				if newQty < 0 {
					return errors.New("no se puede anular: stock insuficiente del producto " + item.Description)
				}
				if stock.ID == 0 {
					return errors.New("no se encontró stock para anular del producto " + item.Description)
				}
				if err := tx.Model(&stock).Updates(map[string]interface{}{
					"quantity":   newQty,
					"updated_at": time.Now(),
				}).Error; err != nil {
					return err
				}
				if err := tx.Create(&database.TenantStockMovement{
					ProductID: *item.ProductID,
					BranchID:  p.BranchID,
					Type:      "out",
					Quantity:  item.Quantity,
					UnitCost:  item.UnitCost,
					Balance:   newQty,
					Reference: ref,
					UserID:    userID,
					CreatedAt: time.Now(),
				}).Error; err != nil {
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
