package campeonato

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

const claveDePrueba = "clave-de-prueba"

// panelDePrueba levanta el panel sobre un campeonato real, con un servidor HTTP
// de verdad.
func panelDePrueba(t *testing.T, cfg Config, ins Inscritos) (*Campeonato, *httptest.Server) {
	t.Helper()
	c := campeonatoDePrueba(t, cfg, ins)
	ctx, cancelar := context.WithCancel(context.Background())
	servidor := httptest.NewServer(NuevoPanel(ctx, c, claveDePrueba).Handler())
	t.Cleanup(func() {
		cancelar()
		servidor.Close()
	})
	return c, servidor
}

func pedir(t *testing.T, servidor *httptest.Server, metodo, ruta, clave string, cuerpo any) (int, []byte) {
	t.Helper()
	var lector io.Reader
	switch v := cuerpo.(type) {
	case nil:
	case []byte:
		lector = bytes.NewReader(v)
	default:
		datos, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		lector = bytes.NewReader(datos)
	}
	req, err := http.NewRequest(metodo, servidor.URL+ruta, lector)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if clave != "" {
		req.Header.Set("X-Clave", clave)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, ruta, err)
	}
	defer resp.Body.Close()
	datos, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, datos
}

func TestLaPaginaSeSirveSinClaveYSinDatos(t *testing.T) {
	_, servidor := panelDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))

	estado, cuerpo := pedir(t, servidor, "GET", "/", "", nil)
	if estado != http.StatusOK || !strings.Contains(string(cuerpo), "Panel del campeonato") {
		t.Fatalf("GET / dio %d; se esperaba la pagina", estado)
	}
	// La pagina es estatica: ni un token ni un nombre de equipo.
	for _, secreto := range []string{"pfb-", "Equipo 1"} {
		if strings.Contains(string(cuerpo), secreto) {
			t.Errorf("la pagina sin clave contiene %q", secreto)
		}
	}
}

func TestLaAPIExigeLaClaveDeAdministracion(t *testing.T) {
	_, servidor := panelDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))

	rutas := []struct{ metodo, ruta string }{
		{"GET", "/api/estado"}, {"POST", "/api/plan"}, {"PUT", "/api/config"},
		{"POST", "/api/ronda"}, {"POST", "/api/auto"}, {"POST", "/api/pausar"}, {"POST", "/api/cancelar"},
	}
	for _, r := range rutas {
		for _, clave := range []string{"", "incorrecta", claveDePrueba + "x", strings.ToUpper(claveDePrueba)} {
			if estado, _ := pedir(t, servidor, r.metodo, r.ruta, clave, nil); estado != http.StatusUnauthorized {
				t.Errorf("%s %s con clave %q dio %d, se esperaba 401", r.metodo, r.ruta, clave, estado)
			}
		}
	}

	// La clave en la URL no autoriza: iria a los logs y al historial del
	// navegador. Solo vale por el encabezado.
	if estado, _ := pedir(t, servidor, "GET", "/api/estado?clave="+claveDePrueba, "", nil); estado != http.StatusUnauthorized {
		t.Errorf("la clave por query string dio %d, se esperaba 401", estado)
	}
}

// TestUnaAPISinClaveConfiguradaNoAbreLaPuerta: una clave vacia no puede
// significar "sin clave".
func TestUnaAPISinClaveConfiguradaNoAbreLaPuerta(t *testing.T) {
	c := campeonatoDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))
	servidor := httptest.NewServer(NuevoPanel(context.Background(), c, "").Handler())
	defer servidor.Close()

	if estado, _ := pedir(t, servidor, "GET", "/api/estado", "", nil); estado != http.StatusUnauthorized {
		t.Fatalf("con la clave vacia, GET /api/estado dio %d; se esperaba 401", estado)
	}
}

func TestElEstadoTraeLoQueNecesitaLaPagina(t *testing.T) {
	_, servidor := panelDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 6))

	estado, cuerpo := pedir(t, servidor, "GET", "/api/estado", claveDePrueba, nil)
	if estado != http.StatusOK {
		t.Fatalf("GET /api/estado dio %d: %s", estado, cuerpo)
	}
	var snap Instantanea
	if err := json.Unmarshal(cuerpo, &snap); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if snap.Etapa != EtapaInscripcion || len(snap.Jugadores) != 6 || len(snap.Plan) != 3 || snap.Inscritos != 6 {
		t.Errorf("etapa %s, %d jugadores, %d fases en el plan, %d inscritos; se esperaba inscripcion, 6, 3 y 6",
			snap.Etapa, len(snap.Jugadores), len(snap.Plan), snap.Inscritos)
	}
	for _, j := range snap.Jugadores {
		if j.Equipo == "" || j.Bot == "" || j.Alias == "" {
			t.Errorf("fila incompleta en la tabla de jugadores: %+v", j)
		}
	}
}

func TestLaVistaPreviaDelPlanSeCalculaSinGuardar(t *testing.T) {
	c, servidor := panelDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))
	antes := c.Config()

	cfg := ConfigBases()
	estado, cuerpo := pedir(t, servidor, "POST", "/api/plan?jugadores=64", claveDePrueba, cfg)
	if estado != http.StatusOK {
		t.Fatalf("POST /api/plan dio %d: %s", estado, cuerpo)
	}
	var r struct {
		Plan      []FasePlan `json:"plan"`
		Jugadores int        `json:"jugadores"`
	}
	if err := json.Unmarshal(cuerpo, &r); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if r.Jugadores != 64 || len(r.Plan) != 4 || r.Plan[0].Salen != 32 || r.Plan[3].Salen != 1 {
		t.Errorf("plan: %d jugadores, %d fases, salen %d y %d; se esperaba 64, 4, 32 y 1",
			r.Jugadores, len(r.Plan), r.Plan[0].Salen, r.Plan[len(r.Plan)-1].Salen)
	}

	// Mirar un plan no cambia la configuracion del campeonato.
	if got := c.Config(); got.Nombre != antes.Nombre || len(got.Fases) != len(antes.Fases) {
		t.Error("la vista previa modifico la configuracion del campeonato")
	}
}

func TestLaAPIRechazaPedidosMalFormados(t *testing.T) {
	_, servidor := panelDePrueba(t, configDePrueba(t), inscritosDePrueba(t, 4))

	casos := []struct {
		nombre, metodo, ruta string
		cuerpo               any
		esperado             int
	}{
		{"jugadores no numerico", "POST", "/api/plan?jugadores=muchos", ConfigBases(), 400},
		{"jugadores negativo", "POST", "/api/plan?jugadores=-3", ConfigBases(), 400},
		{"cuerpo que no es JSON", "POST", "/api/plan", []byte("{no es json"), 400},
		{"campo desconocido", "POST", "/api/plan", []byte(`{"nombre":"x","fases_extra":1}`), 400},
		{"configuracion invalida", "POST", "/api/plan", Config{Nombre: "x"}, 400},
		{"guardar una configuracion invalida", "PUT", "/api/config", Config{Nombre: "x"}, 400},
		{"cuerpo gigante", "POST", "/api/plan", bytes.Repeat([]byte("a"), 2<<20), 400},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			estado, cuerpo := pedir(t, servidor, caso.metodo, caso.ruta, claveDePrueba, caso.cuerpo)
			if estado != caso.esperado {
				t.Fatalf("dio %d (%s), se esperaba %d", estado, cuerpo, caso.esperado)
			}
			var r map[string]string
			if err := json.Unmarshal(cuerpo, &r); err != nil || r["error"] == "" {
				t.Errorf("la respuesta de error no trae un mensaje: %s", cuerpo)
			}
		})
	}
}

// TestSeJuegaUnCampeonatoEnteroPorLaAPI: lo que hace la pagina, sin la pagina.
// Se cambia la configuracion, se lanza el modo automatico y se espera al final.
func TestSeJuegaUnCampeonatoEnteroPorLaAPI(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:2]
	cfg.Fases[0].Rondas = 1
	cfg.Fases[0].Clasifican = 2
	ins := inscritosDePrueba(t, 6)
	c, servidor := panelDePrueba(t, cfg, ins)
	for i, in := range ins {
		conectar(c, in, nuevoBot([]decisor{pasivo, conPar}[i%2]))
	}
	esperarConectados(t, c, len(ins))

	// Se cambia por la API la cantidad de bots que clasifican.
	nueva := c.Config()
	nueva.Fases = append([]Fase(nil), nueva.Fases...)
	nueva.Fases[0].Clasifican = 4
	if estado, cuerpo := pedir(t, servidor, "PUT", "/api/config", claveDePrueba, nueva); estado != http.StatusOK {
		t.Fatalf("PUT /api/config dio %d: %s", estado, cuerpo)
	}
	if got := c.Config().Fases[0].Clasifican; got != 4 {
		t.Fatalf("clasifican = %d, se esperaba 4", got)
	}

	if estado, cuerpo := pedir(t, servidor, "POST", "/api/auto", claveDePrueba, nil); estado != http.StatusAccepted {
		t.Fatalf("POST /api/auto dio %d: %s", estado, cuerpo)
	}
	esperarEtapa(t, c, EtapaTerminado)

	// Ya empezo: la configuracion queda bloqueada (409, no 400: el pedido era valido).
	if estado, _ := pedir(t, servidor, "PUT", "/api/config", claveDePrueba, nueva); estado != http.StatusConflict {
		t.Errorf("cambiar la configuracion con el campeonato terminado dio %d, se esperaba 409", estado)
	}
	if estado, _ := pedir(t, servidor, "POST", "/api/ronda", claveDePrueba, nil); estado != http.StatusConflict {
		t.Errorf("jugar un campeonato terminado dio %d, se esperaba 409", estado)
	}

	_, cuerpo := pedir(t, servidor, "GET", "/api/estado", claveDePrueba, nil)
	var snap Instantanea
	json.Unmarshal(cuerpo, &snap)
	if snap.Campeon == "" || len(snap.Rondas) != 2 {
		t.Errorf("campeon %q, %d rondas; se esperaba un campeon y 2 rondas", snap.Campeon, len(snap.Rondas))
	}
}

// TestLasMesasEnJuegoSeVenEnVivo: mientras se juega, el estado trae las mesas
// con sus asientos y sin ninguna carta privada.
func TestLasMesasEnJuegoSeVenEnVivo(t *testing.T) {
	cfg := configDePrueba(t)
	cfg.Fases = cfg.Fases[:1]
	cfg.Fases[0].Rondas = 1
	cfg.Fases[0].Clasifican = 2
	cfg.Fases[0].ManosPorRonda = 200
	ins := inscritosDePrueba(t, 5)
	c, servidor := panelDePrueba(t, cfg, ins)
	for _, in := range ins {
		b := nuevoBot(pasivo)
		b.antesDeJugar = func() { time.Sleep(20 * time.Millisecond) }
		conectar(c, in, b)
	}
	esperarConectados(t, c, len(ins))

	if estado, _ := pedir(t, servidor, "POST", "/api/ronda", claveDePrueba, nil); estado != http.StatusAccepted {
		t.Fatalf("POST /api/ronda dio %d", estado)
	}

	var snap Instantanea
	esperar(t, 10*time.Second, "ver una mesa en juego con asientos", func() bool {
		_, cuerpo := pedir(t, servidor, "GET", "/api/estado", claveDePrueba, nil)
		snap = Instantanea{}
		json.Unmarshal(cuerpo, &snap)
		return len(snap.Mesas) == 1 && len(snap.Mesas[0].Asientos) == 5 && snap.Mesas[0].Mano > 1
	})
	if !snap.Jugando {
		t.Error("el estado no dice que haya una ronda en juego")
	}
	m := snap.Mesas[0]
	if m.CiegaGrande == 0 {
		t.Errorf("la mesa en vivo no trae las ciegas: %+v", m)
	}

	// Ninguna carta privada: el estado de la mesa solo tiene las comunitarias.
	cuerpoCrudo, _ := json.Marshal(snap.Mesas)
	for _, prohibido := range []string{"privadas", "cartas", "mano_privada"} {
		if strings.Contains(string(cuerpoCrudo), prohibido) {
			t.Errorf("la vista de la mesa expone %q", prohibido)
		}
	}
	c.Cancelar()
	esperar(t, 20*time.Second, "que termine la ronda cancelada", func() bool { return !c.Instantanea().Jugando })
}

// TestLaTablaPonePrimeroAQuienLlegoMasLejos: quien pierde en una fase final queda
// con 0 fichas igual que quien quebro en la clasificatoria. Ordenar solo por
// fichas los mezclaria y se perderia quien llego hasta donde.
func TestLaTablaPonePrimeroAQuienLlegoMasLejos(t *testing.T) {
	jugadores := []VistaJugador{
		{Alias: "a-quebro", Estado: MotivoQuiebra, FaseSalida: 0},
		{Alias: "b-final", Estado: MotivoEliminado, FaseSalida: 2, Puesto: 2},
		{Alias: "c-semi", Estado: MotivoEliminado, FaseSalida: 1, Puesto: 3},
		{Alias: "d-campeon", Estado: MotivoCampeon, Fichas: 20000, FaseSalida: -1},
		{Alias: "e-no-paso", Estado: MotivoNoClasifico, FaseSalida: 0, Puesto: 9},
		{Alias: "f-en-juego", Estado: "en_juego", Fichas: 500, FaseSalida: -1},
	}
	sort.Slice(jugadores, func(i, j int) bool { return ordenJugadores(jugadores[i], jugadores[j]) })

	var orden []string
	for _, j := range jugadores {
		orden = append(orden, j.Alias)
	}
	esperado := []string{"d-campeon", "f-en-juego", "b-final", "c-semi", "e-no-paso", "a-quebro"}
	if strings.Join(orden, ",") != strings.Join(esperado, ",") {
		t.Fatalf("orden = %v, se esperaba %v", orden, esperado)
	}
}
