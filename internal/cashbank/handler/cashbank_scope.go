package handler

import (
	"tukifac/internal/cashbank/service"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"
	"tukifac/pkg/restaurantperm"

	"github.com/gofiber/fiber/v3"
)

// canManageAnyCashSession admin tenant, cashbank.manage o supervisor restaurante (s.m).
func canManageAnyCashSession(c fiber.Ctx) bool {
	if claims, ok := c.Locals("tenant_claims").(*middleware.TenantClaims); ok && claims != nil {
		if claims.RoleName == "Administrador" {
			return true
		}
		for _, p := range claims.Permissions {
			if p == "cashbank.manage" {
				return true
			}
		}
	}
	return middleware.HasRestaurantPerm(c, restaurantperm.SettingsManage)
}

func canAccessCashSession(c fiber.Ctx, sess *database.TenantCashSession) bool {
	if sess == nil {
		return false
	}
	if canManageAnyCashSession(c) {
		return true
	}
	return sess.OpenedBy == userID(c)
}

func filterSessionsForCaller(c fiber.Ctx, items []service.CashSessionListItem) []service.CashSessionListItem {
	if canManageAnyCashSession(c) {
		return items
	}
	uid := userID(c)
	if uid == 0 {
		return nil
	}
	out := make([]service.CashSessionListItem, 0, len(items))
	for _, it := range items {
		if it.OpenedBy == uid {
			out = append(out, it)
		}
	}
	return out
}

func callerUserIDOrZero(c fiber.Ctx) uint {
	if canManageAnyCashSession(c) {
		return 0
	}
	return userID(c)
}
