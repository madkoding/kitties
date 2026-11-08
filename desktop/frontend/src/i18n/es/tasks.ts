import type { Dictionary } from "../../i18n";

// The scheduled tasks (their own section, with their history of runs) and the threads between
// two kitties of a hand-over, which go a while after the work ends unless they are kept.
const dict: Dictionary = {
  "Scheduled tasks": "Tareas programadas",
  "No scheduled tasks yet.": "Aún no hay tareas programadas.",
  "No scheduled tasks yet: {name} only works when asked.":
    "Aún no hay tareas programadas: {name} solo trabaja cuando se lo piden.",
  "No scheduled tasks yet. Pick a kitty to give it one.":
    "Aún no hay tareas programadas. Elige un kitty para darle una.",
  "New scheduled task": "Nueva tarea programada",
  "Add scheduled task": "Añadir tarea programada",
  "See scheduled tasks": "Ver tareas programadas",
  "What the kitties do on their own, on a schedule or when a service reports something. Each task keeps its history of runs here, out of your chats.":
    "Lo que los kitties hacen por su cuenta, según un horario o cuando un servicio avisa algo. Cada tarea guarda aquí su historial de ejecuciones, fuera de tus chats.",
  "all {count}": "todas ({count})",
  "on events": "con eventos",
  history: "historial",
  "It has not run yet.": "Todavía no se ha ejecutado.",
  "Open every run in full": "Abrir todas las ejecuciones completas",
  "Every run of the scheduled task “{title}”. It is not a chat: it stays out of the sidebar.":
    "Todas las ejecuciones de la tarea programada “{title}”. No es un chat: queda fuera de la barra lateral.",
  "See its history": "Ver su historial",
  "{from} handed {to} a subtask here; the answer shows in the chat that asked.":
    "{from} le pasó aquí una subtarea a {to}; la respuesta se ve en el chat que la pidió.",
  "A hand-over between two kitties; the answer shows in the chat that asked.":
    "Una delegación entre dos kitties; la respuesta se ve en el chat que la pidió.",
  "Kept: it stays among your chats.": "Conservado: queda entre tus chats.",
  "Deleted after {time} unless you keep it.": "Se borra después de {time} si no lo conservas.",
  "Deleted a while after the work ends, unless you keep it.":
    "Se borra un tiempo después de terminar el trabajo, si no lo conservas.",
  Keep: "Conservar",
  "Let it go": "Dejar que se borre",
  kept: "conservado",
  "Kept: a hand-over between two kitties": "Conservado: una delegación entre dos kitties",
  "Keep the threads between two kitties for": "Conservar los hilos entre dos kitties durante",
  "When a kitty hands a subtask to another, the answer shows in the chat that asked; the thread between the two is deleted this long after the work ends (1h to 2160h), unless you keep it. Write off to keep them all.":
    "Cuando un kitty le pasa una subtarea a otro, la respuesta se ve en el chat que la pidió; el hilo entre los dos se borra este tiempo después de terminar (1h a 2160h), salvo que lo conserves. Escribe off para conservarlos todos.",
  "Last run": "Última ejecución",
  "No scheduled task matches.": "Ninguna tarea programada coincide.",
  Reports: "Reportes",
  all: "todas",
  "need a look": "a revisar",
  "{count} to look at": "{count} a revisar",
  "Scheduled tasks whose last run was blocked, escalated or failed":
    "Tareas programadas cuya última ejecución quedó bloqueada, escalada o falló",
};

export default dict;
