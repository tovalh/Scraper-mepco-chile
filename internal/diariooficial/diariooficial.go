// Package diariooficial obtiene el decreto de componente variable desde el Diario Oficial.
package diariooficial

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	"script_combustibles/internal/descarga"
)

// ErrSinDecreto indica que ninguna edicion del dia trae el decreto: no es un
// fallo de scraping, simplemente ese dia no hay nada publicado (no era
// miercoles, o el Diario Oficial todavia no lo sube).
var ErrSinDecreto = errors.New("ninguna edicion trae el decreto de componente variable")

const baseURL = "https://www.diariooficial.interior.gob.cl"

// Componente base fijado por la ley 18.502.
const (
	BaseGasolina = 6.0
	BaseDiesel   = 1.5
)

type Periodo struct {
	Desde   time.Time
	V93     float64
	V97     float64
	Diesel  float64
	CVE     string
	Edicion string
}

// La vigencia corre de jueves a miercoles.
func (p Periodo) Hasta() time.Time { return p.Desde.AddDate(0, 0, 6) }

// La 95 no viene en el decreto, se calcula como el promedio de 93 y 97.
func (p Periodo) V95() float64 { return redondear4((p.V93 + p.V97) / 2) }

// Solo la 95 se redondea, porque es el unico valor calculado: 4 decimales,
// igual que el decreto y que la columna de la base.
func redondear4(v float64) float64 { return math.Round(v*10000) / 10000 }

var (
	reEdiciones = regexp.MustCompile(`index\.php\?date=(\d{2}-\d{2}-\d{4})&(?:amp;)?edition=([^"'&\s]+)&(?:amp;)?v=(\d+)`)
	rePDF       = regexp.MustCompile(`/publicaciones/\d{4}/\d{2}/\d{2}/[^"'\s]+\.pdf`)
	reCVE       = regexp.MustCompile(`(\d+)\.pdf$`)
	reVigencia  = regexp.MustCompile(`a contar del dia (\d{1,2}) de ([a-z]+) de (\d{4})`)
	reNumero    = regexp.MustCompile(`-?\d+,\d{4}`)
	reEspacios  = regexp.MustCompile(`\s+`)
)

var meses = map[string]time.Month{
	"enero": time.January, "febrero": time.February, "marzo": time.March,
	"abril": time.April, "mayo": time.May, "junio": time.June,
	"julio": time.July, "agosto": time.August, "septiembre": time.September,
	"octubre": time.October, "noviembre": time.November, "diciembre": time.December,
}

// Buscar revisa las ediciones del dia y devuelve el decreto de la ultima que lo traiga.
func Buscar(dia time.Time) (Periodo, error) {
	fecha := dia.Format("02-01-2006")
	portada := fmt.Sprintf("%s/edicionelectronica/index.php?date=%s", baseURL, fecha)

	html, err := descarga.Bajar(portada)
	if err != nil {
		return Periodo{}, err
	}

	ediciones := map[string]string{}
	orden := []string{}
	for _, m := range reEdiciones.FindAllStringSubmatch(string(html), -1) {
		if _, ok := ediciones[m[2]]; !ok {
			orden = append(orden, m[2])
			ediciones[m[2]] = fmt.Sprintf("%s/edicionelectronica/index.php?date=%s&edition=%s&v=%s",
				baseURL, m[1], m[2], m[3])
		}
	}
	if len(orden) == 0 {
		orden = append(orden, "unica")
		ediciones["unica"] = portada
	}

	log.Printf("[DIARIO] %d edicion(es) encontradas: %v", len(orden), orden)

	var ultimo *Periodo
	for _, ed := range orden {
		log.Printf("[DIARIO] edicion %s: revisando...", ed)
		p, ok, err := buscarEnEdicion(ed, ediciones[ed])
		if err != nil {
			return Periodo{}, err
		}
		if !ok {
			log.Printf("[DIARIO] edicion %s: no trae \"componente variable\", se omite", ed)
			continue
		}
		p.Edicion = ed
		log.Printf("[DIARIO] edicion %s: CVE-%s, vigencia %s -> OK", ed, p.CVE, p.Desde.Format("2006-01-02"))
		ultimo = &p
	}

	if ultimo == nil {
		return Periodo{}, fmt.Errorf("%s: %w", fecha, ErrSinDecreto)
	}
	log.Printf("[DIARIO] usando la ultima edicion con decreto: %s (CVE-%s)", ultimo.Edicion, ultimo.CVE)
	return *ultimo, nil
}

func buscarEnEdicion(ed, url string) (Periodo, bool, error) {
	html, err := descarga.Bajar(url)
	if err != nil {
		return Periodo{}, false, err
	}

	i := strings.Index(strings.ToLower(string(html)), "componente variable")
	if i < 0 {
		return Periodo{}, false, nil
	}
	ruta := rePDF.FindString(string(html)[i:])
	if ruta == "" {
		return Periodo{}, false, nil
	}
	log.Printf("[DIARIO] edicion %s: contiene \"componente variable\"", ed)

	datos, err := descarga.Bajar(baseURL + ruta)
	if err != nil {
		return Periodo{}, false, err
	}

	p, err := parsear(datos)
	if err != nil {
		return Periodo{}, false, fmt.Errorf("%s: %w", ruta, err)
	}
	if m := reCVE.FindStringSubmatch(ruta); m != nil {
		p.CVE = m[1]
	}
	return p, true, nil
}

func parsear(datos []byte) (Periodo, error) {
	var p Periodo

	r, err := pdf.NewReader(bytes.NewReader(datos), int64(len(datos)))
	if err != nil {
		return p, fmt.Errorf("abriendo pdf: %w", err)
	}
	buf, err := r.GetPlainText()
	if err != nil {
		return p, fmt.Errorf("extrayendo texto: %w", err)
	}
	crudo, err := io.ReadAll(buf)
	if err != nil {
		return p, err
	}

	txt := normalizar(string(crudo))

	m := reVigencia.FindStringSubmatch(txt)
	if m == nil {
		return p, fmt.Errorf("no encontre la fecha de vigencia")
	}
	mes, ok := meses[m[2]]
	if !ok {
		return p, fmt.Errorf("mes desconocido %q", m[2])
	}
	dia, _ := strconv.Atoi(m[1])
	anio, _ := strconv.Atoi(m[3])
	p.Desde = time.Date(anio, mes, dia, 0, 0, 0, 0, time.UTC)
	log.Printf("[DIARIO] PDF: vigencia \"a contar del dia %s de %s de %s\"", m[1], m[2], m[3])

	if p.V93, err = leer(txt, "gasolina automotriz de 93 octanos", BaseGasolina); err != nil {
		return p, err
	}
	log.Printf("[DIARIO] 93 octanos: base=%.4f (ok, ley 18.502) variable=%.4f", BaseGasolina, p.V93)

	if p.V97, err = leer(txt, "gasolina automotriz de 97 octanos", BaseGasolina); err != nil {
		return p, err
	}
	log.Printf("[DIARIO] 97 octanos: base=%.4f (ok, ley 18.502) variable=%.4f", BaseGasolina, p.V97)

	if p.Diesel, err = leer(txt, "petroleo diesel", BaseDiesel); err != nil {
		return p, err
	}
	log.Printf("[DIARIO] diesel: base=%.4f (ok, ley 18.502) variable=%.4f", BaseDiesel, p.Diesel)

	log.Printf("[DIARIO] 95 octanos: no viene en el decreto, se calcula como promedio de 93/97 = %.4f", p.V95())
	return p, nil
}

// leer usa la segunda tabla del decreto, donde cada combustible viene como
// "base variable total". El texto del PDF llega sin separadores entre columnas
// ("6,0000-0,69175,3083"), por eso los numeros se toman con 4 decimales exactos
// y se valida que base + variable = total: si la fila se leyo mal, no cuadra.
func leer(txt, marcador string, baseEsperada float64) (float64, error) {
	i := strings.LastIndex(txt, marcador)
	if i < 0 {
		return 0, fmt.Errorf("no encontre %q", marcador)
	}

	crudos := reNumero.FindAllString(txt[i:], 3)
	if len(crudos) < 3 {
		return 0, fmt.Errorf("%q: esperaba 3 numeros, encontre %d %v", marcador, len(crudos), crudos)
	}

	var n [3]float64
	for k, crudo := range crudos {
		v, err := strconv.ParseFloat(strings.ReplaceAll(crudo, ",", "."), 64)
		if err != nil {
			return 0, fmt.Errorf("%q: numero %q: %w", marcador, crudo, err)
		}
		n[k] = v
	}
	base, variable, total := n[0], n[1], n[2]

	if base != baseEsperada {
		return 0, fmt.Errorf("%q: base %.4f, esperaba %.2f", marcador, base, baseEsperada)
	}
	if math.Abs(base+variable-total) > 0.00001 {
		return 0, fmt.Errorf("%q: %.4f + %.4f no da %.4f, la fila se leyo mal", marcador, base, variable, total)
	}
	return variable, nil
}

var acentos = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
)

func normalizar(s string) string {
	s = acentos.Replace(strings.ToLower(s))
	return strings.TrimSpace(reEspacios.ReplaceAllString(s, " "))
}
