# Plantilla de documentacion de prompts de IA

> Regla del proyecto: **si se usa IA, se documenta el prompt**. Un archivo por
> sesion, nombrado `AAAA-MM-DD-autor-tema.md` en `docs/prompts/`.

---

## Metadatos

- **Autor:**
- **Fecha:**
- **Herramienta y modelo:**
- **Archivos afectados:**
- **Commit relacionado:**

## Objetivo

Que se queria resolver, en una o dos frases.

## Prompt (literal)

```text
Pegar aca el prompt exacto, sin editar.
```

## Salida usada

Que se tomo de la respuesta: todo, un fragmento, solo la idea.

## Verificacion humana

- [ ] Lei linea por linea lo que se incorporo
- [ ] Corri `make lint` y `make test`
- [ ] Agregue o ajuste tests que cubren esto
- [ ] Confirme que respeta las interfaces de `docs/interfaces.md`

## Cambios que le hice a la salida

Lista de correcciones sobre lo generado.
