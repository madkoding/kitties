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
};

export default dict;
