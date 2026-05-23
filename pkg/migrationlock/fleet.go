package migrationlock

import (
	"context"
	"database/sql"
	"time"

	"tukifac/pkg/cronlock"
	"tukifac/pkg/database"
	"tukifac/pkg/tenantcache"
)

const (
	redisJobKey    = "migrate-fleet"
	mysqlLockName  = "tukifac_fleet_migrate"
	defaultLease   = time.Hour
)

// TryAcquireFleet adquiere lock global de ejecución fleet (cron).
// Orden: Redis SETNX (multi-nodo) → MySQL GET_LOCK en BD central (single/fallback).
// Si no se adquiere, retorna acquired=false (el caller debe salir en silencio).
func TryAcquireFleet(lease time.Duration) (release func(), acquired bool) {
	if lease <= 0 {
		lease = defaultLease
	}
	if tenantcache.RDB() != nil {
		return cronlock.TryAcquire(redisJobKey, lease)
	}
	return tryMySQLAdvisoryLock(lease)
}

func tryMySQLAdvisoryLock(lease time.Duration) (func(), bool) {
	noop := func() {}
	if database.CentralDB == nil {
		return noop, false
	}
	sqlDB, err := database.CentralDB.DB()
	if err != nil {
		return noop, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return noop, false
	}

	var got sql.NullInt64
	// timeout 0 = no bloquear; el lease lo gestiona RELEASE_LOCK + cierre de conexión al liberar.
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", mysqlLockName).Scan(&got); err != nil || !got.Valid || got.Int64 != 1 {
		_ = conn.Close()
		return noop, false
	}

	release := func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		var released sql.NullInt64
		_ = conn.QueryRowContext(ctx2, "SELECT RELEASE_LOCK(?)", mysqlLockName).Scan(&released)
		_ = conn.Close()
	}

	// La conexión dedicada se mantiene viva hasta release (GET_LOCK es por sesión).
	_ = lease
	return release, true
}
