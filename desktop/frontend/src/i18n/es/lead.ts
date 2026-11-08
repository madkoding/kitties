import type { Dictionary } from "../../i18n";

// The lead kitty: the principal one, which orchestrates the others and reports for all of them.
const dict: Dictionary = {
  Lead: "Principal",
  "Lead kitty": "Kitty principal",
  reports: "reportes",
  "{name} is the lead kitty": "{name} es el kitty principal",
  "Lead kitty (orchestrator)": "Kitty principal (orquestador)",
  "It splits what you ask among the other kitties, gathers their answers and gives you one report. Their news (goals done or stuck, help, approvals) reach you through it, in its Reports thread.":
    "Reparte lo que pides entre los demás kitties, junta sus respuestas y te entrega un solo reporte. Sus novedades (metas cumplidas o trabadas, ayuda, aprobaciones) te llegan a través de él, en su hilo Reportes.",
  "{lead} leads the fleet now; making {name} the lead takes it from {lead}.":
    "{lead} es el principal ahora; hacer principal a {name} se lo quita a {lead}.",
  "One kitty can lead the others: you talk to it, it hands the work out and reports for all of them.":
    "Un kitty puede dirigir a los demás: hablas con él, reparte el trabajo y te reporta por todos.",
  "Open reports": "Abrir reportes",
  "Stop leading": "Dejar de ser principal",
  "Make {name} the lead": "Hacer principal a {name}",
  "Ask me before the lead kitty changes another's limits":
    "Pregúntame antes de que el kitty principal cambie los límites de otro",
  "The lead may give a kitty more rounds, tokens or seconds per task (up to 500 rounds, 2M tokens, 2 hours). On: each lasting change waits in Approvals. What it gives for one task alone ends with the task.":
    "El principal puede darle a un kitty más rondas, tokens o segundos por tarea (hasta 500 rondas, 2M de tokens, 2 horas). Activado: cada cambio permanente espera en Aprobaciones. Lo que da para una sola tarea termina con la tarea.",
  "The lead kitty ({name})": "El kitty principal ({name})",
  "Summarize reports": "Resumir reportes",
  "Summarize the reports in this thread since your last summary: what each kitty did, what is stuck or failed, what waits for me, and what you suggest next. Short, in bullets.":
    "Resume los reportes de este hilo desde tu último resumen: qué hizo cada kitty, qué está trabado o falló, qué espera por mí y qué sugieres hacer ahora. Breve, en viñetas.",
  limit: "límite",
  "Hand-overs at the same time": "Encargos al mismo tiempo",
  "When a kitty hands several pieces of a task to others in one reply, this many work at once (1 to 16); the rest wait their turn. Write off for one at a time.":
    "Cuando un kitty reparte varias partes de una tarea en una misma respuesta, esta cantidad trabaja a la vez (1 a 16); el resto espera su turno. Escribe off para una a la vez.",
  "The lead sums up the reports every day at": "El principal resume los reportes cada día a las",
  "At this time the lead kitty writes a short summary in its Reports thread (what each kitty did, what is stuck, what waits for you, what next) and sends it to your notification channel. Empty: never.":
    "A esta hora el kitty principal escribe un resumen corto en su hilo Reportes (qué hizo cada kitty, qué está trabado, qué espera tu respuesta, qué sigue) y lo envía a tu canal de notificaciones. Vacío: nunca.",
};

export default dict;
