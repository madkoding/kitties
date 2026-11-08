# Límites

Kitties no limita los tokens por defecto, así que un documento grande se lee completo. Lo que aún
puede frenar una petición viene del modelo, no de Kitties.

## Quitados (de Kitties)

| Antes | Ahora |
| --- | --- |
| Texto de un archivo cortado a 30.000 caracteres (60.000 por mensaje) | Completo, hasta la mitad de la ventana de contexto del modelo; un archivo más largo conserva su inicio tal cual y el resto se lee por partes y el modelo lo condensa (una vez, guardado junto al archivo) |
| 6 archivos y 20 MB por mensaje; 2 MB un texto, 10 MB un PDF | 20 archivos y 100 MB; 25 MB un texto, 32 MB un PDF, 50 MB otros |
| Respuestas de 4096 tokens | 16384 (`max_tokens` del perfil) |
| Presupuesto de tokens diario y por tarea | `0` (sin límite) por defecto; `limits.day_tokens` / `limits.max_tokens` los fijan si quieres |
| Conversación cortada a un número fijo de turnos | Ajustada a la ventana del modelo (cuentan imágenes y documentos); se sueltan primero los más antiguos |
| 4 horas por tarea | Sin límite de tiempo por defecto; `limits.max_seconds` lo fija |
| Rondas con tope (8 ventanas en el escritorio, 40 rondas de conectores por respuesta) | Sin tope; `limits.max_rounds` es el tamaño de la ventana de punto de control; la detección de pantalla atascada sigue avisando a quien se repite |

`limits.attended: true` es el modo cauto opcional: se detiene al final de sus rondas y espera un «continúa», sin tope propio de tokens ni de tiempo.

## No quitables (del modelo)

- **Ventana de contexto.** `context_tokens` del perfil; por defecto la habitual del proveedor
  (Gemini 1.000.000; Ollama 32.768; Groq, DeepSeek, Qwen, Mistral 128.000; el resto 200.000). Pon la
  ventana real de tu modelo: Kitties ajusta archivos y conversaciones a ella en vez de fallar.
- **Tamaño de la respuesta.** Lo máximo que puede escribir una respuesta; Anthropic exige el valor.
  Sube `max_tokens` si el modelo permite más, bájalo si permite menos (rechazaría la petición).
- **Imágenes (5 MB) y PDF (32 MB).** Límites de tamaño de los propios proveedores.
- **Tamaño de la petición.** Las capturas van al modelo en JPEG cuando pesan menos (mismo tamaño). Una petición lleva como imagen las últimas 6 a 11 capturas y como
  mucho 12 MB entre imágenes y documentos; las anteriores se nombran en una línea. Si el proveedor
  igual responde «failed to read request body» (demasiado grande, o cortada por un proxy), se
  reenvía solo con las 2 últimas, y el chat lo dice en palabras simples.
- **La cuota de tu proveedor.** Los límites de ritmo y el uso del plan son del proveedor.
