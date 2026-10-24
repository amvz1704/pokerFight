package campeonato

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

//go:embed panel.html
var paginaPanel []byte

// maxCuerpo acota lo que acepta el panel en un pedido. Una configuración de
// campeonato pesa unos pocos KB.
const maxCuerpo = 1 << 20

// Panel es la interfaz de control del campeonato: una API HTTP y una página web
// que la usa. Con ella el organizador define cuántas fases, mesas y jugadores
// por mesa, ve cómo quedaría el torneo antes de empezar, lo lanza y lo sigue en
// vivo.
//
// Controla el torneo entero, así que todo /api exige la clave de administración
// en el encabezado X-Clave. La página en sí es estática y no lleva datos: sin la
// clave no muestra nada. Por defecto cmd/campeonato la sirve solo en
// 127.0.0.1.
type Panel struct {
	c     *Campeonato
	clave string
	ctx   context.Context
}

// NuevoPanel arma el panel. El contexto tiene que vivir lo que dure el servidor:
// es el que rige las rondas que se lanzan desde acá, que siguen corriendo
// después de que termina el pedido que las inició.
func NuevoPanel(ctx context.Context, c *Campeonato, clave string) *Panel {
	return &Panel{c: c, clave: clave, ctx: ctx}
}

// NuevaClave genera una clave de administración aleatoria.
func NuevaClave() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("campeonato: no se pudo generar la clave de administración: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Handler devuelve el http.Handler del panel.
func (p *Panel) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", p.pagina)
	mux.Handle("GET /api/estado", p.protegido(p.estado))
	mux.Handle("POST /api/plan", p.protegido(p.plan))
	mux.Handle("PUT /api/config", p.protegido(p.guardarConfig))
	mux.Handle("POST /api/ronda", p.protegido(p.iniciar(false)))
	mux.Handle("POST /api/auto", p.protegido(p.iniciar(true)))
	mux.Handle("POST /api/pausar", p.protegido(p.pausar))
	mux.Handle("POST /api/cancelar", p.protegido(p.cancelar))
	return mux
}

func (p *Panel) pagina(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// La página no necesita nada de afuera: se prohíbe cargar cualquier cosa que
	// no sea de este origen, y que se la embeba en otro sitio.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(paginaPanel)
}

// protegido exige la clave de administración. La compara en tiempo constante.
func (p *Panel) protegido(siguiente http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recibida := r.Header.Get("X-Clave")
		if p.clave == "" || subtle.ConstantTimeCompare([]byte(recibida), []byte(p.clave)) != 1 {
			responderError(w, http.StatusUnauthorized, errors.New("clave de administración incorrecta"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCuerpo)
		w.Header().Set("Cache-Control", "no-store")
		siguiente(w, r)
	})
}

func (p *Panel) estado(w http.ResponseWriter, _ *http.Request) {
	responder(w, http.StatusOK, p.c.Instantanea())
}

// plan calcula las fases de una configuración sin guardarla. Es lo que usa la
// página para mostrar la vista previa mientras se edita el formulario.
func (p *Panel) plan(w http.ResponseWriter, r *http.Request) {
	var cfg Config
	if err := leerJSON(r, &cfg); err != nil {
		responderError(w, http.StatusBadRequest, err)
		return
	}
	jugadores := len(p.c.inscritos)
	if v := r.URL.Query().Get("jugadores"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100000 {
			responderError(w, http.StatusBadRequest, errors.New("jugadores tiene que ser un número entre 0 y 100000"))
			return
		}
		jugadores = n
	}
	planes, err := Plan(cfg, jugadores)
	if err != nil {
		responderError(w, http.StatusBadRequest, err)
		return
	}
	responder(w, http.StatusOK, map[string]any{"plan": planes, "jugadores": jugadores})
}

func (p *Panel) guardarConfig(w http.ResponseWriter, r *http.Request) {
	var cfg Config
	if err := leerJSON(r, &cfg); err != nil {
		responderError(w, http.StatusBadRequest, err)
		return
	}
	if err := p.c.ActualizarConfig(cfg); err != nil {
		// Una configuración inválida es culpa del pedido; una válida que llega
		// tarde es un conflicto con el estado del campeonato.
		estado := http.StatusBadRequest
		if errors.Is(err, ErrYaEmpezo) {
			estado = http.StatusConflict
		}
		responderError(w, estado, err)
		return
	}
	responder(w, http.StatusOK, p.c.Instantanea())
}

func (p *Panel) iniciar(auto bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := p.c.Iniciar(p.ctx, auto); err != nil {
			responderError(w, http.StatusConflict, err)
			return
		}
		responder(w, http.StatusAccepted, map[string]bool{"ok": true})
	}
}

func (p *Panel) pausar(w http.ResponseWriter, _ *http.Request) {
	p.c.Pausar()
	responder(w, http.StatusOK, map[string]bool{"ok": true})
}

func (p *Panel) cancelar(w http.ResponseWriter, _ *http.Request) {
	p.c.Cancelar()
	responder(w, http.StatusOK, map[string]bool{"ok": true})
}

// leerJSON decodifica el cuerpo rechazando campos desconocidos, por la misma
// razón que la configuración en disco: un nombre mal escrito se ignoraría y se
// guardaría otra configuración que la que el organizador cree.
func leerJSON(r *http.Request, destino any) error {
	decodificador := json.NewDecoder(r.Body)
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(destino); err != nil {
		return errors.New("el cuerpo no es una configuración válida: " + err.Error())
	}
	return nil
}

func responder(w http.ResponseWriter, estado int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	json.NewEncoder(w).Encode(v)
}

func responderError(w http.ResponseWriter, estado int, err error) {
	responder(w, estado, map[string]string{"error": strings.TrimSpace(err.Error())})
}
