package saasdocuments

import (
	"tukifac/internal/saasdocuments/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(saAPI fiber.Router) {
	h := handler.NewHandler()
	g := saAPI.Group("/document-packages")
	g.Get("/", h.ListCatalogAPI)
	g.Post("/", h.UpsertCatalogAPI)
	g.Put("/:id", h.UpsertCatalogAPI)
	g.Delete("/:id", h.DeleteCatalogAPI)
	g.Get("/purchases/pending", h.ListPendingAPI)
	g.Patch("/purchases/:id/approve", h.ApproveAPI)
	g.Patch("/purchases/:id/reject", h.RejectAPI)
}
