package health

import (
	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/metrics"

	"github.com/gofiber/fiber/v3"
)

// FleetHealthPayload respuesta estándar de salud del fleet.
type FleetHealthPayload struct {
	Pending       int64  `json:"pending"`
	Running       int64  `json:"running"`
	Failed        int64  `json:"failed"`
	Blocked       int64  `json:"blocked"`
	CurrentTarget int    `json:"current_target"`
	CircuitOpen   bool   `json:"circuit_open"`
	CircuitReason string `json:"circuit_reason,omitempty"`
	Critical      bool   `json:"critical,omitempty"`
	Outdated      int64  `json:"outdated,omitempty"`
}

func buildFleetHealthPayload() (*FleetHealthPayload, error) {
	sum, err := database.FleetMigrationSummaryQuery()
	if err != nil {
		return nil, err
	}
	pending := sum.Pending + sum.Outdated
	if sum.CircuitOpen {
		pending = 0
	}
	payload := &FleetHealthPayload{
		Pending:       pending,
		Running:       sum.Running,
		Failed:        sum.Failed,
		Blocked:       sum.Blocked,
		CurrentTarget: sum.SchemaTargetVersion,
		CircuitOpen:   sum.CircuitOpen,
		CircuitReason: sum.CircuitReason,
		Outdated:      sum.Outdated,
	}
	threshold := 25
	if cfg := config.AppConfig; cfg != nil && cfg.FleetFailedThreshold > 0 {
		threshold = cfg.FleetFailedThreshold
	}
	payload.Critical = sum.Failed > int64(threshold) || sum.CircuitOpen
	metrics.FleetPending.Store(pending)
	metrics.FleetFailed.Store(sum.Failed)
	return payload, nil
}

func authorizeFleetHealth(c fiber.Ctx) bool {
	cfg := config.AppConfig
	if cfg == nil || cfg.InternalAPIKey == "" {
		return true
	}
	return c.Get("X-Internal-Key") == cfg.InternalAPIKey
}

// FleetHealth GET /fleet-health y /api/internal/fleet-health
func FleetHealth(c fiber.Ctx) error {
	if !authorizeFleetHealth(c) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	payload, err := buildFleetHealthPayload()
	if err != nil {
		return c.Status(503).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(payload)
}
