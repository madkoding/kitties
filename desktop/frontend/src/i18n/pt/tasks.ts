import type { Dictionary } from "../../i18n";

// The scheduled tasks (their own section, with their history of runs) and the threads between
// two kitties of a hand-over, which go a while after the work ends unless they are kept.
const dict: Dictionary = {
  "Scheduled tasks": "Tarefas agendadas",
  "No scheduled tasks yet.": "Ainda não há tarefas agendadas.",
  "No scheduled tasks yet: {name} only works when asked.":
    "Ainda não há tarefas agendadas: {name} só trabalha quando pedem.",
  "No scheduled tasks yet. Pick a kitty to give it one.":
    "Ainda não há tarefas agendadas. Escolha um kitty para dar uma a ele.",
  "New scheduled task": "Nova tarefa agendada",
  "Add scheduled task": "Adicionar tarefa agendada",
  "See scheduled tasks": "Ver tarefas agendadas",
  "What the kitties do on their own, on a schedule or when a service reports something. Each task keeps its history of runs here, out of your chats.":
    "O que os kitties fazem sozinhos, num horário ou quando um serviço avisa algo. Cada tarefa guarda aqui seu histórico de execuções, fora dos seus chats.",
  "all {count}": "todas ({count})",
  "on events": "com eventos",
  history: "histórico",
  "It has not run yet.": "Ainda não foi executada.",
  "Open every run in full": "Abrir todas as execuções completas",
  "Every run of the scheduled task “{title}”. It is not a chat: it stays out of the sidebar.":
    "Todas as execuções da tarefa agendada “{title}”. Não é um chat: fica fora da barra lateral.",
  "See its history": "Ver seu histórico",
  "{from} handed {to} a subtask here; the answer shows in the chat that asked.":
    "{from} passou aqui uma subtarefa para {to}; a resposta aparece no chat que pediu.",
  "A hand-over between two kitties; the answer shows in the chat that asked.":
    "Uma delegação entre dois kitties; a resposta aparece no chat que pediu.",
  "Kept: it stays among your chats.": "Mantido: fica entre os seus chats.",
  "Deleted after {time} unless you keep it.": "Apagado depois de {time} se você não o mantiver.",
  "Deleted a while after the work ends, unless you keep it.":
    "Apagado um tempo depois de o trabalho terminar, se você não o mantiver.",
  Keep: "Manter",
  "Let it go": "Deixar apagar",
  kept: "mantido",
  "Kept: a hand-over between two kitties": "Mantido: uma delegação entre dois kitties",
  "Keep the threads between two kitties for": "Manter as conversas entre dois kitties por",
  "When a kitty hands a subtask to another, the answer shows in the chat that asked; the thread between the two is deleted this long after the work ends (1h to 2160h), unless you keep it. Write off to keep them all.":
    "Quando um kitty passa uma subtarefa para outro, a resposta aparece no chat que pediu; a conversa entre os dois é apagada esse tempo depois de terminar (1h a 2160h), a menos que você a mantenha. Escreva off para manter todas.",
};

export default dict;
