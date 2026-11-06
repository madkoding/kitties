# WhatsApp: un número propio para los kitties

[In English](WHATSAPP.md)

Kitties puede vincularse a un número de WhatsApp como lo hace WhatsApp Web: como **dispositivo
vinculado** de un teléfono, con un código QR que aparece en Ajustes. Desde ahí, las personas que
pongas en la lista escriben a ese número y uno de tus kitties les responde, y los kitties pueden
escribirles con el conector `whatsapp-web`.

![WhatsApp en Ajustes, esperando el código QR](screenshots/60-settings-whatsapp.png)

> [!WARNING]
> **No es una API oficial.** Los términos de servicio de WhatsApp no permiten clientes no oficiales
> ni automatizados, y WhatsApp puede bloquear un número usado así, por un tiempo o para siempre.
> Usa un **número de repuesto** (otra SIM, una línea prepago), nunca el tuyo, y mantén un uso bajo y
> personal (nada de envíos masivos ni a desconocidos).
>
> **La alternativa oficial** es la [WhatsApp Business Cloud API](https://developers.facebook.com/docs/whatsapp/cloud-api)
> de Meta: la conexión `whatsapp` del catálogo (Ajustes, Conexiones, Agregar una conexión,
> WhatsApp) envía mensajes con ella. Necesita una cuenta de Meta Business y un número registrado
> allí, y fuera de una ventana de 24 horas solo se pueden mandar plantillas aprobadas; a cambio está
> permitida y no se bloquea por ser automática.

## Activarlo

1. Ajustes, **Conexiones**, la sección **WhatsApp (dispositivo vinculado)** al final.
2. Elige **el kitty que responde** y agrega los **números que pueden escribir** con su código de
   país (`+56 9 1234 5678`). La lista parte vacía: hasta que agregues a alguien, se ignora todo
   mensaje. Marca **Responder en grupos cuando alguien de la lista nombra al kitty** para que también
   hable en grupos.
3. Marca **Activar WhatsApp**, luego **Entiendo que WhatsApp puede bloquear este número**, y
   **Guardar**.
4. Aparece un código QR. En el teléfono del número de los kitties: WhatsApp, Ajustes,
   **Dispositivos vinculados**, **Vincular un dispositivo**, y apunta al código. El código cambia
   cada pocos segundos; si se acaban, **Mostrar un código nuevo**.
5. Cuando diga **Conectado · +número**, **Enviar una prueba** escribe a un número de la lista.

El teléfono debe seguir encendido y conectarse de vez en cuando (WhatsApp desvincula un dispositivo
cuyo teléfono lleva unas dos semanas apagado). **Desvincular** quita el dispositivo del teléfono y
olvida la sesión.

## Cómo funciona

- **Un chat por número.** El primer mensaje de un número abre un chat `WhatsApp: <nombre>` con el
  kitty elegido; los siguientes van al mismo chat, así que también ves la conversación en la página.
  La respuesta vuelve a WhatsApp con su Markdown convertido a las marcas de WhatsApp (`*negrita*`,
  `_cursiva_`). Una tarea en el escritorio se sigue: las primeras palabras van al tiro y el resultado
  cuando termina.
- **"escribiendo…" mientras piensa.** El chat del teléfono muestra al kitty escribiendo hasta que
  sale la respuesta. Un mensaje que llega mientras sigue ocupado recibe un aviso corto en el idioma
  del kitty (el de su alma, o si no el del mensaje: inglés, español o portugués).
- **Grupos, cuando lo nombran.** Con grupos activado, un mensaje de grupo se responde solo si quien
  escribe está en la lista y menciona con @ al número de los kitties, responde a uno de sus
  mensajes o nombra al kitty. Cada grupo tiene su chat (`WhatsApp group …`), con cada línea firmada
  por quien la escribió; la respuesta va al grupo.
- **Fotos, videos, documentos y audios.** Le llegan al kitty como archivos del chat (hasta 25 MB
  cada uno), con su pie de foto como texto. Una **nota de voz** se pasa a texto con la API de voz
  (Whisper) de un perfil `openai` o `groq` con llave; sin uno llega como archivo de audio. Stickers,
  ubicaciones y contactos no se responden.
- **La lista funciona en ambos sentidos.** Un mensaje de un número que no está se ignora (y se
  cuenta en Ajustes) sin respuesta; los kitties solo pueden escribir a números de la lista.
- **Los kitties escriben con el conector.** Mientras el vínculo está conectado, `whatsapp-web` se
  ofrece a los kitties (los que tienen su lista de conexiones vacía o lo nombran):

  ```
  @connector whatsapp-web {"action": "send", "to": "+56912345678", "text": "El informe está listo."}
  @connector whatsapp-web {"action": "status"}
  ```

  Sin `to` escribe a quien escribió último (o al único número de la lista). Un envío llega a otras
  personas, así que como todo `send` **queda retenido para tu aprobación** hasta que confíes en el
  kitty para eso ([AUTONOMY.md](AUTONOMY.md)). Las respuestas a quien escribió no se retienen: lo
  pidió.

## Desde el archivo

La sección `whatsapp:` de `kitties.yaml` define lo mismo mientras no se haya guardado nada desde
Ajustes (en un servidor, antes de abrir la página). Después Ajustes guarda lo suyo en
`settings.json`.

```yaml
whatsapp:
  enabled: true                  # activarlo aquí es aceptar el riesgo
  allow: ["+56 9 1234 5678"]
  kitty: mimi                    # vacío: el primer kitty
  groups: false
  transcribe: ""                 # el perfil openai o groq para notas de voz; vacío: el primero; off: ninguno
```

El código QR se sigue escaneando desde Ajustes, o se lee con `GET /v1/whatsapp` (`qr_png`).

## Dónde se guarda cada cosa

| Qué | Dónde | Cómo |
| --- | --- | --- |
| Encendido o no, la lista, el kitty, el chat de cada número | `<data>/whatsapp/settings.json` | modo 0600, sin secretos |
| La sesión del dispositivo vinculado (sus llaves, las sesiones Signal, los contactos) | `<data>/whatsapp/session.bin` | **sellada** con la llave del vault (AES-256-GCM, atada a su uso); nunca en claro |

La sesión es el estado de la librería guardado en memoria y sellado entero: el gateway no usa un
archivo SQLite ni otro almacenamiento plano. La llave es la del vault (`vault.key` al lado, o
`KITTIES_VAULT_KEY`); sin vault (una carpeta de datos de solo lectura) WhatsApp no se puede activar
y Ajustes dice por qué. Una sesión que no abre con la llave (la llave cambió) no se usa: se vincula
el teléfono de nuevo con otro código.

## La API

| Ruta | Qué hace |
| --- | --- |
| `GET /v1/whatsapp` | El vínculo: `enabled`, `allow`, `kitty`, `state` (`off`, `starting`, `qr`, `qr_expired`, `connected`, `disconnected`, `logged_out`, `error`), `qr` y `qr_png` (un data URL PNG) mientras espera, `phone`, `linked`, `ignored` |
| `PUT /v1/whatsapp` | `{"enabled", "allow", "kitty", "groups"}`; activarlo necesita `"accept_risk": true` |
| `POST /v1/whatsapp/link` | Códigos QR nuevos (si expiraron o el teléfono quitó el dispositivo) |
| `POST /v1/whatsapp/unlink` | Quita el dispositivo del teléfono y olvida la sesión |
| `POST /v1/whatsapp/test` | `{"to": "+56912345678"}` manda un mensaje corto a un número de la lista |

## Por dentro

El protocolo es [whatsmeow](https://github.com/tulir/whatsmeow) (MPL-2.0), el protocolo multi-
dispositivo de WhatsApp Web en Go. `internal/whatsapp` es el puente (ajustes, lista, chats, conector)
y no sabe nada del protocolo (`transcribe.go` manda las notas de voz a `/audio/transcriptions`);
`internal/whatsapp/wameow` es la conexión, las descargas de archivos y el almacenamiento en memoria
que se exporta, se sella y se vuelve a leer. El puente se prueba con un cliente falso; el
almacenamiento y la conexión, con un falso del cliente de whatsmeow.
