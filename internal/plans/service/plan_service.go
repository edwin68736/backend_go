package service

import (
	"errors"

	"tukifac/pkg/database"
)

type PlanService struct{}

func NewPlanService() *PlanService { return &PlanService{} }

type PlanWithModules struct {
	database.SaasPlan
	Modules []string `json:"modules"`
}

func (s *PlanService) List() ([]PlanWithModules, error) {
	var plans []database.SaasPlan
	if err := database.CentralDB.Order("id asc").Find(&plans).Error; err != nil {
		return nil, err
	}
	result := make([]PlanWithModules, 0, len(plans))
	for _, p := range plans {
		pw := PlanWithModules{SaasPlan: p}
		var pms []database.SaasPlanModule
		database.CentralDB.Where("plan_id = ?", p.ID).Find(&pms)
		for _, pm := range pms {
			pw.Modules = append(pw.Modules, pm.ModuleKey)
		}
		if pw.Modules == nil {
			pw.Modules = []string{}
		}
		result = append(result, pw)
	}
	return result, nil
}

func (s *PlanService) GetByID(id uint) (*PlanWithModules, error) {
	var plan database.SaasPlan
	if err := database.CentralDB.First(&plan, id).Error; err != nil {
		return nil, errors.New("plan no encontrado")
	}
	pw := &PlanWithModules{SaasPlan: plan, Modules: []string{}}
	var pms []database.SaasPlanModule
	database.CentralDB.Where("plan_id = ?", id).Find(&pms)
	for _, pm := range pms {
		pw.Modules = append(pw.Modules, pm.ModuleKey)
	}
	return pw, nil
}

type CreatePlanInput struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Price        float64  `json:"price"`
	BillingCycle string   `json:"billing_cycle"`
	Modules      []string `json:"modules"`
}

func (s *PlanService) Create(input CreatePlanInput) (*database.SaasPlan, error) {
	if input.Name == "" {
		return nil, errors.New("nombre del plan es requerido")
	}
	if input.BillingCycle == "" {
		input.BillingCycle = "monthly"
	}
	plan := &database.SaasPlan{
		Name:         input.Name,
		Description:  input.Description,
		Price:        input.Price,
		BillingCycle: input.BillingCycle,
		Active:       true,
	}
	if err := database.CentralDB.Create(plan).Error; err != nil {
		return nil, err
	}
	s.syncModules(plan.ID, input.Modules)
	return plan, nil
}

func (s *PlanService) Update(id uint, input CreatePlanInput) error {
	var plan database.SaasPlan
	if err := database.CentralDB.First(&plan, id).Error; err != nil {
		return errors.New("plan no encontrado")
	}
	if input.BillingCycle == "" {
		input.BillingCycle = "monthly"
	}
	database.CentralDB.Model(&plan).Updates(map[string]interface{}{
		"name":          input.Name,
		"description":   input.Description,
		"price":         input.Price,
		"billing_cycle": input.BillingCycle,
	})
	s.syncModules(id, input.Modules)
	return nil
}

func (s *PlanService) ToggleActive(id uint) error {
	var plan database.SaasPlan
	if err := database.CentralDB.First(&plan, id).Error; err != nil {
		return errors.New("plan no encontrado")
	}
	return database.CentralDB.Model(&plan).Update("active", !plan.Active).Error
}

func (s *PlanService) Delete(id uint) error {
	var count int64
	database.CentralDB.Model(&database.SaasSubscription{}).Where("plan_id = ?", id).Count(&count)
	if count > 0 {
		return errors.New("no se puede eliminar: el plan tiene suscripciones asociadas")
	}
	database.CentralDB.Where("plan_id = ?", id).Delete(&database.SaasPlanModule{})
	return database.CentralDB.Delete(&database.SaasPlan{}, id).Error
}

func (s *PlanService) syncModules(planID uint, keys []string) {
	database.CentralDB.Where("plan_id = ?", planID).Delete(&database.SaasPlanModule{})
	for _, k := range keys {
		if k != "" {
			database.CentralDB.Create(&database.SaasPlanModule{PlanID: planID, ModuleKey: k})
		}
	}
}

// ListModules devuelve el catálogo global de módulos
func (s *PlanService) ListModules() ([]database.SaasModule, error) {
	modules := make([]database.SaasModule, 0)
	err := database.CentralDB.Order("id asc").Find(&modules).Error
	return modules, err
}
