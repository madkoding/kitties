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
};

export default dict;
