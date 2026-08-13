// Package indicadores guarda valores de indicadores economicos (UF, UTM,
// UTA, IPC) en gen_indicadoreconomico / gen_indicadoreconomicovalor.
package indicadores

import (
	"database/sql"
	"fmt"
	"log"

	"script_combustibles/internal/config"
	"script_combustibles/internal/db"
	"script_combustibles/internal/sii"
)

type Base struct {
	Nombre string
	user   string
	db     *sql.DB
}

func Abrir(c config.Base) (*Base, error) {
	conn, err := db.Abrir(c.Nombre, c.DSN)
	if err != nil {
		return nil, err
	}
	return &Base{Nombre: c.Nombre, user: c.User, db: conn}, nil
}

func (b *Base) Close() error { return b.db.Close() }

// ResolverID busca el id del indicador por su abreviatura (UF, UTM, UTA, IPC).
// El catalogo gen_indicadoreconomico ya trae estas filas cargadas.
func (b *Base) ResolverID(abreviatura string) (int, error) {
	var id int
	err := b.db.QueryRow(
		`SELECT idgen_indicadoreconomico FROM gen_indicadoreconomico WHERE abreviatura = ?`,
		abreviatura).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no existe el indicador %q en gen_indicadoreconomico", abreviatura)
	}
	if err != nil {
		return 0, err
	}
	log.Printf("[DB] %-6s -> idgen_indicadoreconomico=%d", abreviatura, id)
	return id, nil
}

// Guardar inserta el valor, o lo actualiza si cambio. Devuelve que hizo.
func (b *Base) Guardar(idIndicador int, v sii.Valor) (string, error) {
	desde := v.Desde.Format("2006-01-02")
	hasta := v.Hasta.Format("2006-01-02")

	var actual float64
	err := b.db.QueryRow(
		`SELECT valor FROM gen_indicadoreconomicovalor
		  WHERE idgen_indicadoreconomico = ? AND fechadesde = ?`, idIndicador, desde).Scan(&actual)

	switch {
	case err == sql.ErrNoRows:
		if err := b.insertar(idIndicador, desde, hasta, v.Valor); err != nil {
			return "", err
		}
		return "insertado", nil
	case err != nil:
		return "", err
	case actual == v.Valor:
		return "sin cambios", nil
	default:
		if err := b.actualizar(idIndicador, desde, hasta, v.Valor); err != nil {
			return "", err
		}
		return fmt.Sprintf("actualizado %.4f->%.4f", actual, v.Valor), nil
	}
}

func (b *Base) insertar(idIndicador int, desde, hasta string, valor float64) error {
	_, err := b.db.Exec(`INSERT INTO gen_indicadoreconomicovalor
		(idgen_indicadoreconomico, fechadesde, fechahasta, valor, habilitado, userins, fechains, usermod, fechamod)
		VALUES (?,?,?,?,1,?,NOW(),?,NOW())`,
		idIndicador, desde, hasta, valor, b.user, b.user)
	return err
}

func (b *Base) actualizar(idIndicador int, desde, hasta string, valor float64) error {
	_, err := b.db.Exec(`UPDATE gen_indicadoreconomicovalor
		   SET fechahasta = ?, valor = ?, usermod = ?, fechamod = NOW()
		 WHERE idgen_indicadoreconomico = ? AND fechadesde = ?`,
		hasta, valor, b.user, idIndicador, desde)
	return err
}
