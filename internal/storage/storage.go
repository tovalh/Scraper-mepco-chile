// Package storage resuelve los IDs del catalogo y graba el periodo.
package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"script_combustibles/internal/config"
	"script_combustibles/internal/diariooficial"
)

type Combustible struct {
	Clave     string
	CodigoSII int
	Subcodigo string
	Base      float64
}

// El idcon_impuesto cambia entre instalaciones, el codigo del SII no.
var Catalogo = []Combustible{
	{Clave: "93", CodigoSII: 35, Subcodigo: "93", Base: diariooficial.BaseGasolina},
	{Clave: "97", CodigoSII: 35, Subcodigo: "97", Base: diariooficial.BaseGasolina},
	{Clave: "95", CodigoSII: 35, Subcodigo: "95", Base: diariooficial.BaseGasolina},
	{Clave: "diesel", CodigoSII: 28, Subcodigo: "", Base: diariooficial.BaseDiesel},
}

type Base struct {
	Nombre string
	user   string
	db     *sql.DB
}

func Abrir(c config.Base) (*Base, error) {
	db, err := sql.Open("mysql", c.DSN)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(time.Minute)
	db.SetMaxOpenConns(4)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("no responde: %w", err)
	}
	return &Base{Nombre: c.Nombre, user: c.User, db: db}, nil
}

func (b *Base) Close() error { return b.db.Close() }

// ResolverIDs exige una sola coincidencia por combustible: si hay cero o varias
// preferimos abortar antes que grabar en el ID equivocado.
func (b *Base) ResolverIDs() (map[string]int, error) {
	ids := make(map[string]int, len(Catalogo))

	for _, c := range Catalogo {
		var (
			rows *sql.Rows
			err  error
		)
		if c.Subcodigo == "" {
			rows, err = b.db.Query(
				`SELECT idcon_impuesto FROM con_impuesto
				  WHERE codigosii = ? AND (subcodigo IS NULL OR subcodigo = '')`, c.CodigoSII)
		} else {
			rows, err = b.db.Query(
				`SELECT idcon_impuesto FROM con_impuesto
				  WHERE codigosii = ? AND subcodigo = ?`, c.CodigoSII, c.Subcodigo)
		}
		if err != nil {
			return nil, fmt.Errorf("catalogo %s: %w", c.Clave, err)
		}

		var enc []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			enc = append(enc, id)
		}
		rows.Close()

		if len(enc) != 1 {
			return nil, fmt.Errorf("%s: codigosii=%d subcodigo=%q calza con %d filas %v",
				c.Clave, c.CodigoSII, c.Subcodigo, len(enc), enc)
		}
		ids[c.Clave] = enc[0]
	}
	return ids, nil
}

// Guardar inserta el periodo, o lo actualiza si los valores cambiaron.
// Devuelve que hizo y el detalle de los cambios.
func (b *Base) Guardar(ids map[string]int, p diariooficial.Periodo) (string, []string, error) {
	desde := p.Desde.Format("2006-01-02")
	hasta := p.Hasta().Format("2006-01-02")

	nuevos := map[string]float64{
		"93": p.V93, "97": p.V97, "95": p.V95(), "diesel": p.Diesel,
	}

	actuales := map[string]float64{}
	for _, c := range Catalogo {
		var v float64
		err := b.db.QueryRow(
			`SELECT factorvariable FROM con_impuestocombustible
			  WHERE idcon_impuesto = ? AND fechadesde = ?`, ids[c.Clave], desde).Scan(&v)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		actuales[c.Clave] = v
	}

	if len(actuales) == 0 {
		if err := b.insertar(ids, desde, hasta, nuevos); err != nil {
			return "", nil, err
		}
		return "insertado", nil, nil
	}

	var cambios []string
	for _, c := range Catalogo {
		viejo, hay := actuales[c.Clave]
		if !hay || viejo != nuevos[c.Clave] {
			cambios = append(cambios, fmt.Sprintf("%s %.4f->%.4f", c.Clave, viejo, nuevos[c.Clave]))
		}
	}
	if len(cambios) == 0 {
		return "sin cambios", nil, nil
	}

	if err := b.actualizar(ids, desde, hasta, nuevos); err != nil {
		return "", nil, err
	}
	return "actualizado", cambios, nil
}

func (b *Base) insertar(ids map[string]int, desde, hasta string, v map[string]float64) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	st, err := tx.Prepare(`INSERT INTO con_impuestocombustible
		(idcon_impuesto, fechadesde, fechahasta, factorfijo, factorvariable,
		 factorlitro, tipofactor, habilitado, userins, fechains, usermod, fechamod)
		VALUES (?,?,?,?,?,1000,'UTM',1,?,NOW(),?,NOW())`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer st.Close()

	for _, c := range Catalogo {
		if _, err := st.Exec(ids[c.Clave], desde, hasta, c.Base, v[c.Clave], b.user, b.user); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (b *Base) actualizar(ids map[string]int, desde, hasta string, v map[string]float64) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	st, err := tx.Prepare(`UPDATE con_impuestocombustible
		   SET fechahasta = ?, factorfijo = ?, factorvariable = ?, usermod = ?, fechamod = NOW()
		 WHERE idcon_impuesto = ? AND fechadesde = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer st.Close()

	for _, c := range Catalogo {
		if _, err := st.Exec(hasta, c.Base, v[c.Clave], b.user, ids[c.Clave], desde); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
