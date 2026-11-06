package service

import (
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupUserServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantUser{}, &database.TenantRole{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// seedOwnerAndStaff: el primer usuario creado (menor ID) es el "owner" — el que se crea
// automáticamente al aprovisionar el tenant (ver database.TenantOwnerUserID). El segundo es un
// empleado cualquiera, para contrastar que a ÉL sí se lo puede desactivar.
func seedOwnerAndStaff(t *testing.T, db *gorm.DB) (ownerID, staffID uint) {
	t.Helper()
	owner := database.TenantUser{Name: "Dueño", Email: "owner@t.com", RoleID: 1, Active: true}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	staff := database.TenantUser{Name: "Empleado", Email: "staff@t.com", RoleID: 1, Active: true}
	if err := db.Create(&staff).Error; err != nil {
		t.Fatal(err)
	}
	return owner.ID, staff.ID
}

// El caso reportado: desde la pantalla de editar usuario, desmarcar "Activo" y guardar
// desactivaba al usuario principal sin ninguna restricción.
func TestUpdate_NoPuedeDesactivarAlOwner(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	ownerID, _ := seedOwnerAndStaff(t, db)

	err := svc.Update(ownerID, UpdateUserInput{
		Name: "Dueño", Email: "owner@t.com", RoleID: 1, Active: false,
	})
	if err == nil {
		t.Fatal("esperaba error al intentar desactivar al usuario principal")
	}

	var reloaded database.TenantUser
	if err := db.First(&reloaded, ownerID).Error; err != nil {
		t.Fatal(err)
	}
	if !reloaded.Active {
		t.Fatal("el owner quedó inactivo pese al rechazo — el error no debe aplicar cambios parciales")
	}
}

// Editar los demás datos del owner (nombre, email, teléfono) sigue permitido — el pedido fue
// "no debe permitir desactivar, solamente editar".
func TestUpdate_PuedeEditarAlOwnerManteniendoloActivo(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	ownerID, _ := seedOwnerAndStaff(t, db)

	err := svc.Update(ownerID, UpdateUserInput{
		Name: "Dueño Actualizado", Email: "owner@t.com", Phone: "999888777", RoleID: 1, Active: true,
	})
	if err != nil {
		t.Fatalf("editar al owner sin tocar Active debe permitirse: %v", err)
	}

	var reloaded database.TenantUser
	if err := db.First(&reloaded, ownerID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Name != "Dueño Actualizado" || !reloaded.Active {
		t.Errorf("edición no aplicada correctamente: %+v", reloaded)
	}
}

// Un empleado normal (no owner) sí puede desactivarse — la restricción es exclusiva del principal.
func TestUpdate_PuedeDesactivarAUnEmpleadoNormal(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	_, staffID := seedOwnerAndStaff(t, db)

	if err := svc.Update(staffID, UpdateUserInput{
		Name: "Empleado", Email: "staff@t.com", RoleID: 1, Active: false,
	}); err != nil {
		t.Fatalf("desactivar a un empleado normal debe permitirse: %v", err)
	}

	var reloaded database.TenantUser
	if err := db.First(&reloaded, staffID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Active {
		t.Fatal("el empleado debía quedar inactivo")
	}
}

// Mismo caso que Update, pero por el botón directo de activar/desactivar (PATCH /toggle) — es un
// camino de código separado que se saltaba la misma validación.
func TestToggleActive_NoPuedeDesactivarAlOwner(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	ownerID, _ := seedOwnerAndStaff(t, db)

	if err := svc.ToggleActive(ownerID); err == nil {
		t.Fatal("esperaba error al intentar desactivar al owner vía toggle")
	}

	var reloaded database.TenantUser
	if err := db.First(&reloaded, ownerID).Error; err != nil {
		t.Fatal(err)
	}
	if !reloaded.Active {
		t.Fatal("el owner quedó inactivo pese al rechazo del toggle")
	}
}

func TestToggleActive_PuedeDesactivarAUnEmpleadoNormal(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	_, staffID := seedOwnerAndStaff(t, db)

	if err := svc.ToggleActive(staffID); err != nil {
		t.Fatalf("desactivar a un empleado vía toggle debe permitirse: %v", err)
	}
	var reloaded database.TenantUser
	db.First(&reloaded, staffID)
	if reloaded.Active {
		t.Fatal("el empleado debía quedar inactivo tras el toggle")
	}
}

// Reactivar al owner nunca debe bloquearse — la regla es "no puede quedar inactivo", no "no se
// puede tocar su estado". Si por algún motivo estuviera inactivo, debe poder reactivarse.
func TestToggleActive_ReactivarAlOwnerSiempreSePermite(t *testing.T) {
	db := setupUserServiceTestDB(t)
	ownerID, _ := seedOwnerAndStaff(t, db)
	// Lo dejamos inactivo directo en BD (bypass del service), simulando un estado heredado.
	if err := db.Model(&database.TenantUser{}).Where("id = ?", ownerID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewUserService(db)
	if err := svc.ToggleActive(ownerID); err != nil {
		t.Fatalf("reactivar al owner debe permitirse siempre: %v", err)
	}
	var reloaded database.TenantUser
	db.First(&reloaded, ownerID)
	if !reloaded.Active {
		t.Fatal("el owner debía quedar activo tras reactivarlo")
	}
}

func seedAdminRoleAndUsers(t *testing.T, db *gorm.DB) (ownerID, adminID uint) {
	t.Helper()
	role := database.TenantRole{Name: "Administrador", IsSystem: true}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	owner := database.TenantUser{Name: "Dueño", Email: "o@t.com", RoleID: role.ID, Active: true}
	admin := database.TenantUser{Name: "Admin 2", Email: "a@t.com", RoleID: role.ID, Active: true}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	return owner.ID, admin.ID
}

// El caso del incidente: el botón de eliminar borraba al usuario principal sin validación.
func TestDelete_NoPuedeEliminarAlOwner(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	ownerID, _ := seedAdminRoleAndUsers(t, db)

	if err := svc.Delete(ownerID); err == nil {
		t.Fatal("esperaba error al eliminar al usuario principal")
	}
	var reloaded database.TenantUser
	if err := db.First(&reloaded, ownerID).Error; err != nil {
		t.Fatalf("el owner no debía eliminarse: %v", err)
	}
}

// Un segundo administrador sí puede eliminarse mientras quede otro administrador activo.
func TestDelete_PuedeEliminarAdminSiQuedaOtroActivo(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	_, adminID := seedAdminRoleAndUsers(t, db)

	if err := svc.Delete(adminID); err != nil {
		t.Fatalf("eliminar un admin con otro admin activo debe permitirse: %v", err)
	}
	if err := db.First(&database.TenantUser{}, adminID).Error; err == nil {
		t.Fatal("el admin debía quedar eliminado")
	}
}

// Si el owner está inactivo (estado heredado), el único admin activo no puede borrarse.
func TestDelete_NoPuedeEliminarAlUltimoAdminActivo(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	ownerID, adminID := seedAdminRoleAndUsers(t, db)
	if err := db.Model(&database.TenantUser{}).Where("id = ?", ownerID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(adminID); err == nil {
		t.Fatal("esperaba error al eliminar al último administrador activo")
	}
	if err := db.First(&database.TenantUser{}, adminID).Error; err != nil {
		t.Fatalf("el admin no debía eliminarse: %v", err)
	}
}

// Un usuario sin rol de administrador se elimina sin restricciones.
func TestDelete_PuedeEliminarUsuarioNormal(t *testing.T) {
	db := setupUserServiceTestDB(t)
	svc := NewUserService(db)
	seedAdminRoleAndUsers(t, db)
	seller := database.TenantRole{Name: "Vendedor", IsSystem: true}
	db.Create(&seller)
	u := database.TenantUser{Name: "V", Email: "v@t.com", RoleID: seller.ID, Active: true}
	db.Create(&u)

	if err := svc.Delete(u.ID); err != nil {
		t.Fatalf("eliminar un vendedor debe permitirse: %v", err)
	}
}
