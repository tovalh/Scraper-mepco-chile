// Package sii obtiene UF, UTM, UTA e IPC desde las paginas de valores y
// fechas del Servicio de Impuestos Internos.
package sii

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"script_combustibles/internal/descarga"
)

type Valor struct {
	Indicador string
	Desde     time.Time
	Hasta     time.Time
	Valor     float64
}

const (
	ufURL  = "https://www.sii.cl/valores_y_fechas/uf/uf%d.htm"
	utmURL = "https://www.sii.cl/valores_y_fechas/utm/utm%d.htm"
)

var mesesColumna = []time.Month{
	time.January, time.February, time.March, time.April, time.May, time.June,
	time.July, time.August, time.September, time.October, time.November, time.December,
}

var mesesNombre = map[string]time.Month{
	"Enero": time.January, "Febrero": time.February, "Marzo": time.March,
	"Abril": time.April, "Mayo": time.May, "Junio": time.June,
	"Julio": time.July, "Agosto": time.August, "Septiembre": time.September,
	"Octubre": time.October, "Noviembre": time.November, "Diciembre": time.December,
}

// columnasUTM: posicion (0-based, despues del nombre del mes) de cada
// indicador en la fila de la tabla UTM-UTA-IPC.
var columnasUTM = []struct {
	Indicador string
	Col       int
}{
	{"UTM", 0},
	{"UTA", 1},
	{"IPC", 2},
}

var (
	reTablaExport = regexp.MustCompile(`(?s)<table[^>]*id=['"]table_export['"][^>]*>.*?</table>`)
	reFila        = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	reCelda       = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	reEspacios    = regexp.MustCompile(`\s+`)
)

// BuscarUF lee la tabla resumen dia x mes de uf{anio}.htm. Los dias sin
// publicar (celda vacia) se omiten, no son un error.
func BuscarUF(anio int) ([]Valor, error) {
	html, err := descarga.Bajar(fmt.Sprintf(ufURL, anio))
	if err != nil {
		return nil, err
	}
	tabla, err := extraerTabla(string(html))
	if err != nil {
		return nil, fmt.Errorf("uf %d: %w", anio, err)
	}
	log.Printf("[SII] tabla table_export encontrada")

	var valores []Valor
	vacias := 0
	for _, fila := range filas(tabla) {
		cols := celdas(fila)
		if len(cols) != 1+len(mesesColumna) {
			continue
		}
		dia, err := strconv.Atoi(limpiar(cols[0]))
		if err != nil {
			continue // fila de encabezado, no un dia
		}
		for i, mes := range mesesColumna {
			v, ok := numero(cols[i+1])
			if !ok {
				vacias++
				continue
			}
			fecha := time.Date(anio, mes, dia, 0, 0, 0, 0, time.UTC)
			valores = append(valores, Valor{Indicador: "UF", Desde: fecha, Hasta: fecha, Valor: v})
		}
	}
	if len(valores) == 0 {
		return nil, fmt.Errorf("uf %d: no encontre valores en la tabla", anio)
	}
	ordenar(valores)
	log.Printf("[SII] UF: %d valores parseados (%s a %s), %d celdas vacias omitidas",
		len(valores), valores[0].Desde.Format("2006-01-02"), valores[len(valores)-1].Desde.Format("2006-01-02"), vacias)
	return valores, nil
}

// BuscarUTM lee la tabla mensual de utm{anio}.htm y devuelve UTM, UTA e IPC
// de cada mes ya publicado.
func BuscarUTM(anio int) ([]Valor, error) {
	html, err := descarga.Bajar(fmt.Sprintf(utmURL, anio))
	if err != nil {
		return nil, err
	}
	tabla, err := extraerTabla(string(html))
	if err != nil {
		return nil, fmt.Errorf("utm %d: %w", anio, err)
	}
	log.Printf("[SII] tabla table_export encontrada")

	type rango struct {
		n        int
		min, max time.Time
	}
	resumen := map[string]*rango{}

	var valores []Valor
	for _, fila := range filas(tabla) {
		cols := celdas(fila)
		if len(cols) < 4 {
			continue
		}
		mes, ok := mesesNombre[limpiar(cols[0])]
		if !ok {
			continue // fila de encabezado, no un mes
		}
		desde := time.Date(anio, mes, 1, 0, 0, 0, 0, time.UTC)
		hasta := finDeMes(desde)

		for _, c := range columnasUTM {
			v, ok := numero(cols[1+c.Col])
			if !ok {
				continue
			}
			valores = append(valores, Valor{Indicador: c.Indicador, Desde: desde, Hasta: hasta, Valor: v})

			r := resumen[c.Indicador]
			if r == nil {
				r = &rango{min: desde}
				resumen[c.Indicador] = r
			}
			r.n++
			r.max = desde
		}
	}
	if len(valores) == 0 {
		return nil, fmt.Errorf("utm %d: no encontre valores en la tabla", anio)
	}
	ordenar(valores)
	for _, ind := range []string{"UTM", "UTA", "IPC"} {
		r := resumen[ind]
		if r == nil {
			continue
		}
		log.Printf("[SII] %s: %d valores (%s a %s)", ind, r.n, r.min.Format("2006-01"), r.max.Format("2006-01"))
	}
	return valores, nil
}

// ordenar deja los valores por fecha: la extraccion de la tabla UF los arma
// en orden dia-de-mes (columna por columna), no cronologico.
func ordenar(valores []Valor) {
	sort.Slice(valores, func(i, j int) bool { return valores[i].Desde.Before(valores[j].Desde) })
}

func finDeMes(primerDia time.Time) time.Time {
	return primerDia.AddDate(0, 1, -1)
}

func extraerTabla(html string) (string, error) {
	m := reTablaExport.FindString(html)
	if m == "" {
		return "", fmt.Errorf("no encontre la tabla table_export")
	}
	return m, nil
}

func filas(tabla string) []string {
	var out []string
	for _, m := range reFila.FindAllStringSubmatch(tabla, -1) {
		out = append(out, m[1])
	}
	return out
}

func celdas(fila string) []string {
	var out []string
	for _, m := range reCelda.FindAllStringSubmatch(fila, -1) {
		out = append(out, m[1])
	}
	return out
}

func limpiar(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", "")
	return strings.TrimSpace(reEspacios.ReplaceAllString(s, " "))
}

// numero interpreta el formato numerico chileno (punto de miles, coma
// decimal). Celda vacia o texto no numerico -> false, no es un error: son
// las columnas de meses que todavia no se publican.
func numero(s string) (float64, bool) {
	s = limpiar(s)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
