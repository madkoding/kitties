import type { Dictionary } from "../../i18n";

// The page's frame: sidebar, header, welcome, composer, sign-in, confirmations and the new chat,
// new kitty, kitty and gateway dialogs.
const dict: Dictionary = {
  // Required marks
  Required: "Obrigatório",

  // Sign-in
  "sign in to this gateway": "entre neste gateway",
  Token: "Token",
  "the gateway's token": "o token do gateway",
  "Checking…": "Verificando…",
  "Sign in": "Entrar",
  "the token": "o token",
  "The token stays on the server: this browser keeps only a session that you can end with Sign out.":
    "O token fica no servidor: este navegador guarda só uma sessão, que você pode encerrar com Sair.",

  // Composer
  "A kitty is working on its desktop…": "Um kitty está trabalhando na área de trabalho dele…",
  "Say something to the team; @name picks who answers":
    "Escreva algo para a equipe; @nome escolhe quem responde",
  "Say something…": "Escreva algo…",
  Stop: "Parar",
  // Attachments
  "A message takes at most {n} files.": "Uma mensagem aceita no máximo {n} arquivos.",
  "{name} is empty.": "{name} está vazio.",
  "{name} is {size}; the most for this kind of file is {max}.":
    "{name} tem {size}; o máximo para este tipo de arquivo é {max}.",
  "The files add up to more than {max}.": "Os arquivos somam mais de {max}.",
  "Remove {name}": "Remover {name}",
  "Download {name}": "Baixar {name}",
  "Open {name}": "Abrir {name}",
  "Drop the files to attach them": "Solte os arquivos para anexá-los",
  "Attach files": "Anexar arquivos",
  Send: "Enviar",

  // Welcome
  "Give your kitties a brain": "Dê um cérebro aos seus kitties",
  "Add a model from a provider you have an account with (Claude, OpenAI, Gemini…), or Ollama to run free models on this computer.":
    "Adicione um modelo de um provedor onde você tem conta (Claude, OpenAI, Gemini…), ou o Ollama para usar modelos grátis neste computador.",
  "Add a model": "Adicionar um modelo",
  "Create your first kitty": "Crie seu primeiro kitty",
  "Give it a name, a personality and a look. It starts working at once.":
    "Dê a ele um nome, uma personalidade e uma aparência. Ele começa a trabalhar na hora.",
  "Create a kitty": "Criar um kitty",
  "Start a chat": "Começar um chat",
  "Ask it anything, or give it a task. Several kitties in one chat work as a team.":
    "Pergunte o que quiser ou dê uma tarefa. Vários kitties no mesmo chat trabalham em equipe.",
  "Welcome to Kitties": "Boas-vindas ao Kitties",
  "No chat open": "Nenhum chat aberto",
  "One step to get your kitties working.":
    "Falta um passo para seus kitties começarem a trabalhar.",
  "{count} steps to get your kitties working.":
    "Faltam {count} passos para seus kitties começarem a trabalhar.",
  "Pick a chat on the left, or start a new one. In a group, write @name to pick who answers.":
    "Escolha um chat à esquerda ou comece um novo. Em um grupo, escreva @nome para escolher quem responde.",
  "New chat": "Novo chat",
  "Connect a service": "Conectar um serviço",
  "Get alerts on your phone": "Receba alertas no celular",

  // Sidebar
  Chats: "Chats",
  "{count} kitties": "{count} kitties",
  "See {name}'s hand-over threads": "Ver as conversas de passagem de {name}",
  "Bring {name} back to this chat": "Trazer {name} de volta a este chat",
  "Subtasks handed around the team": "Subtarefas passadas na equipe",
  "Handed out": "Passadas",
  "Taken on": "Recebidas",
  Failed: "Falharam",
  Kitty: "Kitty",
  "Take {name} off this chat": "Tirar {name} deste chat",
  "Take {name} off this chat? Its answers stay; it shows again if it is handed work here.":
    "Tirar {name} deste chat? As respostas ficam; volta a aparecer se receber trabalho aqui.",
  "Take it off": "Tirar",
  "Make {name} a member: it answers here too": "Tornar {name} membro: também responde aqui",
  "Make {name} a member of this chat? It will answer what you write here too.":
    "Tornar {name} membro deste chat? Também vai responder o que você escrever aqui.",
  "Make it a member": "Tornar membro",
  groups: "grupos",
  "Show every chat": "Mostrar todos os chats",
  "Show only the chats with more than one kitty": "Mostrar só os chats com mais de um kitty",
  "No group chats yet.": "Ainda não há chats em grupo.",
  "Show the whole answer": "Ver a resposta completa",
  "{name} is working on it…": "{name} está trabalhando nisso…",
  "Follow it in the thread between {from} and {name}":
    "Acompanhe na conversa entre {from} e {name}",
  "{name} helps in this chat: a subtask was handed to it":
    "{name} ajuda neste chat: recebeu uma subtarefa",
  "{from} asked: {task}": "{from} pediu: {task}",
  "Could not do it: {error}": "Não conseguiu: {error}",
  "Open the thread between {from} and {name}": "Abrir a conversa entre {from} e {name}",
  "You: {text}": "Você: {text}",
  "No chats yet.": "Ainda não há chats.",
  help: "ajuda",
  goal: "objetivo",
  Kitties: "Kitties",
  "No kitties yet.": "Ainda não há kitties.",
  "New kitty": "Novo kitty",
  Approvals: "Aprovações",
  "{count} waiting": "{count} esperando",
  Library: "Biblioteca",
  Settings: "Configurações",
  Theme: "Tema",
  "Sign out": "Sair",

  // Header and banners
  You: "Você",
  "Something broke in the page: {message}": "Algo quebrou na página: {message}",
  "Something failed: {message}": "Algo deu errado: {message}",
  'Delete the chat "{title}"?': "Excluir o chat “{title}”?",
  Menu: "Menu",
  group: "grupo",
  working: "trabalhando",
  offline: "offline",
  connected: "conectado",
  "Delete chat": "Excluir chat",
  Dismiss: "Dispensar",
  "The gateway did not answer or refused the token.": "O gateway não respondeu ou recusou o token.",

  // New chat
  "A chat is a conversation with your kitties, and there is none yet. Create a kitty first, then chat with it.":
    "Um chat é uma conversa com seus kitties, e você ainda não tem nenhum. Primeiro crie um kitty e depois converse com ele.",
  "Pick one kitty, or several for a group that works as a team.":
    "Escolha um kitty, ou vários para um grupo que trabalha em equipe.",
  "Title (optional)": "Título (opcional)",
  "Launch planning": "Planejar o lançamento",
  "Start group chat": "Começar chat em grupo",
  "Start chat": "Começar chat",
  "a kitty": "um kitty",

  // New kitty
  "A kitty needs a brain to think with, and there is no model yet. Add one in Settings (it takes a minute: pick a provider and paste its key), then come back.":
    "Um kitty precisa de um cérebro para pensar, e ainda não há nenhum modelo. Adicione um em Configurações (leva um minuto: escolha um provedor e cole a chave dele) e depois volte.",
  "{name} was created, but {why}": "{name} foi criado, mas {why}",
  Name: "Nome",
  "There is already a kitty called ": "Já existe um kitty chamado ",
  ": pick another name.": ": escolha outro nome.",
  "It will be called ": "Ele vai se chamar ",
  " (lowercase letters, digits and dashes).": " (letras minúsculas, números e hifens).",
  "Who is it?": "Quem é?",
  "a planner who keeps the team on track": "alguém organizado que mantém a equipe no rumo",
  "A line about its job and character. Empty makes a helpful all-rounder.":
    "Uma linha sobre o trabalho e o jeito dele. Se ficar vazio, ele vira um ajudante para tudo.",
  "How does it talk?": "Como ele fala?",
  "calm and structured": "calmo e organizado",
  Looks: "Aparência",
  Brain: "Cérebro",
  "the default model": "o modelo padrão",
  "Let it use my connections ({list}, and the ones I add later)":
    "Deixar usar minhas conexões ({list}, e as que eu adicionar depois)",
  "Give it a computer of its own (a desktop with a browser it can use)":
    "Dar a ele um computador próprio (uma área de trabalho com um navegador que ele pode usar)",
  "Needs Docker running on the gateway's machine. Without it the kitty still thinks, talks and uses connections.":
    "Precisa do Docker rodando na máquina do gateway. Sem ele, o kitty continua pensando, falando e usando conexões.",
  "Docker is not installed on the gateway's machine. Install Docker Desktop to give kitties a computer; without it they still think, talk and use connections.":
    "O Docker não está instalado na máquina do gateway. Instale o Docker Desktop para dar um computador aos kitties; sem ele eles continuam pensando, falando e usando conexões.",
  "Docker is installed but not running. Open Docker Desktop and wait for it to start; the option unlocks by itself.":
    "O Docker está instalado, mas não está rodando. Abra o Docker Desktop e espere iniciar; a opção é liberada sozinha.",
  "Creating…": "Criando…",
  Create: "Criar",
  "a name": "um nome",
  "a name no other kitty has": "um nome que nenhum outro kitty tenha",

  // Kitty: basics
  "{name} may use {connection}.": "{name} pode usar {connection}.",
  "{name} no longer uses {connection}.": "{name} não usa mais {connection}.",
  "{name} now thinks with {model}.": "{name} agora pensa com {model}.",
  "no model": "sem modelo",
  Personality: "Personalidade",
  "Saved.": "Salvo.",
  "Save personality": "Salvar personalidade",
  Connections: "Conexões",
  "None yet. Add services in Settings, Connections, and choose the kitties that may use them.":
    "Nenhuma ainda. Adicione serviços em Configurações, Conexões, e escolha quais kitties podem usá-los.",
  "Every connection, including the ones you add later":
    "Todas as conexões, incluindo as que você adicionar depois",
  "{name} uses no connection.": "{name} não usa nenhuma conexão.",
  "{name} may use every connection.": "{name} pode usar todas as conexões.",
  "All, new ones too": "Todas, as novas também",

  // Kitty: drawer
  Configuration: "Configuração",
  "looks, brain, personality, connections, limits and memory":
    "aparência, cérebro, personalidade, conexões, limites e memória",
  done: "pronto",
  Desktop: "Área de trabalho",
  "brain only, no desktop": "só cérebro, sem área de trabalho",
  refresh: "atualizar",
  "Chat with {name}": "Conversar com {name}",
  Start: "Iniciar",
  "Remove {name} and its container?": "Remover {name} e o contêiner dele?",

  // Gateway connection
  "The address has to be like http://192.168.1.20:7488 or https://kitties.example.com.":
    "O endereço deve ser como http://192.168.1.20:7488 ou https://kitties.example.com.",
  "This address is plain http: the token would travel unencrypted. Use https.":
    "Este endereço é http sem criptografia: o token viajaria sem proteção. Use https.",
  "the address": "o endereço",
  "The app is a remote for the Kitties gateway running on your computer or server. Type its address on the network and its token.":
    "O app é um controle remoto para o gateway do Kitties que roda no seu computador ou servidor. Digite o endereço dele na rede e o token.",
  "Let the gateway listen on the network, not only on the computer:":
    "Deixe o gateway escutar na rede, e não só no computador:",
  " (or a server with HTTPS, see DEPLOY.md).": " (ou um servidor com HTTPS, veja DEPLOY.md).",
  "The address is the computer's, like": "O endereço é o do computador, por exemplo",
  "The token is in ": "O token está em ",
  " on that computer.": " nesse computador.",
  "The address of the kitties gateway. On this machine the token is found on its own (the gateway leaves it in ":
    "O endereço do gateway dos kitties. Nesta máquina o token é encontrado sozinho (o gateway o deixa em ",
  "); a remote gateway needs the token it printed when it started.":
    "); um gateway remoto precisa do token que ele mostrou ao iniciar.",
  " Leave the address empty when this page is served by the gateway itself.":
    " Deixe o endereço vazio quando esta página for servida pelo próprio gateway.",
  Address: "Endereço",
  " (optional)": " (opcional)",
  " (remote gateways only)": " (só gateways remotos)",
  "paste the gateway's token": "cole o token do gateway",
  "found on this machine when empty": "se ficar vazio, é buscado nesta máquina",
  Connect: "Conectar",
  Gateway: "Gateway",
  "Where is it? In the console where the gateway started, or run":
    "Onde está? No console onde o gateway foi iniciado, ou rode",
  "on that machine. In the desktop app: Settings, Preferences, Network access.":
    "nessa máquina. No app de desktop: Configurações, Preferências, Acesso de rede.",
  // Quick start (Welcome, first start)
  "Quick start": "Início rápido",
  "Three answers and your first kitty is ready to chat. The rest can wait.":
    "Três respostas e seu primeiro kitty fica pronto para conversar. O resto pode esperar.",
  "Its brain": "O cérebro dele",
  "Its key": "A chave dele",
  "paste the key": "cole a chave",
  "Get a key": "Conseguir uma chave",
  "Ollama has to be running on this computer, at {addr}.":
    "O Ollama tem que estar rodando neste computador, em {addr}.",
  "Your kitty's name": "O nome do seu kitty",
  "the key": "a chave",
  "the kitty's name": "o nome do kitty",
  "Checking the model…": "Testando o modelo…",
  "Creating {name}…": "Criando {name}…",
};

export default dict;
