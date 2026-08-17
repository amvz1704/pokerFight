// Este archivo contiene el alta y versionado de bots por cuenta. Ver
// casino.go para los tipos y la interfaz Casino.
package casino

import (
	"fmt"
	"time"
)

// RegistrarBot da de alta una nueva versión de un bot para una cuenta. No
// reemplaza versiones anteriores: quedan todas en ListarBots, en el orden en
// que se subieron, para poder auditar con qué versión jugó cada partida.
func (m *motor) RegistrarBot(idCuenta, nombre, version string) (Bot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, existe := m.datos.Cuentas[idCuenta]; !existe {
		return Bot{}, fmt.Errorf("casino: no existe la cuenta %q", idCuenta)
	}
	if nombre == "" {
		return Bot{}, fmt.Errorf("casino: el bot necesita un nombre")
	}
	if version == "" {
		return Bot{}, fmt.Errorf("casino: el bot necesita una versión")
	}

	bot := Bot{
		ID:       nuevoID("bot"),
		IDCuenta: idCuenta,
		Nombre:   nombre,
		Version:  version,
		Creado:   time.Now().UTC(),
	}
	m.datos.Bots[idCuenta] = append(m.datos.Bots[idCuenta], bot)

	if err := m.guardarSinLock(); err != nil {
		bots := m.datos.Bots[idCuenta]
		m.datos.Bots[idCuenta] = bots[:len(bots)-1]
		return Bot{}, fmt.Errorf("casino: no se pudo guardar el bot nuevo: %w", err)
	}
	return bot, nil
}

// ListarBots devuelve las versiones de bots subidas por una cuenta, en el
// orden en que se registraron (la más reciente al final).
func (m *motor) ListarBots(idCuenta string) ([]Bot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, existe := m.datos.Cuentas[idCuenta]; !existe {
		return nil, fmt.Errorf("casino: no existe la cuenta %q", idCuenta)
	}
	return append([]Bot(nil), m.datos.Bots[idCuenta]...), nil
}
