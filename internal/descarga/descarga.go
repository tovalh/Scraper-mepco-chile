// Package descarga baja URLs por HTTP con reintentos, compartido por los
// distintos scrapers del repo.
package descarga

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var cliente = &http.Client{Timeout: 45 * time.Second}

// Bajar reintenta hasta 3 veces con backoff antes de darse por vencido.
func Bajar(url string) ([]byte, error) {
	log.Printf("[HTTP] GET %s", url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mepco-cron/1.0)")

	var ultErr error
	for intento := 1; intento <= 3; intento++ {
		if intento > 1 {
			log.Printf("[HTTP]   reintento %d/3...", intento)
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
		log.Printf("[HTTP]   -> 200 OK (%d bytes)", len(b))
		return b, nil
	}
	return nil, fmt.Errorf("no se pudo bajar %s: %w", url, ultErr)
}
