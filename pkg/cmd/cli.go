package cmd

import (
	"fmt"
	"os"
)

// Execute despacha subcomandos CLI. Retorna código de salida.
func Execute(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	if err := InitDatabase(); err != nil {
		fmt.Fprintf(os.Stderr, "database connection failed: %v\n", err)
		return 1
	}

	switch args[0] {
	case "migrate":
		return RunMigrate()
	case "migrate-central":
		return RunMigrateCentral()
	case "migrate-tenants":
		return RunMigrateTenants()
	case "migrate-tenant":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: tukifac-api migrate-tenant <slug>")
			return 1
		}
		return RunMigrateTenant(args[1])
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`Tukifac API — comandos disponibles:

  serve                 Inicia el servidor HTTP (por defecto sin argumentos)
  migrate               Migra BD central + todos los tenants activos (por lotes)
  migrate-central       Solo BD central
  migrate-tenants       Solo tenants activos
  migrate-tenant <slug> Un tenant por slug (debug)

Ejemplo deploy:
  docker exec tukifac-backend-go ./tukifac-api migrate`)
}
