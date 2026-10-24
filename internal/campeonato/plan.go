package campeonato

import (
	"fmt"
	"sort"
	"strings"
)

// FasePlan es lo que va a pasar en una fase, calculado de antemano. Es lo que
// ve el organizador al configurar el campeonato: cuántas mesas, de qué tamaño y
// cuántos jugadores pasan a la fase siguiente.
type FasePlan struct {
	Indice int    `json:"indice"`
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`

	// Entran son los jugadores con los que arranca la fase.
	Entran int `json:"entran"`
	// Mesas son los tamaños de las mesas, de mayor a menor.
	Mesas []int `json:"mesas"`
	// MesasTexto es lo mismo en una frase: "13 mesas: 3 de 6 y 10 de 5".
	MesasTexto string `json:"mesas_texto"`
	// Salen son los jugadores que pasan a la fase siguiente.
	Salen int `json:"salen"`

	// Rondas es cuántas veces se sortean las mesas en la fase.
	Rondas int `json:"rondas"`
	// ManosPorMesa es el tope de manos de cada mesa en cada ronda.
	ManosPorMesa int64 `json:"manos_por_mesa"`

	// Avisos son cosas que el organizador debería mirar: mesas que no se pueden
	// armar, fases que no eliminan a nadie, una última fase sin campeón único.
	Avisos []string `json:"avisos,omitempty"`
}

// Plan calcula las fases del campeonato para una cantidad de inscritos.
//
// En la clasificatoria las mesas se sortean de nuevo en cada ronda con los que
// siguen en pie, así que el plan muestra las del arranque: si hay eliminados
// habrá menos. Eso no se puede saber de antemano.
//
// Un plan que se rompe a mitad de camino (porque no quedan jugadores
// suficientes) no es un error: devuelve las fases que sí se pueden calcular y
// lo avisa. Es lo que el organizador necesita ver mientras ajusta números.
func Plan(cfg Config, inscritos int) ([]FasePlan, error) {
	if err := cfg.Validar(); err != nil {
		return nil, err
	}

	planes := make([]FasePlan, 0, len(cfg.Fases))
	entran := inscritos

	for i, fase := range cfg.Fases {
		plan := FasePlan{Indice: i, Nombre: fase.Nombre, Tipo: fase.Tipo, Entran: entran}

		if entran < 2 {
			plan.Avisos = append(plan.Avisos, fmt.Sprintf("a esta fase llegarían %d jugadores: hacen falta al menos 2", entran))
			planes = append(planes, plan)
			break
		}

		tamanos, aviso, err := RepartirMesas(entran, fase.JugadoresPorMesa, fase.Mesas)
		if err != nil {
			plan.Avisos = append(plan.Avisos, err.Error())
			planes = append(planes, plan)
			break
		}
		if aviso != "" {
			plan.Avisos = append(plan.Avisos, aviso)
		}
		plan.Mesas = tamanos
		plan.MesasTexto = DescribirMesas(tamanos)

		switch fase.Tipo {
		case Clasificatoria:
			plan.Rondas = fase.Rondas
			plan.ManosPorMesa = fase.ManosPorRonda
			plan.Salen = fase.Clasifican
			if plan.Salen == 0 {
				plan.Salen = ClasificanAuto(entran)
			}
			if plan.Salen >= entran {
				plan.Salen = entran
				plan.Avisos = append(plan.Avisos, "pasan todos los jugadores: la clasificatoria no elimina a nadie")
			}

		case Eliminatoria:
			plan.Rondas = 1
			plan.ManosPorMesa = fase.MaxManos
			for _, tamano := range tamanos {
				if fase.AvanzanPorMesa >= tamano {
					plan.Avisos = append(plan.Avisos, fmt.Sprintf(
						"hay mesas de %d y pasan %d por mesa: en ellas no se elimina a nadie", tamano, fase.AvanzanPorMesa))
					plan.Salen += tamano
					continue
				}
				plan.Salen += fase.AvanzanPorMesa
			}
		}

		planes = append(planes, plan)
		entran = plan.Salen
	}

	if n := len(planes); n > 0 && len(planes) == len(cfg.Fases) {
		if ultima := &planes[n-1]; ultima.Salen != 1 && len(ultima.Avisos) == 0 {
			ultima.Avisos = append(ultima.Avisos, fmt.Sprintf(
				"la última fase deja %d jugadores: no hay un campeón único", ultima.Salen))
		}
	}
	return planes, nil
}

// RepartirMesas decide los tamaños de mesa para repartir `jugadores`.
//
// Si se fija la cantidad de mesas, se reparte lo más parejo posible entre
// ellas. Si no, se busca el tamaño `porMesa` y, cuando la división no es
// exacta, algunas mesas llevan un jugador de más (las bases: "mesas de 5; si la
// división no es exacta, algunas mesas tienen 6"). Si con eso no alcanza para
// absorber el resto (9 jugadores en mesas de 5 darían una mesa de 9), se arman
// más mesas y algunas quedan de un jugador menos.
//
// Las mesas nunca tienen menos de 2 jugadores. Si se piden más mesas de las que
// los jugadores permiten, se arman las que se puede y se avisa: pasa en la
// clasificatoria cuando hubo muchos eliminados.
func RepartirMesas(jugadores, porMesa, mesas int) (tamanos []int, aviso string, err error) {
	if jugadores < 2 {
		return nil, "", fmt.Errorf("hacen falta al menos 2 jugadores, hay %d", jugadores)
	}

	cantidad := mesas
	if cantidad > 0 {
		if maximo := jugadores / 2; cantidad > maximo {
			aviso = fmt.Sprintf("con %d jugadores no se pueden armar %d mesas: se arman %d", jugadores, cantidad, maximo)
			cantidad = maximo
		}
	} else {
		if porMesa < 2 {
			return nil, "", fmt.Errorf("jugadores_por_mesa debe ser al menos 2, es %d", porMesa)
		}
		cantidad = jugadores / porMesa
		if cantidad == 0 {
			cantidad = 1
		}
		// Los que sobran se reparten de a uno entre las mesas. Si sobran más que
		// mesas hay, no entran como "un jugador de más" y hace falta otra mesa.
		if jugadores-cantidad*porMesa > cantidad {
			cantidad = (jugadores + porMesa) / (porMesa + 1)
		}
	}

	base, resto := jugadores/cantidad, jugadores%cantidad
	tamanos = make([]int, cantidad)
	for i := range tamanos {
		tamanos[i] = base
		if i < resto {
			tamanos[i]++
		}
	}
	if tamanos[0] > MaxJugadoresMesa {
		return nil, "", fmt.Errorf("%d jugadores en %d mesas dejan mesas de %d: el máximo es %d. Hacen falta más mesas",
			jugadores, cantidad, tamanos[0], MaxJugadoresMesa)
	}
	return tamanos, aviso, nil
}

// DescribirMesas resume los tamaños en una frase: "8 mesas de 4" o
// "13 mesas: 3 de 6 y 10 de 5".
func DescribirMesas(tamanos []int) string {
	if len(tamanos) == 0 {
		return ""
	}

	cuantas := map[int]int{}
	for _, t := range tamanos {
		cuantas[t]++
	}
	distintos := make([]int, 0, len(cuantas))
	for t := range cuantas {
		distintos = append(distintos, t)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(distintos)))

	if len(distintos) == 1 {
		t := distintos[0]
		if len(tamanos) == 1 {
			return fmt.Sprintf("1 mesa de %d", t)
		}
		return fmt.Sprintf("%d mesas de %d", len(tamanos), t)
	}

	partes := make([]string, len(distintos))
	for i, t := range distintos {
		partes[i] = fmt.Sprintf("%d de %d", cuantas[t], t)
	}
	ultima := len(partes) - 1
	listado := strings.Join(partes[:ultima], ", ") + " y " + partes[ultima]
	return fmt.Sprintf("%d mesas: %s", len(tamanos), listado)
}

// ClasificanAuto es cuántos pasan de la clasificatoria cuando no se fija el
// número. Las bases dicen "los 16 o 32 con más fichas, según la cantidad de
// participantes" sin decir dónde está el corte; esta es la regla elegida:
//
//   - 64 inscritos o más: pasan 32.
//   - de 32 a 63: pasan 16.
//   - menos: la mayor potencia de dos que no pase de la mitad de los inscritos,
//     con un mínimo de 2. Con eso las pruebas chicas siguen teniendo fase final.
//
// Si el corte real es otro, se fija Fase.Clasifican y esto no se usa.
func ClasificanAuto(inscritos int) int {
	switch {
	case inscritos >= 64:
		return 32
	case inscritos >= 32:
		return 16
	}
	pasan := 2
	for pasan*2 <= inscritos/2 {
		pasan *= 2
	}
	if pasan > inscritos {
		pasan = inscritos
	}
	return pasan
}
