// Package db abre conexiones MySQL con la configuracion de pool estandar
// que usan los distintos scrapers del repo.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Abrir conecta y hace ping. nombre es solo para el log (nunca el DSN, que
// trae la contraseña).
func Abrir(nombre, dsn string) (*sql.DB, error) {
	log.Printf("[DB] conectando a %q...", nombre)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetConnMaxLifetime(time.Minute)
	conn.SetMaxOpenConns(4)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("no responde: %w", err)
	}
	log.Printf("[DB]   -> conexion OK")
	return conn, nil
}
