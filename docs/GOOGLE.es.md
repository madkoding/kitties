# Cuentas de Google

La conexión **Google Workspace** inicia sesión en una cuenta de Google con OAuth (código de
autorización con PKCE): un clic en **Ajustes → Conexiones**, una ventana del navegador para elegir
la cuenta y autorizar, y el refresh token queda en los ajustes del gateway. Funciona con cuentas
personales `@gmail.com` y de Workspace. En inglés: [GOOGLE.md](GOOGLE.md).

Una release sin app de Google incluida pide una app tuya: un cliente OAuth gratuito en un proyecto
tuyo de Google Cloud, que se crea una sola vez en unos cinco minutos.

## 1. Crear el cliente OAuth (una vez)

1. Abre [console.cloud.google.com](https://console.cloud.google.com/) con tu cuenta y crea un
   proyecto (selector de proyectos → **Proyecto nuevo**, por ejemplo "Kitties").
2. **APIs y servicios → Biblioteca**: habilita las APIs de los servicios que vayas a marcar:
   Gmail API, Google Calendar API, Google Drive API, Google Sheets API, Google Docs API,
   People API (contactos) y Google Tasks API.
3. **Google Auth Platform → Desarrollo de la marca** (antes *Pantalla de consentimiento*): nombre
   ("Kitties") y tu correo como soporte y contacto. Guarda.
4. **Público**: tipo de usuario **Externo**. Mientras la app esté en **Prueba**, agrega en
   **Usuarios de prueba** cada cuenta con la que vayas a entrar (tus Gmail personales). Ver
   [Prueba o producción](#prueba-o-producción).
5. **Clientes → Crear cliente**: tipo de aplicación **App de escritorio**, cualquier nombre. Copia
   el **ID de cliente** y el **Secreto del cliente**.

Un cliente de escritorio acepta cualquier dirección `http://127.0.0.1:<puerto>`, así que no hay que
registrar ninguna redirección: Kitties envía `http://127.0.0.1:<puerto del gateway>/v1/oauth/callback`.

## 2. Iniciar sesión desde Kitties

1. **Ajustes → Conexiones → Agregar → Google Workspace**.
2. **Servicios**: desmarca lo que los kitties no deban usar. Kitties solo pide a Google esos
   permisos (tabla de abajo).
3. Pega el **ID de cliente** y el **Secreto** del paso 1.
4. **Iniciar sesión con Google**: se abre el selector de cuentas de Google. Elige la cuenta,
   autoriza y la página dice *Connected*. La conexión muestra el correo de la cuenta.

### Varias cuentas

Cada conexión es una cuenta. Para otra, agrega otra conexión **Google Workspace** (por ejemplo
`google-trabajo`) e inicia sesión: el selector te deja elegir la otra cuenta. Cada kitty usa las
conexiones que marques en *Qué kitties pueden usarla*. Volver a iniciar sesión en una conexión
sugiere su propia cuenta (`login_hint`).

### Una app para todas las conexiones

En vez de pegar el cliente en cada conexión, ponlo una vez en el gateway:
`KITTIES_GOOGLE_CLIENT_ID` y `KITTIES_GOOGLE_CLIENT_SECRET`. También puede ir en el build:
`make OAUTH_APPS="google=<client id>:<client secret>" dist` (la release lee el secreto
`KITTIES_OAUTH_APPS` del repositorio).

### Gemini con la misma app

Con esa app en el gateway, un modelo Gemini también inicia sesión con tu cuenta de Google (Inicio
rápido o **Ajustes → Modelos → Google Gemini → Cuenta de Google → Iniciar sesión con Google**), sin
gcloud: escribe el proyecto de Cloud al que se cobra Gemini e inicia sesión. Activa la **Generative
Language API** en ese proyecto. Pide los mismos permisos que gcloud (`cloud-platform`,
`generative-language.retriever`), que Google considera sensibles: deja la app en Prueba con tus
cuentas como usuarios de prueba, o verifícala. Un navegador en otra máquina termina en una página de
`127.0.0.1` que no carga: copia su dirección de vuelta en Kitties, como en las conexiones. Sin la
app, *Usar mi sesión de gcloud* sigue usando `gcloud auth application-default login`.

## Permisos (scopes)

Siempre `openid email` (para mostrar qué cuenta entró) y, por cada servicio marcado, el permiso
más estrecho que necesitan sus acciones:

| Servicio | Scope | Permite |
| --- | --- | --- |
| Gmail | `gmail.readonly`, `gmail.send` | leer y buscar correo, enviar (no borrar) |
| Calendar | `calendar.events` | listar y crear eventos |
| Drive | `drive.file` | solo los archivos que crea Kitties |
| Sheets | `spreadsheets` | agregar filas a una hoja que indiques |
| Docs | `documents` | crear documentos |
| Contacts | `contacts` | buscar y agregar contactos |
| Tasks | `tasks` | listar, agregar y completar tareas |

Si en la pantalla de Google desmarcas un permiso, la conexión se queda solo con los servicios
autorizados.

## Prueba o producción

- **Prueba**: solo entran los usuarios de prueba y Google cierra su sesión a los **7 días**.
  Kitties dice entonces *the sign-in expired or was revoked* y la conexión pide volver a entrar.
- **En producción** (Público → **Publicar app**): sin límite de 7 días. Sin verificación, Google
  avisa *Google no verificó esta app*; para tu propia app y cuenta elige **Configuración avanzada →
  Ir a Kitties (no seguro)**. Las apps sin verificar admiten hasta 100 usuarios, suficiente para uso
  personal. La verificación solo hace falta para ofrecer la app a otras personas.

## Gateway en otra máquina

Google solo devuelve el navegador a `127.0.0.1` (clientes de escritorio) o a una dirección `https`
(clientes web). Kitties rechaza, antes de abrir Google, un inicio de sesión que volvería a una IP
de la LAN o a un nombre en `http` como `http://192.168.1.20:7488`. Opciones:

- inicia sesión desde el equipo donde corre el gateway (la app de escritorio lo hace así), o
- dale al gateway una dirección pública `https` (`KITTIES_PUBLIC_URL`, ver [DEPLOY.md](DEPLOY.md)),
  crea un cliente **Aplicación web** y agrega `https://<tu dirección>/v1/oauth/callback` en
  *URIs de redireccionamiento autorizados*.

## Si falla

| Google dice | Solución |
| --- | --- |
| `access_denied` / *no completó el proceso de verificación* | la app está en Prueba y la cuenta no es usuario de prueba: agrégala (paso 1.4) |
| `redirect_uri_mismatch` | cliente web sin el callback de este gateway: agrega la dirección que muestra Ajustes, o usa un cliente de escritorio |
| `invalid_client` | ID o secreto incorrectos, o el cliente se borró |
| 403 *… API has not been used in project …* | habilita esa API en el proyecto (paso 1.2) |
| *the sign-in expired or was revoked* | pasaron los 7 días de Prueba, o se quitó el acceso en myaccount.google.com/permissions: vuelve a entrar |
