package campeonato

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// Inscrito es un bot anotado en el campeonato.
//
// Las bases dicen que cada equipo recibe "la documentación de la API, sus
// credenciales y un kit de inicio": las credenciales son el Token. Es lo único
// que identifica al bot ante la arena, así que es secreto: quien lo conozca
// puede jugar en su lugar.
type Inscrito struct {
	// ID identifica la inscripción dentro de la organización. Nunca se le
	// muestra a ningún bot: los bots solo ven un alias aleatorio (ver Estado).
	ID string `json:"id"`
	// Equipo y Bot son los nombres con que se inscribieron. Solo los ve la
	// organización.
	Equipo string `json:"equipo"`
	Bot    string `json:"bot"`
	// Token es la credencial secreta del bot.
	Token string `json:"token"`

	// Comando y Directorio permiten que la arena lance el bot por su cuenta.
	// Es para las pruebas en local con los bots de ejemplo: en el evento los
	// bots corren en la máquina de cada equipo y se conectan solos.
	Comando    []string `json:"comando,omitempty"`
	Directorio string   `json:"directorio,omitempty"`
}

// Inscritos es la lista de bots anotados.
type Inscritos []Inscrito

// NuevoToken genera una credencial. Se muestra una sola vez al equipo y la
// arena la compara tal cual, así que tiene que ser larga e impredecible.
func NuevoToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("campeonato: no se pudo generar una credencial: " + err.Error())
	}
	return "pfb-" + hex.EncodeToString(b[:])
}

// Validar verifica que la lista sirva para jugar: identificadores y
// credenciales únicos, nombres presentes y no más de dos bots por equipo.
func (l Inscritos) Validar() error {
	ids := make(map[string]bool, len(l))
	tokens := make(map[string]string, len(l))
	porEquipo := make(map[string]int)

	for i, in := range l {
		posicion := fmt.Sprintf("inscrito #%d", i+1)
		switch {
		case strings.TrimSpace(in.ID) == "":
			return fmt.Errorf("campeonato: %s no tiene id", posicion)
		case strings.TrimSpace(in.Equipo) == "":
			return fmt.Errorf("campeonato: %s (%s) no tiene equipo", posicion, in.ID)
		case strings.TrimSpace(in.Bot) == "":
			return fmt.Errorf("campeonato: %s (%s) no tiene nombre de bot", posicion, in.ID)
		case strings.TrimSpace(in.Token) == "":
			return fmt.Errorf("campeonato: %s (%s) no tiene token", posicion, in.ID)
		case ids[in.ID]:
			return fmt.Errorf("campeonato: el id %q está repetido", in.ID)
		}
		// Dos bots con el mismo token serían el mismo jugador para el lobby: el
		// segundo en conectarse le quitaría la conexión al primero.
		if otro, repetido := tokens[in.Token]; repetido {
			return fmt.Errorf("campeonato: %q y %q comparten token", otro, in.ID)
		}
		ids[in.ID] = true
		tokens[in.Token] = in.ID

		clave := strings.ToLower(strings.TrimSpace(in.Equipo))
		porEquipo[clave]++
		if porEquipo[clave] > MaxBotsPorEquipo {
			return fmt.Errorf("campeonato: el equipo %q tiene más de %d bots (las bases permiten hasta %d)",
				in.Equipo, MaxBotsPorEquipo, MaxBotsPorEquipo)
		}
	}
	return nil
}

// Agregar inscribe un bot nuevo con una credencial recién generada. Si `bot`
// está vacío, se llama "bot1", "bot2"... según cuántos tenga ya el equipo.
func (l *Inscritos) Agregar(equipo, bot string) (Inscrito, error) {
	equipo = strings.TrimSpace(equipo)
	bot = strings.TrimSpace(bot)
	if equipo == "" {
		return Inscrito{}, errors.New("campeonato: el equipo no puede estar vacío")
	}

	del := 0
	for _, in := range *l {
		if strings.EqualFold(in.Equipo, equipo) {
			del++
		}
	}
	if bot == "" {
		bot = fmt.Sprintf("bot%d", del+1)
	}

	nuevo := Inscrito{ID: l.idLibre(equipo, bot), Equipo: equipo, Bot: bot, Token: NuevoToken()}
	candidata := append(append(Inscritos(nil), *l...), nuevo)
	if err := candidata.Validar(); err != nil {
		return Inscrito{}, err
	}
	*l = candidata
	return nuevo, nil
}

// idLibre arma un id legible a partir del equipo y el bot, sin repetir uno
// existente.
func (l Inscritos) idLibre(equipo, bot string) string {
	base := slug(equipo) + "-" + slug(bot)
	existe := func(id string) bool {
		for _, in := range l {
			if in.ID == id {
				return true
			}
		}
		return false
	}
	id := base
	for n := 2; existe(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// slug deja solo minúsculas, números y guiones.
func slug(s string) string {
	var b strings.Builder
	guion := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII, unicode.IsDigit(r):
			b.WriteRune(r)
			guion = false
		case !guion && b.Len() > 0:
			b.WriteByte('-')
			guion = true
		}
	}
	if resultado := strings.Trim(b.String(), "-"); resultado != "" {
		return resultado
	}
	return "x"
}

// ImportarCSV inscribe los bots de un CSV con las columnas `equipo,bot`
// (encabezado opcional; `bot` puede quedar vacío). Es la salida natural del
// formulario de inscripción. Si alguna fila es inválida no se agrega nada.
func (l *Inscritos) ImportarCSV(origen io.Reader) ([]Inscrito, error) {
	lector := csv.NewReader(origen)
	lector.FieldsPerRecord = -1
	lector.TrimLeadingSpace = true

	filas, err := lector.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("campeonato: el CSV no se pudo leer: %w", err)
	}

	candidata := append(Inscritos(nil), *l...)
	var agregados []Inscrito
	for i, fila := range filas {
		if len(fila) == 0 || (len(fila) == 1 && strings.TrimSpace(fila[0]) == "") {
			continue
		}
		equipo := strings.TrimSpace(fila[0])
		bot := ""
		if len(fila) > 1 {
			bot = fila[1]
		}
		// Encabezado.
		if i == 0 && strings.EqualFold(equipo, "equipo") {
			continue
		}
		nuevo, err := candidata.Agregar(equipo, bot)
		if err != nil {
			return nil, fmt.Errorf("campeonato: fila %d del CSV: %w", i+1, err)
		}
		agregados = append(agregados, nuevo)
	}

	*l = candidata
	return agregados, nil
}

// CargarInscritos lee la lista de un archivo JSON y la valida.
func CargarInscritos(ruta string) (Inscritos, error) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("campeonato: no se pudo leer %s: %w", ruta, err)
	}
	var lista Inscritos
	if err := json.Unmarshal(datos, &lista); err != nil {
		return nil, fmt.Errorf("campeonato: %s no es una lista de inscritos válida: %w", ruta, err)
	}
	if err := lista.Validar(); err != nil {
		return nil, err
	}
	return lista, nil
}

// Guardar escribe la lista. Se guarda con permisos 0600 porque tiene las
// credenciales de todos los equipos.
func (l Inscritos) Guardar(ruta string) error {
	datos, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ruta, append(datos, '\n'), 0o600)
}
