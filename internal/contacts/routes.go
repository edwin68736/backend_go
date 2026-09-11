package contacts

import (
	"tukifac/internal/contacts/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes registra las rutas de contactos. Antes solo exigían el módulo del plan
// ("contacts"), nunca el rol del usuario — el catálogo ya define contacts.{view,create,edit,delete}
// (ver internal/users/service/role_service.go) y el frontend ya deja marcarlos en Roles, pero no
// tenían ningún efecto aquí. Se agrega RequirePermission para que sí lo tengan.
func RegisterRoutes(api fiber.Router) {
	h := handler.NewContactHandler()
	mod := middleware.RequireModule("contacts")
	view := middleware.RequirePermission("contacts.view")
	create := middleware.RequirePermission("contacts.create")
	edit := middleware.RequirePermission("contacts.edit")
	del := middleware.RequirePermission("contacts.delete")

	api.Get("/contacts/default", mod, view, h.DefaultClientAPI)
	api.Get("/contacts", mod, view, h.SearchAPI)
	api.Get("/contacts/:id", mod, view, h.GetAPI)
	api.Post("/contacts", mod, create, h.CreateAPI)
	// Import masivo antes de /contacts/:id para que «bulk-import» no se lea como un id.
	api.Post("/contacts/bulk-import", mod, create, h.BulkImportAPI)
	api.Put("/contacts/:id", mod, edit, h.UpdateAPI)
	api.Post("/contacts/:id/photo", mod, edit, h.UploadPhotoAPI)
	api.Delete("/contacts/:id", mod, del, h.DeleteAPI)
	api.Patch("/contacts/:id/toggle", mod, edit, h.ToggleAPI)
}
