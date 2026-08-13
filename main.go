// Carga el impuesto especifico a los combustibles desde el Diario Oficial.
//
//	go build -o mepco .
//	0 9,19 * * 3  /opt/mepco/mepco >> /var/log/mepco.log 2>&1
package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"script_combustibles/internal/config"
	"script_combustibles/internal/diariooficial"
	"script_combustibles/internal/storage"
)

func main() {
	log.SetFlags(log.LstdFlags)

	fecha := flag.String("fecha", "", "dia del Diario Oficial a leer (YYYY-MM-DD), por defecto hoy")
	soloLeer := flag.Bool("solo-leer", false, "muestra lo que encuentra sin tocar la base")
	flag.Parse()

	dia := time.Now()
	if *fecha != "" {
		d, err := time.Parse("2006-01-02", *fecha)
		if err != nil {
			log.Fatalf("fecha invalida: %v", err)
		}
		dia = d
	}

	p, err := diariooficial.Buscar(dia)
	if err != nil {
		log.Fatalf("diario oficial: %v", err)
	}

	log.Printf("vigencia %s a %s | 93=%.4f 97=%.4f 95=%.4f diesel=%.4f",
		p.Desde.Format("2006-01-02"), p.Hasta().Format("2006-01-02"),
		p.V93, p.V97, p.V95(), p.Diesel)

	if *soloLeer {
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	fallos := 0
	for _, bc := range cfg.Bases {
		if err := guardar(bc, p); err != nil {
			log.Printf("[%s] ERROR: %v", bc.Nombre, err)
			fallos++
		}
	}

	if fallos > 0 {
		os.Exit(1)
	}
}

func guardar(bc config.Base, p diariooficial.Periodo) error {
	b, err := storage.Abrir(bc)
	if err != nil {
		return err
	}
	defer b.Close()

	ids, err := b.ResolverIDs()
	if err != nil {
		return err
	}

	accion, cambios, err := b.Guardar(ids, p)
	if err != nil {
		return err
	}

	if len(cambios) > 0 {
		log.Printf("[%s] %s: %s", bc.Nombre, accion, strings.Join(cambios, ", "))
	} else {
		log.Printf("[%s] %s", bc.Nombre, accion)
	}
	return nil
}
