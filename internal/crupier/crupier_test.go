package crupier

import (
	"testing"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// --- Mazo ------------------------------------------------------------------

func TestMazoNuevoTiene52CartasDistintas(t *testing.T) {
	m := NuevoMazo()
	if m.Restantes() != 52 {
		t.Fatalf("mazo nuevo con %d cartas, se esperaban 52", m.Restantes())
	}

	vistas := map[protocolo.Carta]bool{}
	for i := 0; i < 52; i++ {
		c, err := m.Robar()
		if err != nil {
			t.Fatalf("Robar carta %d: %v", i, err)
		}
		if vistas[c] {
			t.Fatalf("la carta %s salio dos veces", c)
		}
		vistas[c] = true
	}
	if _, err := m.Robar(); err == nil {
		t.Fatal("robar del mazo agotado deberia fallar")
	}
}

func TestBarajarNoPierdeNiDuplicaCartas(t *testing.T) {
	m := NuevoMazo()
	if err := m.Barajar(); err != nil {
		t.Fatalf("Barajar: %v", err)
	}
	if m.Restantes() != 52 {
		t.Fatalf("tras barajar quedan %d cartas, se esperaban 52", m.Restantes())
	}

	vistas := map[protocolo.Carta]bool{}
	for i := 0; i < 52; i++ {
		c, _ := m.Robar()
		if vistas[c] {
			t.Fatalf("la carta %s aparece dos veces tras barajar", c)
		}
		vistas[c] = true
	}
}

// TestMismaSemillaMismoMazo es la propiedad de la que depende el formato de
// ida y vuelta de la arena: si esto se rompe, los dos lados de un
// emparejamiento dejan de jugar las mismas cartas y el torneo pasa a medir
// suerte.
func TestMismaSemillaMismoMazo(t *testing.T) {
	repartir := func(semilla uint64) []protocolo.Carta {
		m := NuevoMazoCon(FuenteSembrada(semilla))
		if err := m.Barajar(); err != nil {
			t.Fatalf("Barajar: %v", err)
		}
		var cartas []protocolo.Carta
		for i := 0; i < 52; i++ {
			c, _ := m.Robar()
			cartas = append(cartas, c)
		}
		return cartas
	}

	a, b := repartir(1234), repartir(1234)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("dos mazos con la misma semilla difieren en la posicion %d: %s vs %s", i, a[i], b[i])
		}
	}

	distinto := repartir(9999)
	iguales := 0
	for i := range a {
		if a[i] == distinto[i] {
			iguales++
		}
	}
	if iguales == 52 {
		t.Fatal("dos semillas distintas produjeron el mismo mazo")
	}
}

// TestCrupierSembradoRepiteLaPartida cubre el nivel de arriba: no solo el
// mazo, sino la secuencia completa de manos de un crupier.
func TestCrupierSembradoRepiteLaPartida(t *testing.T) {
	jugar := func() []protocolo.Carta {
		c := NuevoConSemilla(77)
		var todo []protocolo.Carta
		for mano := 0; mano < 3; mano++ {
			if err := c.NuevaMano("m"); err != nil {
				t.Fatalf("NuevaMano: %v", err)
			}
			manos, err := c.RepartirPrivadas(2)
			if err != nil {
				t.Fatalf("RepartirPrivadas: %v", err)
			}
			for _, m := range manos {
				todo = append(todo, m[0], m[1])
			}
			for _, etapa := range []protocolo.Etapa{protocolo.Flop, protocolo.Turn, protocolo.River} {
				cartas, err := c.RepartirComunitarias(etapa)
				if err != nil {
					t.Fatalf("RepartirComunitarias(%s): %v", etapa, err)
				}
				todo = append(todo, cartas...)
			}
		}
		return todo
	}

	a, b := jugar(), jugar()
	if len(a) != len(b) {
		t.Fatalf("dos partidas con la misma semilla repartieron %d y %d cartas", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("las partidas divergen en la carta %d: %s vs %s", i, a[i], b[i])
		}
	}
}

// --- Reparto ---------------------------------------------------------------

func TestRepartirPrivadasNoRepiteCartas(t *testing.T) {
	c := NuevoConSemilla(5)
	if err := c.NuevaMano("m-1"); err != nil {
		t.Fatalf("NuevaMano: %v", err)
	}

	manos, err := c.RepartirPrivadas(8)
	if err != nil {
		t.Fatalf("RepartirPrivadas: %v", err)
	}
	if len(manos) != 8 {
		t.Fatalf("se repartieron %d manos, se esperaban 8", len(manos))
	}

	vistas := map[protocolo.Carta]string{}
	for i, mano := range manos {
		for _, carta := range mano {
			if antes, repetida := vistas[carta]; repetida {
				t.Fatalf("la carta %s se repartio a %s y al jugador %d", carta, antes, i)
			}
			vistas[carta] = "jugador " + string(rune('0'+i))
		}
	}
}

func TestRepartirComunitariasQuemaUnaCarta(t *testing.T) {
	c := NuevoConSemilla(5)
	c.NuevaMano("m-1")

	motorConcreto, ok := c.(*motor)
	if !ok {
		t.Fatal("Nuevo no devolvio un *motor")
	}

	antes := motorConcreto.mazo.Restantes()
	cartas, err := c.RepartirComunitarias(protocolo.Flop)
	if err != nil {
		t.Fatalf("RepartirComunitarias(flop): %v", err)
	}
	if len(cartas) != 3 {
		t.Fatalf("el flop trajo %d cartas, se esperaban 3", len(cartas))
	}
	// 3 repartidas + 1 quemada.
	if consumidas := antes - motorConcreto.mazo.Restantes(); consumidas != 4 {
		t.Fatalf("el flop consumio %d cartas, se esperaban 4 (3 + 1 quemada)", consumidas)
	}

	for _, etapa := range []protocolo.Etapa{protocolo.Turn, protocolo.River} {
		antes = motorConcreto.mazo.Restantes()
		cartas, err = c.RepartirComunitarias(etapa)
		if err != nil {
			t.Fatalf("RepartirComunitarias(%s): %v", etapa, err)
		}
		if len(cartas) != 1 {
			t.Fatalf("%s trajo %d cartas, se esperaba 1", etapa, len(cartas))
		}
		if consumidas := antes - motorConcreto.mazo.Restantes(); consumidas != 2 {
			t.Fatalf("%s consumio %d cartas, se esperaban 2 (1 + 1 quemada)", etapa, consumidas)
		}
	}
}

func TestRepartirComunitariasRechazaEtapasSinCartas(t *testing.T) {
	c := NuevoConSemilla(5)
	c.NuevaMano("m-1")
	for _, etapa := range []protocolo.Etapa{protocolo.PreFlop, protocolo.Showdown, "inventada"} {
		if _, err := c.RepartirComunitarias(etapa); err == nil {
			t.Errorf("RepartirComunitarias(%s) deberia fallar", etapa)
		}
	}
}

// --- Pozo ------------------------------------------------------------------

func TestPozoSinAllInEsUnSoloSubPozo(t *testing.T) {
	p := NuevoPozo([]string{"a", "b", "c"})
	p.Apostar("a", 100)
	p.Apostar("b", 100)
	p.Apostar("c", 100)

	if total := p.Total(); total != 300 {
		t.Fatalf("total = %d, se esperaba 300", total)
	}
	sub := p.Descomponer()
	if len(sub) != 1 {
		t.Fatalf("se armaron %d sub-pozos, se esperaba 1: %+v", len(sub), sub)
	}
	if sub[0].Monto != 300 || len(sub[0].Elegibles) != 3 {
		t.Fatalf("sub-pozo = %+v, se esperaban 300 fichas y 3 elegibles", sub[0])
	}
}

// TestPozoLateralConAllInCorto es el caso que justifica todo el mecanismo de
// sub-pozos: un jugador que se queda sin fichas no puede ganar lo que apostaron
// los demas por encima de lo que el cubrio.
func TestPozoLateralConAllInCorto(t *testing.T) {
	p := NuevoPozo([]string{"corto", "medio", "grande"})
	p.Apostar("corto", 50)   // all-in por 50
	p.Apostar("medio", 200)  //
	p.Apostar("grande", 200) //

	if total := p.Total(); total != 450 {
		t.Fatalf("total = %d, se esperaba 450", total)
	}

	sub := p.Descomponer()
	if len(sub) != 2 {
		t.Fatalf("se armaron %d sub-pozos, se esperaban 2: %+v", len(sub), sub)
	}

	// Principal: 50 de cada uno, los tres pueden ganarlo.
	if sub[0].Monto != 150 || len(sub[0].Elegibles) != 3 {
		t.Errorf("pozo principal = %+v, se esperaban 150 fichas y 3 elegibles", sub[0])
	}
	// Lateral: 150 de cada uno de los dos grandes, "corto" no puede ganarlo.
	if sub[1].Monto != 300 || len(sub[1].Elegibles) != 2 {
		t.Errorf("pozo lateral = %+v, se esperaban 300 fichas y 2 elegibles", sub[1])
	}
	for _, id := range sub[1].Elegibles {
		if id == "corto" {
			t.Error("el jugador all-in por 50 no deberia ser elegible para el pozo lateral")
		}
	}

	if suma := sub[0].Monto + sub[1].Monto; suma != p.Total() {
		t.Fatalf("los sub-pozos suman %d, el pozo tiene %d: se estan perdiendo fichas", suma, p.Total())
	}
}

// TestPozoConservaLasFichasDelQueSeRetira: quien hace fold deja sus fichas en
// el pozo pero deja de poder ganarlo.
func TestPozoConservaLasFichasDelQueSeRetira(t *testing.T) {
	p := NuevoPozo([]string{"a", "b", "c"})
	p.Apostar("a", 100)
	p.Apostar("b", 100)
	p.Apostar("c", 40)
	p.Fold("c")

	if total := p.Total(); total != 240 {
		t.Fatalf("total = %d, se esperaba 240: las fichas del que se retira se quedan en el pozo", total)
	}

	for _, sp := range p.Descomponer() {
		for _, id := range sp.Elegibles {
			if id == "c" {
				t.Fatalf("el jugador retirado sigue elegible en %+v", sp)
			}
		}
	}

	var suma int64
	for _, sp := range p.Descomponer() {
		suma += sp.Monto
	}
	if suma != 240 {
		t.Fatalf("los sub-pozos suman %d, el pozo tiene 240", suma)
	}
}

func TestPozoRechazaApuestasNegativas(t *testing.T) {
	p := NuevoPozo([]string{"a"})
	if err := p.Apostar("a", -1); err == nil {
		t.Fatal("apostar un monto negativo deberia fallar")
	}
}

// --- Decidir ganadores -----------------------------------------------------

// cartasDe parsea "As Kh" a cartas, igual que en pkg/poker.
func cartasDe(t *testing.T, notacion string) []protocolo.Carta {
	t.Helper()
	rangos := map[byte]protocolo.Rango{
		'2': protocolo.Dos, '3': protocolo.Tres, '4': protocolo.Cuatro, '5': protocolo.Cinco,
		'6': protocolo.Seis, '7': protocolo.Siete, '8': protocolo.Ocho, '9': protocolo.Nueve,
		'T': protocolo.Diez, 'J': protocolo.Jota, 'Q': protocolo.Reina, 'K': protocolo.Rey,
		'A': protocolo.As,
	}
	palos := map[byte]protocolo.Palo{
		'c': protocolo.Treboles, 'd': protocolo.Diamantes,
		'h': protocolo.Corazones, 's': protocolo.Picas,
	}
	var salida []protocolo.Carta
	for i := 0; i+1 < len(notacion); i += 3 {
		salida = append(salida, protocolo.Carta{Rango: rangos[notacion[i]], Palo: palos[notacion[i+1]]})
	}
	return salida
}

func manoDe(t *testing.T, notacion string) protocolo.Mano {
	t.Helper()
	var m protocolo.Mano
	copy(m[:], cartasDe(t, notacion))
	return m
}

func TestDecidirGanadoresRepartePozoSimple(t *testing.T) {
	c := Nuevo()
	comunitarias := cartasDe(t, "2c 7d 9h Ts 3c")

	pozo := NuevoPozo([]string{"gana", "pierde"})
	pozo.Apostar("gana", 100)
	pozo.Apostar("pierde", 100)

	res, err := c.DecidirGanadores([]Participante{
		{ID: "gana", Mano: manoDe(t, "Ac Ad")},   // par de ases
		{ID: "pierde", Mano: manoDe(t, "Kc Kd")}, // par de reyes
	}, comunitarias, pozo)
	if err != nil {
		t.Fatalf("DecidirGanadores: %v", err)
	}

	if len(res.Repartos) != 1 {
		t.Fatalf("se repartio a %d jugadores, se esperaba 1: %+v", len(res.Repartos), res.Repartos)
	}
	if res.Repartos[0].IDJugador != "gana" || res.Repartos[0].Monto != 200 {
		t.Fatalf("reparto = %+v, se esperaba gana con 200", res.Repartos[0])
	}
}

func TestDecidirGanadoresDividePozoEnEmpate(t *testing.T) {
	c := Nuevo()
	// La escalera esta entera en la mesa: los dos juegan lo mismo.
	comunitarias := cartasDe(t, "9c 8d 7h 6s 5c")

	pozo := NuevoPozo([]string{"a", "b"})
	pozo.Apostar("a", 100)
	pozo.Apostar("b", 100)

	res, err := c.DecidirGanadores([]Participante{
		{ID: "a", Mano: manoDe(t, "Ac Kd")},
		{ID: "b", Mano: manoDe(t, "Qc Jd")},
	}, comunitarias, pozo)
	if err != nil {
		t.Fatalf("DecidirGanadores: %v", err)
	}

	if len(res.Repartos) != 2 {
		t.Fatalf("se esperaba dividir entre 2, se repartio a %d: %+v", len(res.Repartos), res.Repartos)
	}
	var total int64
	for _, r := range res.Repartos {
		if r.Monto != 100 {
			t.Errorf("%s recibio %d, se esperaban 100", r.IDJugador, r.Monto)
		}
		total += r.Monto
	}
	if total != 200 {
		t.Fatalf("se repartieron %d fichas, el pozo tenia 200", total)
	}
}

// TestDecidirGanadoresConPozoLateral es el escenario completo: el jugador
// all-in corto tiene la mejor mano y se lleva el pozo principal, pero el
// lateral se lo lleva el mejor de los dos que siguieron apostando.
func TestDecidirGanadoresConPozoLateral(t *testing.T) {
	c := Nuevo()
	comunitarias := cartasDe(t, "2c 7d 9h Ts 3c")

	pozo := NuevoPozo([]string{"corto", "medio", "grande"})
	pozo.Apostar("corto", 50)
	pozo.Apostar("medio", 200)
	pozo.Apostar("grande", 200)

	res, err := c.DecidirGanadores([]Participante{
		{ID: "corto", Mano: manoDe(t, "Ac Ad")},  // par de ases: la mejor
		{ID: "medio", Mano: manoDe(t, "Kc Kd")},  // par de reyes
		{ID: "grande", Mano: manoDe(t, "Qc Qd")}, // par de damas
	}, comunitarias, pozo)
	if err != nil {
		t.Fatalf("DecidirGanadores: %v", err)
	}

	recibido := map[string]int64{}
	var total int64
	for _, r := range res.Repartos {
		recibido[r.IDJugador] += r.Monto
		total += r.Monto
	}

	if total != 450 {
		t.Fatalf("se repartieron %d fichas, el pozo tenia 450", total)
	}
	// Principal: 50 x 3 = 150, se lo lleva "corto".
	if recibido["corto"] != 150 {
		t.Errorf("corto recibio %d, se esperaban 150 (solo el pozo principal)", recibido["corto"])
	}
	// Lateral: 150 x 2 = 300, se lo lleva "medio" (reyes le gana a damas).
	if recibido["medio"] != 300 {
		t.Errorf("medio recibio %d, se esperaban 300 (el pozo lateral)", recibido["medio"])
	}
	if recibido["grande"] != 0 {
		t.Errorf("grande recibio %d, no deberia ganar nada", recibido["grande"])
	}
}

// TestDecidirGanadoresEsDeterminista: dos corridas identicas tienen que
// producir exactamente el mismo resultado, incluido el orden de los repartos.
// Los resultados terminan en el historial de manos y en el mensaje mano_fin;
// si el orden bailara, dos replicas de la misma partida no serian comparables.
func TestDecidirGanadoresEsDeterminista(t *testing.T) {
	decidir := func() protocolo.ResultadoMano {
		c := Nuevo()
		pozo := NuevoPozo([]string{"a", "b", "c", "d"})
		for _, id := range []string{"a", "b", "c", "d"} {
			pozo.Apostar(id, 100)
		}
		// Los cuatro juegan la escalera de la mesa: empate a cuatro bandas,
		// que es donde el orden del reparto es visible.
		res, err := c.DecidirGanadores([]Participante{
			{ID: "a", Mano: manoDe(t, "Ac Kd")},
			{ID: "b", Mano: manoDe(t, "Ah Kh")},
			{ID: "c", Mano: manoDe(t, "As Ks")},
			{ID: "d", Mano: manoDe(t, "Ad Kc")},
		}, cartasDe(t, "9c 8d 7h 6s 5c"), pozo)
		if err != nil {
			t.Fatalf("DecidirGanadores: %v", err)
		}
		return res
	}

	primero := decidir()
	for i := 0; i < 20; i++ {
		otro := decidir()
		if len(primero.Repartos) != len(otro.Repartos) {
			t.Fatalf("cantidad de repartos inestable: %d vs %d", len(primero.Repartos), len(otro.Repartos))
		}
		for j := range primero.Repartos {
			if primero.Repartos[j] != otro.Repartos[j] {
				t.Fatalf("el reparto %d cambio entre corridas: %+v vs %+v", j, primero.Repartos[j], otro.Repartos[j])
			}
		}
	}
}

// TestDecidirGanadoresDaLaFichaSobranteAlPrimero cubre la regla de
// docs/reglas.md: un pozo que no divide exacto le deja la ficha de mas al
// primero de la lista de participantes, que la Mesa arma desde la izquierda
// del boton.
//
// Para que haya ficha impar de verdad hacen falta tres aportes iguales y un
// fold: si los aportes fueran distintos, la diferencia no igualada se separa
// en un sub-pozo propio y vuelve sola a quien la apostó (que tambien es
// correcto, pero no es este caso).
func TestDecidirGanadoresDaLaFichaSobranteAlPrimero(t *testing.T) {
	c := Nuevo()

	pozo := NuevoPozo([]string{"primero", "segundo", "retirado"})
	pozo.Apostar("primero", 1)
	pozo.Apostar("segundo", 1)
	pozo.Apostar("retirado", 1)
	pozo.Fold("retirado")

	// Los dos juegan la escalera de la mesa: empatan, y 3 fichas no se
	// dividen exacto entre 2.
	res, err := c.DecidirGanadores([]Participante{
		{ID: "primero", Mano: manoDe(t, "Ac Kd")},
		{ID: "segundo", Mano: manoDe(t, "Ah Kh")},
	}, cartasDe(t, "9c 8d 7h 6s 5c"), pozo)
	if err != nil {
		t.Fatalf("DecidirGanadores: %v", err)
	}

	recibido := map[string]int64{}
	var total int64
	for _, r := range res.Repartos {
		recibido[r.IDJugador] = r.Monto
		total += r.Monto
	}
	if total != 3 {
		t.Fatalf("se repartieron %d fichas, el pozo tenia 3", total)
	}
	if recibido["primero"] != 2 || recibido["segundo"] != 1 {
		t.Fatalf("la ficha sobrante deberia ir al primero de la lista, se repartio %+v", recibido)
	}
}

// TestDecidirGanadoresDevuelveLaApuestaNoIgualada: si alguien apostó más de lo
// que cualquier rival podía cubrir, ese excedente forma su propio sub-pozo y
// vuelve a él, gane o pierda la mano. Es la contracara del caso de arriba.
func TestDecidirGanadoresDevuelveLaApuestaNoIgualada(t *testing.T) {
	c := Nuevo()

	pozo := NuevoPozo([]string{"corto", "sobra"})
	pozo.Apostar("corto", 50)  // all-in
	pozo.Apostar("sobra", 200) // 150 de mas que nadie iguala

	res, err := c.DecidirGanadores([]Participante{
		{ID: "corto", Mano: manoDe(t, "Ac Ad")}, // par de ases: gana la mano
		{ID: "sobra", Mano: manoDe(t, "Kc Kd")},
	}, cartasDe(t, "2c 7d 9h Ts 3c"), pozo)
	if err != nil {
		t.Fatalf("DecidirGanadores: %v", err)
	}

	recibido := map[string]int64{}
	for _, r := range res.Repartos {
		recibido[r.IDJugador] = r.Monto
	}
	if recibido["corto"] != 100 {
		t.Errorf("corto gano la mano: deberia llevarse los 100 igualados, se llevo %d", recibido["corto"])
	}
	if recibido["sobra"] != 150 {
		t.Errorf("las 150 fichas que nadie igualo deberian volver a quien las apostó, volvieron %d", recibido["sobra"])
	}
}

func TestDecidirGanadoresRechazaPozoNulo(t *testing.T) {
	c := Nuevo()
	if _, err := c.DecidirGanadores(nil, nil, nil); err == nil {
		t.Fatal("DecidirGanadores con pozo nulo deberia fallar")
	}
}
