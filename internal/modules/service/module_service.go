package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

type ModuleService struct {
	db *gorm.DB
}

func NewModuleService(db *gorm.DB) *ModuleService {
	return &ModuleService{db: db}
}

func (s *ModuleService) List() ([]database.TenantExternalModule, error) {
	var modules []database.TenantExternalModule
	err := s.db.Order("name ASC").Find(&modules).Error
	return modules, err
}

func (s *ModuleService) GetByKey(key string) (*database.TenantExternalModule, error) {
	var mod database.TenantExternalModule
	err := s.db.Where("module_key = ? AND enabled = ?", key, true).First(&mod).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &mod, err
}

func (s *ModuleService) Register(key, name, baseURL, apiKey string) (*database.TenantExternalModule, error) {
	if key == "" || name == "" || baseURL == "" {
		return nil, errors.New("clave, nombre y URL base son requeridos")
	}
	var existing database.TenantExternalModule
	if err := s.db.Where("module_key = ?", key).First(&existing).Error; err == nil {
		// Actualizar
		s.db.Model(&existing).Updates(map[string]interface{}{
			"name":     name,
			"base_url": baseURL,
			"api_key":  apiKey,
			"enabled":  true,
		})
		return &existing, nil
	}
	mod := &database.TenantExternalModule{
		ModuleKey: key,
		Name:      name,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Enabled:   true,
	}
	err := s.db.Create(mod).Error
	return mod, err
}

func (s *ModuleService) SetEnabled(key string, enabled bool) error {
	return s.db.Model(&database.TenantExternalModule{}).
		Where("module_key = ?", key).
		Update("enabled", enabled).Error
}

// Forward envía una solicitud a un módulo externo.
func (s *ModuleService) Forward(moduleKey, path, method string, body []byte, headers map[string]string) ([]byte, int, error) {
	mod, err := s.GetByKey(moduleKey)
	if err != nil {
		return nil, 0, err
	}
	if mod == nil {
		return nil, 404, errors.New("módulo no encontrado o deshabilitado")
	}

	url := mod.BaseURL + path
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("error creando request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if mod.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+mod.APIKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error conectando al módulo: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// Ping verifica la conectividad con un módulo externo.
func (s *ModuleService) Ping(moduleKey string) error {
	mod, err := s.GetByKey(moduleKey)
	if err != nil {
		return err
	}
	if mod == nil {
		return errors.New("módulo no encontrado")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", mod.BaseURL+"/health", nil)
	if mod.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+mod.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sin respuesta del módulo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("módulo respondió con código %d", resp.StatusCode)
	}
	return nil
}

// GetConfig retorna la configuración JSON de un módulo.
func (s *ModuleService) GetConfig(moduleKey string) (map[string]interface{}, error) {
	mod, err := s.GetByKey(moduleKey)
	if err != nil || mod == nil {
		return nil, errors.New("módulo no encontrado")
	}
	var cfg map[string]interface{}
	if mod.ConfigJSON != nil && *mod.ConfigJSON != "" {
		json.Unmarshal([]byte(*mod.ConfigJSON), &cfg)
	}
	return cfg, nil
}
