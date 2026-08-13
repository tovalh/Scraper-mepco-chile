// Package config carga la configuracion desde un archivo .env.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Base struct {
	Nombre string
	DSN    string
	User   string
}

type Config struct {
	Bases []Base
}

func Load() (*Config, error) {
	if ruta := buscarEnv(); ruta != "" {
		if err := cargarArchivo(ruta); err != nil {
			return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
		}
	}

	lista := os.Getenv("MEPCO_DBS")
	if strings.TrimSpace(lista) == "" {
		return nil, fmt.Errorf("MEPCO_DBS no esta definida")
	}

	cfg := &Config{}
	for _, nombre := range strings.Split(lista, ",") {
		nombre = strings.TrimSpace(nombre)
		if nombre == "" {
			continue
		}

		// Cada base declarada en MEPCO_DBS trae sus variables con prefijo DB_<NOMBRE>_.
		pref := "DB_" + strings.ToUpper(nombre) + "_"

		dsn := os.Getenv(pref + "DSN")
		if dsn == "" {
			return nil, fmt.Errorf("falta %sDSN para la base %q", pref, nombre)
		}

		user := os.Getenv(pref + "USER")
		if user == "" {
			user = "cron_mepco"
		}

		cfg.Bases = append(cfg.Bases, Base{Nombre: nombre, DSN: dsn, User: user})
	}

	if len(cfg.Bases) == 0 {
		return nil, fmt.Errorf("MEPCO_DBS no declaro ninguna base valida")
	}
	return cfg, nil
}

// El cron corre con un cwd cualquiera, por eso se busca primero junto al binario.
func buscarEnv() string {
	if r := os.Getenv("MEPCO_ENV"); r != "" {
		return r
	}
	if exe, err := os.Executable(); err == nil {
		if r := filepath.Join(filepath.Dir(exe), ".env"); existe(r) {
			return r
		}
	}
	if existe(".env") {
		return ".env"
	}
	return ""
}

func existe(r string) bool {
	st, err := os.Stat(r)
	return err == nil && !st.IsDir()
}

func cargarArchivo(ruta string) error {
	f, err := os.Open(ruta)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for nlinea := 1; sc.Scan(); nlinea++ {
		linea := strings.TrimSpace(sc.Text())
		if linea == "" || strings.HasPrefix(linea, "#") {
			continue
		}
		linea = strings.TrimPrefix(linea, "export ")

		i := strings.IndexByte(linea, '=')
		if i < 0 {
			return fmt.Errorf("linea %d: falta '='", nlinea)
		}

		clave := strings.TrimSpace(linea[:i])
		valor := strings.TrimSpace(linea[i+1:])

		if len(valor) >= 2 {
			if (valor[0] == '"' && valor[len(valor)-1] == '"') ||
				(valor[0] == '\'' && valor[len(valor)-1] == '\'') {
				valor = valor[1 : len(valor)-1]
			}
		}

		// Lo que ya venga en el entorno gana, asi se puede sobreescribir sin tocar el archivo.
		if _, ok := os.LookupEnv(clave); !ok {
			if err := os.Setenv(clave, valor); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
