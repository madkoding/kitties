import type { Dictionary } from "../../i18n";

// The lead kitty: the principal one, which orchestrates the others and reports for all of them.
const dict: Dictionary = {
  Lead: "Principal",
  "Lead kitty": "Kitty principal",
  reports: "relatórios",
  "{name} is the lead kitty": "{name} é o kitty principal",
  "Lead kitty (orchestrator)": "Kitty principal (orquestrador)",
  "It splits what you ask among the other kitties, gathers their answers and gives you one report. Their news (goals done or stuck, help, approvals) reach you through it, in its Reports thread.":
    "Divide o que você pede entre os outros kitties, junta as respostas e entrega um só relatório. As novidades deles (metas cumpridas ou travadas, ajuda, aprovações) chegam a você por ele, no seu tópico Relatórios.",
  "{lead} leads the fleet now; making {name} the lead takes it from {lead}.":
    "{lead} é o principal agora; tornar {name} o principal tira isso de {lead}.",
  "One kitty can lead the others: you talk to it, it hands the work out and reports for all of them.":
    "Um kitty pode liderar os outros: você fala com ele, ele distribui o trabalho e reporta por todos.",
  "Open reports": "Abrir relatórios",
  "Stop leading": "Deixar de ser principal",
  "Make {name} the lead": "Tornar {name} o principal",
  "Ask me before the lead kitty changes another's limits":
    "Pergunte-me antes que o kitty principal mude os limites de outro",
  "The lead may give a kitty more rounds, tokens or seconds per task (up to 500 rounds, 2M tokens, 2 hours). On: each lasting change waits in Approvals. What it gives for one task alone ends with the task.":
    "O principal pode dar a um kitty mais rodadas, tokens ou segundos por tarefa (até 500 rodadas, 2M de tokens, 2 horas). Ligado: cada mudança permanente espera em Aprovações. O que ele dá para uma só tarefa termina com a tarefa.",
  limit: "limite",
};

export default dict;
