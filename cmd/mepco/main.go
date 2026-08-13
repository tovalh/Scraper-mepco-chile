// Carga el impuesto especifico a los combustibles desde el Diario Oficial.
//
//	go build -o mepco ./cmd/mepco
//	0 9,19 * * 3  /opt/mepco/mepco >> /var/log/mepco.log 2>&1
package main

import (
	"errors"
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

	fecha := flag.String("fecha", "", "dia del Diario Oficial a leer (YYYY-MM-DD), por defecto el miercoles vigente")
	soloLeer := flag.Bool("solo-leer", false, "muestra lo que encuentra sin tocar la base")
	flag.Parse()

	dia := diaVigente(time.Now())
	origen := "miercoles vigente"
	if *fecha != "" {
		d, err := time.Parse("2006-01-02", *fecha)
		if err != nil {
			log.Fatalf("fecha invalida: %v", err)
		}
		dia = d
		origen = "fecha manual"
	}

	log.Printf("=== mepco: inicio (dia=%s %s, solo-leer=%v) ===", dia.Format("2006-01-02"), origen, *soloLeer)

	p, err := diariooficial.Buscar(dia)
	if err != nil {
		if errors.Is(err, diariooficial.ErrSinDecreto) {
			log.Printf("nada que hacer: %v", err)
			log.Printf("=== mepco: fin (nada que hacer) ===")
			return
		}
		log.Fatalf("diario oficial: %v", err)
	}

	log.Printf("Resultado vigencia %s a %s:", p.Desde.Format("2006-01-02"), p.Hasta().Format("2006-01-02"))
	log.Printf("  93     : %.4f", p.V93)
	log.Printf("  95     : %.4f", p.V95())
	log.Printf("  97     : %.4f", p.V97)
	log.Printf("  DIESEL : %.4f", p.Diesel)

	if *soloLeer {
		log.Printf("=== mepco: fin (solo-leer, no se toco la base) ===")
		return
	}

	cfg, err := config.Load("MEPCO")
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

	log.Printf("=== mepco: fin (%d base(s) OK, %d fallo(s)) ===", len(cfg.Bases)-fallos, fallos)
	if fallos > 0 {
		os.Exit(1)
	}
}

// diaVigente retrocede hasta el miercoles mas reciente: el Diario Oficial
// publica el decreto ese dia, y correr el programa cualquier otro dia de la
// semana (cron fuera de horario, gatillo manual) debe encontrar igual el
// decreto de la semana en curso.
func diaVigente(t time.Time) time.Time {
	for t.Weekday() != time.Wednesday {
		t = t.AddDate(0, 0, -1)
	}
	return t
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
