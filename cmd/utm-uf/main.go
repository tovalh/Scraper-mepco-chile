// Carga UF, UTM, UTA e IPC desde las paginas de valores y fechas del SII.
//
//	go build -o utm-uf ./cmd/utm-uf
//	0 13 * * *  /opt/mepco/utm-uf >> /var/log/utm-uf.log 2>&1
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"script_combustibles/internal/config"
	"script_combustibles/internal/indicadores"
	"script_combustibles/internal/sii"
)

func main() {
	log.SetFlags(log.LstdFlags)

	anio := flag.Int("anio", time.Now().Year(), "año de las paginas del SII a leer")
	soloLeer := flag.Bool("solo-leer", false, "muestra lo que encuentra sin tocar la base")
	flag.Parse()

	log.Printf("=== utm-uf: inicio (anio=%d, solo-leer=%v) ===", *anio, *soloLeer)

	var valores []sii.Valor
	fallosScraping := 0

	uf, err := sii.BuscarUF(*anio)
	if err != nil {
		log.Printf("UF: ERROR: %v", err)
		fallosScraping++
	} else {
		valores = append(valores, uf...)
	}

	utm, err := sii.BuscarUTM(*anio)
	if err != nil {
		log.Printf("UTM/UTA/IPC: ERROR: %v", err)
		fallosScraping++
	} else {
		valores = append(valores, utm...)
	}

	if len(valores) == 0 {
		log.Printf("=== utm-uf: fin (sin valores) ===")
		log.Fatalf("no se obtuvo ningun valor")
	}

	log.Printf("Resultado: %d valores a procesar", len(valores))

	if *soloLeer {
		mostrarResumen(valores)
		log.Printf("=== utm-uf: fin (solo-leer, no se toco la base) ===")
		if fallosScraping > 0 {
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load("UTMUF")
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	fallos := fallosScraping
	for _, bc := range cfg.Bases {
		if err := guardar(bc, valores); err != nil {
			log.Printf("[%s] ERROR: %v", bc.Nombre, err)
			fallos++
		}
	}

	log.Printf("=== utm-uf: fin (%d base(s) OK, %d fallo(s)) ===", len(cfg.Bases)-fallos, fallos)
	if fallos > 0 {
		os.Exit(1)
	}
}

func guardar(bc config.Base, valores []sii.Valor) error {
	b, err := indicadores.Abrir(bc)
	if err != nil {
		return err
	}
	defer b.Close()

	type contador struct {
		procesados, insertados, actualizados, sinCambios int
	}

	ids := map[string]int{}
	totales := map[string]*contador{}
	fallos := 0

	for _, v := range valores {
		id, ok := ids[v.Indicador]
		if !ok {
			id, err = b.ResolverID(v.Indicador)
			if err != nil {
				return err
			}
			ids[v.Indicador] = id
		}

		c := totales[v.Indicador]
		if c == nil {
			c = &contador{}
			totales[v.Indicador] = c
		}
		c.procesados++

		accion, err := b.Guardar(id, v)
		if err != nil {
			log.Printf("[%s] %s %s: ERROR: %v", bc.Nombre, v.Indicador, v.Desde.Format("2006-01-02"), err)
			fallos++
			continue
		}

		switch accion {
		case "sin cambios":
			c.sinCambios++
		case "insertado":
			c.insertados++
			log.Printf("[%s] %s %s: insertado (%.4f)", bc.Nombre, v.Indicador, v.Desde.Format("2006-01-02"), v.Valor)
		default: // "actualizado X->Y"
			c.actualizados++
			log.Printf("[%s] %s %s: %s", bc.Nombre, v.Indicador, v.Desde.Format("2006-01-02"), accion)
		}
	}

	for _, ind := range []string{"UF", "UTM", "UTA", "IPC"} {
		c := totales[ind]
		if c == nil {
			continue
		}
		log.Printf("[%s] %s: %d procesados -> %d insertados, %d actualizados, %d sin cambios",
			bc.Nombre, ind, c.procesados, c.insertados, c.actualizados, c.sinCambios)
	}

	if fallos > 0 {
		return fmt.Errorf("%d valores fallaron", fallos)
	}
	return nil
}

// mostrarResumen imprime, por indicador, cuantos valores se encontraron y
// el mas reciente (sii.Buscar* ya los deja ordenados por fecha).
func mostrarResumen(valores []sii.Valor) {
	type resumen struct {
		n      int
		ultimo sii.Valor
	}
	porIndicador := map[string]*resumen{}
	for _, v := range valores {
		r, ok := porIndicador[v.Indicador]
		if !ok {
			r = &resumen{}
			porIndicador[v.Indicador] = r
		}
		r.n++
		r.ultimo = v // ya viene ordenado por fecha, el ultimo visto es el mas reciente
	}
	for _, ind := range []string{"UF", "UTM", "UTA", "IPC"} {
		r := porIndicador[ind]
		if r == nil {
			continue
		}
		log.Printf("%-4s %3d valores, mas reciente %s -> %.4f", ind, r.n, r.ultimo.Desde.Format("2006-01-02"), r.ultimo.Valor)
	}
}
