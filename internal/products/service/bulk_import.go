package service

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

const (
	BulkImportMaxItems    = 2000
	BulkImportBatchPause  = 15 * time.Millisecond // pausa ligera entre lotes para no saturar MySQL
	BulkImportBatchSize   = 50
)

// BulkImportItem fila normalizada para importación masiva.
type BulkImportItem struct {
	RowNumber          int     `json:"row_number"`
	Name               string  `json:"name"`
	Code               string  `json:"code"`
	Description        string  `json:"description"`
	SalePrice          float64 `json:"sale_price"`
	Unit               string  `json:"unit"`
	CategoryName       string  `json:"category_name"`
	IgvAffectationType string  `json:"igv_affectation_type"`
	PriceIncludesIgv   bool    `json:"price_includes_igv"`
	ManageStock        bool    `json:"manage_stock"`
	InitialStock       float64 `json:"initial_stock"`
	IsRestaurant       bool    `json:"is_restaurant"`
	PreparationArea    string  `json:"preparation_area"`
	CatalogType        string  `json:"type"` // product | service (solo catálogo tenant)
}

type BulkImportFail struct {
	Row   int    `json:"row"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

type BulkImportResult struct {
	Created         int              `json:"created"`
	StockRegistered int              `json:"stock_registered"`
	Failed          []BulkImportFail `json:"failed"`
}

type bulkImportRunOpts struct {
	ForceRestaurant bool
	BranchID        uint
	UserID          uint
	StockNotes      string
}

// BulkImportRestaurant importa platos (is_restaurant=true, con área de preparación).
func (s *ProductService) BulkImportRestaurant(items []BulkImportItem, branchID, userID uint) (*BulkImportResult, error) {
	for i := range items {
		items[i].IsRestaurant = true
	}
	return s.bulkImport(items, bulkImportRunOpts{
		ForceRestaurant: true,
		BranchID:        branchID,
		UserID:          userID,
		StockNotes:      "Stock inicial — importación masiva restaurante",
	})
}

// BulkImportCatalog importa productos/servicios del panel tenant (is_restaurant según fila).
func (s *ProductService) BulkImportCatalog(items []BulkImportItem, branchID, userID uint) (*BulkImportResult, error) {
	return s.bulkImport(items, bulkImportRunOpts{
		ForceRestaurant: false,
		BranchID:        branchID,
		UserID:          userID,
		StockNotes:      "Stock inicial — importación masiva catálogo",
	})
}

func (s *ProductService) bulkImport(items []BulkImportItem, opts bulkImportRunOpts) (*BulkImportResult, error) {
	if len(items) == 0 {
		return nil, errors.New("no hay filas para importar")
	}
	if len(items) > BulkImportMaxItems {
		return nil, fmt.Errorf("máximo %d filas por solicitud", BulkImportMaxItems)
	}
	needsBranch := false
	for _, item := range items {
		if item.InitialStock > 0 {
			needsBranch = true
			break
		}
	}
	if needsBranch && opts.BranchID == 0 {
		return nil, errors.New("sucursal activa requerida cuando hay stock_inicial")
	}

	taxCfg := tax.LoadFromDB(s.db)
	result := &BulkImportResult{Failed: make([]BulkImportFail, 0)}
	catCache := make(map[string]uint)
	codesInDB := make(map[string]struct{})
	codesInFile := make(map[string]int)

	for _, item := range items {
		code := strings.TrimSpace(item.Code)
		if code != "" {
			if prev, ok := codesInFile[code]; ok {
				result.Failed = append(result.Failed, BulkImportFail{
					Row: item.RowNumber, Name: item.Name,
					Error: fmt.Sprintf("código duplicado en fila %d", prev),
				})
				continue
			}
			codesInFile[code] = item.RowNumber
		}
	}

	for i := 0; i < len(items); i++ {
		if BulkImportBatchPause > 0 && i > 0 && i%BulkImportBatchSize == 0 {
			time.Sleep(BulkImportBatchPause)
		}
		item := items[i]
		if item.Name == "" {
			result.Failed = append(result.Failed, BulkImportFail{
				Row: item.RowNumber, Name: item.Name, Error: "nombre requerido",
			})
			continue
		}
		if item.SalePrice <= 0 {
			result.Failed = append(result.Failed, BulkImportFail{
				Row: item.RowNumber, Name: item.Name, Error: "precio_venta debe ser mayor a 0",
			})
			continue
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			code = generateImportEAN13(codesInDB, codesInFile)
		}
		if _, dup := codesInDB[code]; dup {
			result.Failed = append(result.Failed, BulkImportFail{
				Row: item.RowNumber, Name: item.Name,
				Error: fmt.Sprintf("el código %q ya existe", code),
			})
			continue
		}

		manageStock := item.ManageStock
		if item.InitialStock > 0 {
			manageStock = true
		}
		isRestaurant := item.IsRestaurant
		if opts.ForceRestaurant {
			isRestaurant = true
		}
		prepArea := strings.TrimSpace(strings.ToLower(item.PreparationArea))
		if !isRestaurant {
			prepArea = ""
		}

		igvType := strings.TrimSpace(item.IgvAffectationType)
		if igvType == "" {
			igvType = "10"
		}

		var catID *uint
		if cn := strings.TrimSpace(item.CategoryName); cn != "" {
			id, err := s.resolveCategoryIDByName(cn, catCache)
			if err != nil {
				result.Failed = append(result.Failed, BulkImportFail{
					Row: item.RowNumber, Name: item.Name, Error: err.Error(),
				})
				continue
			}
			if id > 0 {
				catID = &id
			}
		}

		unit := strings.TrimSpace(item.Unit)
		if unit == "" {
			unit = "NIU"
		}
		catalogType := strings.TrimSpace(strings.ToLower(item.CatalogType))
		if catalogType == "" {
			catalogType = "product"
		}

		input := ProductInput{
			CategoryID:         catID,
			Code:               code,
			Name:               strings.TrimSpace(item.Name),
			Description:        strings.TrimSpace(item.Description),
			Type:               catalogType,
			Unit:               unit,
			SalePrice:          item.SalePrice,
			IgvAffectationType: igvType,
			PriceIncludesIgv:   item.PriceIncludesIgv,
			ManageStock:        manageStock,
			IsRestaurant:       isRestaurant,
			PreparationArea:    prepArea,
			Active:             true,
			TaxRate:            taxCfg.EffectiveRate(igvType),
		}

		var createdID uint
		err := s.db.Transaction(func(tx *gorm.DB) error {
			ps := NewProductService(tx)
			p, err := ps.Create(input)
			if err != nil {
				return err
			}
			createdID = p.ID
			inv := invsvc.NewInventoryService(tx)
			if item.InitialStock > 0 && p.ManageStock && opts.BranchID > 0 {
				return inv.RecordInitialStock(
					p.ID, opts.BranchID, item.InitialStock, opts.UserID, opts.StockNotes,
				)
			}
			if opts.ForceRestaurant && opts.BranchID > 0 {
				return inv.EnsureProductBranchLink(p.ID, opts.BranchID)
			}
			return nil
		})
		if err != nil {
			result.Failed = append(result.Failed, BulkImportFail{
				Row: item.RowNumber, Name: item.Name, Error: err.Error(),
			})
			continue
		}

		codesInDB[code] = struct{}{}
		result.Created++
		if item.InitialStock > 0 && manageStock {
			result.StockRegistered++
		}
		_ = createdID
	}

	return result, nil
}

func (s *ProductService) resolveCategoryIDByName(name string, cache map[string]uint) (uint, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return 0, nil
	}
	if id, ok := cache[key]; ok {
		return id, nil
	}
	var cat database.TenantCategory
	err := s.db.Where("LOWER(name) = ?", key).First(&cat).Error
	if err == nil {
		cache[key] = cat.ID
		return cat.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	created, err := s.CreateCategory(strings.TrimSpace(name), "")
	if err != nil {
		return 0, fmt.Errorf("crear categoría %q: %w", name, err)
	}
	cache[key] = created.ID
	return created.ID, nil
}

func generateImportEAN13(usedDB map[string]struct{}, usedFile map[string]int) string {
	for attempt := 0; attempt < 32; attempt++ {
		raw := fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.Intn(1_000_000))
		base12 := raw
		if len(base12) > 12 {
			base12 = base12[len(base12)-12:]
		}
		for len(base12) < 12 {
			base12 = "0" + base12
		}
		sum := 0
		for i := 0; i < 12; i++ {
			d := int(base12[i] - '0')
			if i%2 == 0 {
				sum += d
			} else {
				sum += 3 * d
			}
		}
		check := (10 - (sum % 10)) % 10
		code := base12 + fmt.Sprintf("%d", check)
		if _, inFile := usedFile[code]; inFile {
			continue
		}
		if _, inDB := usedDB[code]; inDB {
			continue
		}
		return code
	}
	return fmt.Sprintf("%012d", time.Now().UnixNano()%1_000_000_000_000)
}
