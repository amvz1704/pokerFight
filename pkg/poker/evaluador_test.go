package poker

import (
	"testing"

	"github.com/amvz1704/pokerFight/internal/protocolo"
)

// cartas parsea una notación corta ("As Kh 2c") a cartas. Escribir manos a
// mano con structs hace los tests ilegibles, que es la forma más rápida de que
// nadie escriba tests para el evaluador.
func cartas(t *testing.T, notacion string) []protocolo.Carta {
	t.Helper()

	rangos := map[byte]protocolo.Rango{
		'2': protocolo.Dos, '3': protocolo.Tres, '4': protocolo.Cuatro,
		'5': protocolo.Cinco, '6': protocolo.Seis, '7': protocolo.Siete,
		'8': protocolo.Ocho, '9': protocolo.Nueve, 'T': protocolo.Diez,
		'J': protocolo.Jota, 'Q': protocolo.Reina, 'K': protocolo.Rey,
		'A': protocolo.As,
	}
	palos := map[byte]protocolo.Palo{
		'c': protocolo.Treboles, 'd': protocolo.Diamantes,
		'h': protocolo.Corazones, 's': protocolo.Picas,
	}

	var salida []protocolo.Carta
	for i := 0; i+1 < len(notacion); i += 3 {
		rango, okRango := rangos[notacion[i]]
		palo, okPalo := palos[notacion[i+1]]
		if !okRango || !okPalo {
			t.Fatalf("notacion de carta invalida en %q, posicion %d", notacion, i)
		}
		salida = append(salida, protocolo.Carta{Rango: rango, Palo: palo})
	}
	return salida
}

func cinco(t *testing.T, notacion string) [5]protocolo.Carta {
	t.Helper()
	lista := cartas(t, notacion)
	if len(lista) != 5 {
		t.Fatalf("se esperaban 5 cartas en %q, hay %d", notacion, len(lista))
	}
	var c5 [5]protocolo.Carta
	copy(c5[:], lista)
	return c5
}

func TestEvaluar5Categorias(t *testing.T) {
	casos := []struct {
		nombre   string
		mano     string
		esperada Categoria
	}{
		{"escalera real", "As Ks Qs Js Ts", EscaleraDeColor},
		{"escalera de color", "9h 8h 7h 6h 5h", EscaleraDeColor},
		{"escalera de color baja (rueda)", "Ad 2d 3d 4d 5d", EscaleraDeColor},
		{"poker", "7c 7d 7h 7s 2c", Poker},
		{"full house", "Kc Kd Kh 8s 8c", FullHouse},
		{"color", "Ac Jc 9c 5c 2c", Color},
		{"escalera", "9c 8d 7h 6s 5c", Escalera},
		{"escalera baja (rueda)", "Ac 2d 3h 4s 5c", Escalera},
		{"trio", "Qc Qd Qh 7s 2c", Trio},
		{"doble par", "Jc Jd 4h 4s 9c", DoblePar},
		{"par", "Tc Td 8h 5s 2c", Par},
		{"carta alta", "Ac Jd 9h 5s 2c", CartaAlta},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			ev := Evaluar5(cinco(t, caso.mano))
			if ev.Categoria != caso.esperada {
				t.Fatalf("%s (%s) = %v, se esperaba %v", caso.nombre, caso.mano, ev.Categoria, caso.esperada)
			}
		})
	}
}

// TestEscaleraRealSeDescribeAparte cubre que la escalera real, aunque comparte
// categoria con la escalera de color, se nombre distinto en el resultado de la
// mano (es lo que ve el bot en mano_fin).
func TestEscaleraRealSeDescribeAparte(t *testing.T) {
	real := Evaluar5(cinco(t, "As Ks Qs Js Ts"))
	otra := Evaluar5(cinco(t, "9h 8h 7h 6h 5h"))

	if real.Descripcion != "escalera real" {
		t.Errorf("descripcion = %q, se esperaba \"escalera real\"", real.Descripcion)
	}
	if otra.Descripcion != "escalera de color" {
		t.Errorf("descripcion = %q, se esperaba \"escalera de color\"", otra.Descripcion)
	}
	if real.Puntaje <= otra.Puntaje {
		t.Errorf("la escalera real (%d) deberia ganarle a la de 9 (%d)", real.Puntaje, otra.Puntaje)
	}
}

// TestOrdenEntreCategorias verifica la jerarquia completa de docs/reglas.md.
func TestOrdenEntreCategorias(t *testing.T) {
	// De la peor a la mejor.
	escalera := []string{
		"Ac Jd 9h 5s 2c", // carta alta
		"Tc Td 8h 5s 2c", // par
		"Jc Jd 4h 4s 9c", // doble par
		"Qc Qd Qh 7s 2c", // trio
		"9c 8d 7h 6s 5c", // escalera
		"Ac Jc 9c 5c 2c", // color
		"Kc Kd Kh 8s 8c", // full house
		"7c 7d 7h 7s 2c", // poker
		"9h 8h 7h 6h 5h", // escalera de color
	}

	for i := 1; i < len(escalera); i++ {
		peor := Evaluar5(cinco(t, escalera[i-1]))
		mejor := Evaluar5(cinco(t, escalera[i]))
		if mejor.Puntaje <= peor.Puntaje {
			t.Errorf("%s (%s, %d) deberia ganarle a %s (%s, %d)",
				mejor.Descripcion, escalera[i], mejor.Puntaje,
				peor.Descripcion, escalera[i-1], peor.Puntaje)
		}
	}
}

// TestDesempatePorKicker cubre el caso que mas se equivoca al implementar un
// evaluador: dos manos de la misma categoria que se resuelven por la carta
// suelta.
func TestDesempatePorKicker(t *testing.T) {
	casos := []struct {
		nombre string
		mejor  string
		peor   string
	}{
		{"par de ases, kicker rey vs kicker dama", "Ac Ad Kh 5s 2c", "Ac Ad Qh 5s 2c"},
		{"trio de reyes, kickers mas altos", "Kc Kd Kh As 2c", "Kc Kd Kh Qs 2c"},
		{"doble par, mismo par alto y par bajo mayor", "Ac Ad 9h 9s 2c", "Ac Ad 8h 8s Kc"},
		{"color, carta mas alta", "Ac Jc 9c 5c 2c", "Kc Jc 9c 5c 2c"},
		{"carta alta, quinto kicker", "Ac Jd 9h 5s 3c", "Ac Jd 9h 5s 2c"},
		{"full house, trio manda sobre el par", "8c 8d 8h Ks Kc", "7c 7d 7h As Ac"},
		{"escalera al as vs escalera al rey", "Ac Kd Qh Js Tc", "Kc Qd Jh Ts 9c"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			mejor := Evaluar5(cinco(t, caso.mejor))
			peor := Evaluar5(cinco(t, caso.peor))
			if mejor.Puntaje <= peor.Puntaje {
				t.Fatalf("%s (%d) deberia ganarle a %s (%d)",
					caso.mejor, mejor.Puntaje, caso.peor, peor.Puntaje)
			}
		})
	}
}

// TestRuedaPierdeContraSeisAlta es el error clasico del as bajo: si el
// evaluador no baja el as a 1, A-2-3-4-5 le ganaria a 6-5-4-3-2, que es al
// reves de la regla.
func TestRuedaPierdeContraSeisAlta(t *testing.T) {
	rueda := Evaluar5(cinco(t, "Ac 2d 3h 4s 5c"))
	seisAlta := Evaluar5(cinco(t, "6c 5d 4h 3s 2c"))

	if rueda.Categoria != Escalera || seisAlta.Categoria != Escalera {
		t.Fatalf("las dos deberian ser escalera, son %v y %v", rueda.Categoria, seisAlta.Categoria)
	}
	if rueda.Puntaje >= seisAlta.Puntaje {
		t.Fatalf("la rueda (%d) no deberia ganarle a la escalera al 6 (%d): el as vale 1", rueda.Puntaje, seisAlta.Puntaje)
	}
}

// TestManosIgualesEmpatan cubre que el pozo se divida cuando corresponde: dos
// manos con el mismo valor pero distinto palo tienen que dar el mismo puntaje,
// porque en Hold'em los palos no desempatan.
func TestManosIgualesEmpatan(t *testing.T) {
	a := Evaluar5(cinco(t, "Ac Ad Kh Qs Jc"))
	b := Evaluar5(cinco(t, "As Ah Kc Qd Jh"))
	if a.Puntaje != b.Puntaje {
		t.Fatalf("dos pares de ases con los mismos kickers deberian empatar: %d vs %d", a.Puntaje, b.Puntaje)
	}
}

func TestMejorDeSieteCartas(t *testing.T) {
	casos := []struct {
		nombre    string
		siete     string
		categoria Categoria
		descrip   string
	}{
		{"elige el color entre 7 cartas", "Ac Kc 2c 7c 9c 3d 4h", Color, "color"},
		{"elige la escalera y descarta el par", "9c 8d 7h 6s 5c 5d 2h", Escalera, "escalera"},
		{"full house con dos trios disponibles", "Kc Kd Kh 8s 8c 8d 2h", FullHouse, "full house"},
		{"poker con kicker mas alto", "7c 7d 7h 7s Ac Kd 2h", Poker, "poker"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			ev, err := MejorDe(cartas(t, caso.siete))
			if err != nil {
				t.Fatalf("MejorDe: %v", err)
			}
			if ev.Categoria != caso.categoria {
				t.Fatalf("categoria = %v (%s), se esperaba %v", ev.Categoria, ev.Descripcion, caso.categoria)
			}
			if ev.Descripcion != caso.descrip {
				t.Errorf("descripcion = %q, se esperaba %q", ev.Descripcion, caso.descrip)
			}
		})
	}
}

// TestMejorDePokerConKicker cubre que, con poker servido en la mesa, gane el
// que tenga el kicker mas alto y no se declare empate.
func TestMejorDePokerConKicker(t *testing.T) {
	comunitarias := cartas(t, "7c 7d 7h 7s 2c")

	conAs, err := MejorDe(append(cartas(t, "Ac Kd"), comunitarias...))
	if err != nil {
		t.Fatalf("MejorDe: %v", err)
	}
	conTres, err := MejorDe(append(cartas(t, "3c 4d"), comunitarias...))
	if err != nil {
		t.Fatalf("MejorDe: %v", err)
	}
	if conAs.Puntaje <= conTres.Puntaje {
		t.Fatalf("con poker en la mesa deberia ganar el kicker mas alto: as %d vs tres %d", conAs.Puntaje, conTres.Puntaje)
	}
}

func TestMejorConMenosDeCincoCartas(t *testing.T) {
	if _, err := MejorDe(cartas(t, "Ac Kd")); err == nil {
		t.Fatal("evaluar 2 cartas deberia devolver error")
	}
	// La version comoda para bots no falla: devuelve el cero, que es lo que
	// necesita un bot preguntando en preflop.
	if ev := Mejor(protocolo.Mano{}, nil); ev.Puntaje != 0 || ev.Categoria != CartaAlta {
		t.Fatalf("Mejor sin cartas suficientes deberia devolver el cero, dio %+v", ev)
	}
}

// TestMejorNoModificaLasComunitarias protege contra un aliasing sutil: Mejor
// arma su slice con append sobre las privadas y el evaluador ordena las cartas
// para puntuarlas. Si eso llegara a tocar el slice de comunitarias, la mesa
// veria las cartas de la mesa reordenadas despues de cada showdown.
func TestMejorNoModificaLasComunitarias(t *testing.T) {
	comunitarias := cartas(t, "2c 7d 9h Ts Jc")
	antes := append([]protocolo.Carta(nil), comunitarias...)

	mano := protocolo.Mano{}
	copy(mano[:], cartas(t, "Ac Kd"))
	Mejor(mano, comunitarias)

	for i := range antes {
		if comunitarias[i] != antes[i] {
			t.Fatalf("las comunitarias se modificaron: %v -> %v", antes, comunitarias)
		}
	}
}

func BenchmarkMejorDeSiete(b *testing.B) {
	siete := []protocolo.Carta{
		{Rango: protocolo.As, Palo: protocolo.Picas},
		{Rango: protocolo.Rey, Palo: protocolo.Picas},
		{Rango: protocolo.Reina, Palo: protocolo.Corazones},
		{Rango: protocolo.Jota, Palo: protocolo.Diamantes},
		{Rango: protocolo.Diez, Palo: protocolo.Treboles},
		{Rango: protocolo.Nueve, Palo: protocolo.Picas},
		{Rango: protocolo.Dos, Palo: protocolo.Picas},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		MejorDe(siete)
	}
}
