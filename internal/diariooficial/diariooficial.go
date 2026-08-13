// Package diariooficial obtiene el decreto de componente variable desde el Diario Oficial.
package diariooficial

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
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
func (p Periodo) V95() float64 {
	v, _ := strconv.ParseFloat(fmt.Sprintf("%.4f", (p.V93+p.V97)/2), 64)
	return v
}

var (
	reEdiciones = regexp.MustCompile(`index\.php\?date=(\d{2}-\d{2}-\d{4})&(?:amp;)?edition=([^"'&\s]+)&(?:amp;)?v=(\d+)`)
	rePDF       = regexp.MustCompile(`/publicaciones/\d{4}/\d{2}/\d{2}/[^"'\s]+\.pdf`)
	reCVE       = regexp.MustCompile(`(\d+)\.pdf$`)
	reVigencia  = regexp.MustCompile(`a contar del dia (\d{1,2}) de ([a-z]+) de (\d{4})`)
	reNumero    = regexp.MustCompile(`-?\d+,\d+`)
	reEspacios  = regexp.MustCompile(`\s+`)
)

var meses = map[string]time.Month{
	"enero": time.January, "febrero": time.February, "marzo": time.March,
	"abril": time.April, "mayo": time.May, "junio": time.June,
	"julio": time.July, "agosto": time.August, "septiembre": time.September,
	"octubre": time.October, "noviembre": time.November, "diciembre": time.December,
}

var cliente = &http.Client{Timeout: 45 * time.Second}

// Buscar revisa las ediciones del dia y devuelve el decreto de la ultima que lo traiga.
func Buscar(dia time.Time) (Periodo, error) {
	fecha := dia.Format("02-01-2006")
	portada := fmt.Sprintf("%s/edicionelectronica/index.php?date=%s", baseURL, fecha)

	html, err := bajar(portada)
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

	log.Printf("Diario Oficial %s: %d edicion(es) %v", fecha, len(orden), orden)

	var ultimo *Periodo
	for _, ed := range orden {
		p, ok, err := buscarEnEdicion(ediciones[ed])
		if err != nil {
			return Periodo{}, err
		}
		if !ok {
			log.Printf("  edicion %s: sin decreto de componente variable", ed)
			continue
		}
		p.Edicion = ed
		log.Printf("  edicion %s: CVE-%s, vigencia %s", ed, p.CVE, p.Desde.Format("2006-01-02"))
		ultimo = &p
	}

	if ultimo == nil {
		return Periodo{}, fmt.Errorf("%s: %w", fecha, ErrSinDecreto)
	}
	return *ultimo, nil
}

func buscarEnEdicion(url string) (Periodo, bool, error) {
	html, err := bajar(url)
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

	datos, err := bajar(baseURL + ruta)
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

	if p.V93, err = leer(txt, "gasolina automotriz de 93 octanos", BaseGasolina); err != nil {
		return p, err
	}
	if p.V97, err = leer(txt, "gasolina automotriz de 97 octanos", BaseGasolina); err != nil {
		return p, err
	}
	if p.Diesel, err = leer(txt, "petroleo diesel", BaseDiesel); err != nil {
		return p, err
	}
	return p, nil
}

// leer usa la segunda tabla del decreto, donde cada combustible viene como
// "base variable". Si la base no es la esperada, se leyo mal.
func leer(txt, marcador string, baseEsperada float64) (float64, error) {
	i := strings.LastIndex(txt, marcador)
	if i < 0 {
		return 0, fmt.Errorf("no encontre %q", marcador)
	}

	crudos := reNumero.FindAllString(txt[i:], 2)
	if len(crudos) < 2 {
		return 0, fmt.Errorf("%q: esperaba 2 numeros, encontre %d", marcador, len(crudos))
	}

	base, err := strconv.ParseFloat(strings.ReplaceAll(crudos[0], ",", "."), 64)
	if err != nil {
		return 0, err
	}
	if base != baseEsperada {
		return 0, fmt.Errorf("%q: base %.4f, esperaba %.2f", marcador, base, baseEsperada)
	}
	return strconv.ParseFloat(strings.ReplaceAll(crudos[1], ",", "."), 64)
}

var acentos = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
)

func normalizar(s string) string {
	s = acentos.Replace(strings.ToLower(s))
	return strings.TrimSpace(reEspacios.ReplaceAllString(s, " "))
}

func bajar(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mepco-cron/1.0)")

	var ultErr error
	for intento := 1; intento <= 3; intento++ {
		if intento > 1 {
			time.Sleep(time.Duration(intento) * 3 * time.Second)
		}

		resp, err := cliente.Do(req)
		if err != nil {
			ultErr = err
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			ultErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			ultErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		return b, nil
	}
	return nil, fmt.Errorf("no se pudo bajar %s: %w", url, ultErr)
}
