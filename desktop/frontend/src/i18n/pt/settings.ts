import type { Dictionary } from "../../i18n";

// Settings: models, connections, notifications, preferences.
const dict: Dictionary = {
  // Tabs and dialog
  Settings: "Configurações",
  Models: "Modelos",
  Connections: "Conexões",
  Notifications: "Notificações",
  Gateway: "Gateway",
  Connection: "Conexão",
  Preferences: "Preferências",
  "Loading…": "Carregando…",
  "Settings could not be drawn: {error}. The gateway may be older than this app: update it and reopen.":
    "Não deu para mostrar as configurações: {error}. Talvez o gateway seja mais antigo que este app: atualize-o e abra de novo.",
  "More options": "Mais opções",

  // Models
  "Models are the brains your kitties think with. Add one to start; you can add others later and give each kitty the one it should use.":
    "Os modelos são o cérebro com que seus kitties pensam. Adicione um para começar; depois você pode adicionar outros e dar a cada kitty o que ele deve usar.",
  "No model yet": "Nenhum modelo ainda",
  "Your kitties need a brain to think with. Add one from a provider you have an account with, or Ollama to run free models on this computer.":
    "Seus kitties precisam de um cérebro para pensar. Adicione um de um provedor onde você tem conta, ou o Ollama para usar modelos grátis neste computador.",
  "Add a model": "Adicionar um modelo",
  "Which provider?": "Qual provedor?",
  "Pick where the brain comes from. Pay with an API key, or sign in with a subscription you already have (Claude, ChatGPT, GitHub Copilot). Ollama on this computer is free.":
    "Escolha de onde vem o cérebro. Pague com uma chave de API ou entre com uma assinatura que você já tem (Claude, ChatGPT, GitHub Copilot). O Ollama neste computador é grátis.",
  "API key or your {plan}": "Chave de API ou seu {plan}",
  "{name} answered: it works.": "{name} respondeu: funciona.",
  "it did not answer": "não respondeu",
  "the API key": "a chave de API",
  "sign in": "entrar",
  "the model": "o modelo",
  change: "mudar",
  "How to pay for it": "Como pagar",
  "API key": "Chave de API",
  "Pay per use with a key": "Pague pelo uso com uma chave",
  "Not available": "Não disponível",
  "Sign in with your subscription": "Entre com sua assinatura",
  " (if the server asks for one)": " (se o servidor pedir)",
  "saved: leave empty to keep it": "salva: deixe vazio para manter",
  "The provider refused this key: check that it is complete and still valid.":
    "O provedor recusou esta chave: confira se está completa e ainda é válida.",
  "Check key": "Verificar chave",
  "Key accepted: {n} models available.": "Chave aceita: {n} modelos disponíveis.",
  "{n} models available.": "{n} modelos disponíveis.",
  "paste the key here": "cole a chave aqui",
  "Kept on your gateway and never shown again.":
    "Fica guardada no seu gateway e não aparece de novo.",
  "Get a key from {name} ↗": "Pegue uma chave em {name} ↗",
  "Server address": "Endereço do servidor",
  "Leave it empty when Ollama runs on this computer. ":
    "Deixe vazio se o Ollama roda neste computador. ",
  "The address of the server's API, usually ending in /v1. ":
    "O endereço da API do servidor, que costuma terminar em /v1. ",
  "Download Ollama ↗": "Baixar o Ollama ↗",
  Model: "Modelo",
  "model name": "nome do modelo",
  "Not sure? Keep the suggested one.": "Na dúvida? Fique com o sugerido.",
  "Name in Kitties": "Nome no Kitties",
  "How the kitties and the configuration file call it.":
    "Como os kitties e o arquivo de configuração o chamam.",
  "Only to reach it through a proxy or a compatible service.":
    "Só para chegar a ele por um proxy ou um serviço compatível.",
  "Longest answer (tokens)": "Resposta mais longa (tokens)",
  "Tasks a day another kitty may take over": "Tarefas por dia que outro kitty pode assumir",
  "When a kitty with no backup gives up, the free kitty most like it takes the task over, up to this many times a day. Write off to be asked instead.":
    "Quando um kitty sem reserva desiste, o kitty livre mais parecido assume a tarefa, até este número de vezes por dia. Escreva off para que perguntem a você.",
  "Working on their own": "Trabalho por conta própria",
  "Try an unfinished goal again after": "Tentar de novo um objetivo inacabado após",
  "A goal that ended blocked or waiting for you runs again by itself after each of these waits. Write off to never try again on its own.":
    "Um objetivo que terminou bloqueado ou esperando por você roda de novo sozinho após cada uma destas esperas. Escreva off para nunca tentar de novo sozinho.",
  "Wait for a model that is down": "Esperar por um modelo fora do ar",
  "How long a task waits for its model to answer again (busy, failing, unreachable) before giving up. Write off to not wait.":
    "Quanto tempo uma tarefa espera o modelo voltar a responder (ocupado, com falhas, inacessível) antes de desistir. Escreva off para não esperar.",
  "If it is down, think with": "Se estiver fora do ar, pensar com",
  "nothing else (wait for it)": "nada mais (esperar por ele)",
  "While this model is busy or unreachable, the kitties go on with this one instead of waiting.":
    "Enquanto este modelo estiver ocupado ou inacessível, os kitties seguem com este outro em vez de esperar.",
  "Saved, but the test did not pass: ": "Salvo, mas o teste não passou: ",
  "Change something": "Mudar algo",
  "Saved without a passing test.": "Salvo sem passar no teste.",
  "Keep it anyway": "Manter mesmo assim",
  "Saving…": "Salvando…",
  "Checking…": "Verificando…",
  "Save and test": "Salvar e testar",
  "✓ It answered: it works.": "✓ Respondeu: funciona.",
  "no answer": "sem resposta",
  "{names} will switch to another model.": "{names} vai passar para outro modelo.",
  "No kitty uses it.": "Nenhum kitty usa.",
  "Remove {name} ({model})? {what}": "Remover {name} ({model})? {what}",
  default: "padrão",
  "Used by {names}": "Usado por {names}",
  "No kitty uses it yet": "Nenhum kitty usa ainda",
  " · from the configuration file": " · do arquivo de configuração",
  "Testing…": "Testando…",
  Test: "Testar",
  "Make default": "Tornar padrão",
  "Edit {name}": "Editar {name}",
  Edit: "Editar",
  "Remove {name}": "Remover {name}",

  // Connections
  "Connections let your kitties act in other services: send messages, read email, open issues, post. You choose which kitties may use each one, and they act as far as each account allows. To have a kitty ask you first, or never do something, tell it, or set it in its limits.":
    "As conexões deixam seus kitties agir em outros serviços: mandar mensagens, ler e-mails, abrir issues, publicar. Você escolhe quais kitties podem usar cada uma, e eles agem até onde cada conta permite. Para um kitty perguntar antes, ou nunca fazer algo, diga isso a ele ou coloque nos limites dele.",
  "Your connections": "Suas conexões",
  "None yet: pick a service below.": "Nenhuma ainda: escolha um serviço abaixo.",
  "Add a connection": "Adicionar uma conexão",
  "Saved: now sign in.": "Salva: agora entre.",
  "{name} is saved: testing it…": "{name} foi salva: testando…",
  "Search services": "Buscar serviços",
  "Search: Slack, Gmail, GitHub, Stripe…": "Buscar: Slack, Gmail, GitHub, Stripe…",
  All: "Todos",
  "Nothing matches: try another word.": "Nada encontrado: tente outra palavra.",
  "Sign in with {name}": "Entrar com {name}",
  "App keys": "Chaves do app",
  Webhook: "Webhook",
  "Bot token": "Token do bot",
  "Access token": "Token de acesso",
  "API token": "Token de API",
  "API key or token": "Chave de API ou token",
  "Allow Kitties in your browser": "Permita o Kitties no seu navegador",
  "Through an app of yours": "Por um app seu",
  "Four keys from your app's page; they do not expire":
    "Quatro chaves da página do seu app; elas não expiram",
  "Posts to one channel, nothing to sign in": "Publica em um canal, sem precisar entrar",
  "Created in the service's settings": "Criado nas configurações do serviço",
  " (optional)": " (opcional)",
  "Keep the saved one": "Manter a salva",
  Clear: "Limpar",
  "removed when you save": "é removido ao salvar",
  "Sign out of {name}? The kitties stop using this account.":
    "Sair de {name}? Os kitties vão parar de usar esta conta.",
  "Sign out": "Sair",
  "How do you connect?": "Como você se conecta?",
  easiest: "o mais fácil",
  "✓ Signed in as {account}. The kitties use this account.":
    "✓ Você entrou como {account}. Os kitties usam esta conta.",
  "✓ Signed in. The kitties use this account.": "✓ Você entrou. Os kitties usam esta conta.",
  "Sign in again": "Entrar de novo",
  "{name} needs an app of your own for now: add this callback address to it, then paste its client id (and its secret, if it has one).":
    "Por enquanto, {name} precisa de um app seu: adicione este endereço de retorno nele e depois cole o client id (e o secret, se tiver).",
  "Waiting for the browser…": "Esperando o navegador…",
  "Finish signing in in your browser; this changes by itself.":
    "Termine de entrar no seu navegador; isto se atualiza sozinho.",
  "Sign in and allow Kitties. Nothing to copy.": "Entre e permita o Kitties. Nada para copiar.",
  "It opens your browser; this page follows by itself.":
    "Seu navegador vai abrir; esta página acompanha sozinha.",
  "Use an app of your own": "Usar um app seu",
  "Its callback address:": "O endereço de retorno dele:",
  "The token pasted here has expired: it lasts a short time. Sign in, or":
    "O token colado aqui expirou: ele dura pouco tempo. Entre, ou",
  "use the app keys, which do not expire": "use as chaves do app, que não expiram",
  "paste a new one": "cole um novo",
  ", and clear the old token so it does not stand in the way.":
    ", e limpe o token antigo para ele não atrapalhar.",
  "Which kitties may use it": "Quais kitties podem usar",
  "What the kitties can do with it": "O que os kitties podem fazer com ela",
  "What the kitties are told it is for": "Para que você diz aos kitties que ela serve",
  "uses the OAuth 1.0 keys": "usa as chaves OAuth 1.0",
  "uses a token": "usa um token",
  "uses an app password": "usa uma senha de app",
  "A user and an app password of the account": "Um usuário e uma senha de app da conta",
  "uses a webhook": "usa um webhook",
  "token expired": "token expirado",
  "token expires in {time}": "o token expira em {time}",
  "signed in": "você entrou",
  "sign in needed": "precisa entrar",
  "What the kitties use to reach the account": "O que os kitties usam para acessar a conta",
  "A pasted token of this service lasts a short time: sign in, or use keys that do not expire":
    "Um token colado deste serviço dura pouco tempo: entre, ou use chaves que não expiram",
  "No kitty may use it yet": "Nenhum kitty pode usar ainda",
  "✗ It does not work: {error}": "✗ Não funciona: {error}",
  "✓ It works as {account}.": "✓ Funciona como {account}.",
  "✓ It works.": "✓ Funciona.",
  "It cannot be tested without acting: it is tried the first time a kitty uses it.":
    "Não dá para testar sem fazer algo de verdade: ela é testada na primeira vez que um kitty a usar.",
  "Sign in": "Entrar",
  "Remove the connection {name}? The kitties lose it too.":
    "Remover a conexão {name}? Os kitties também a perdem.",

  // Notifications
  "A kitty gave up and needs my help": "Um kitty desistiu e precisa da minha ajuda",
  "An action is waiting for my yes": "Uma ação está esperando o meu sim",
  "A goal finished": "Um objetivo terminou",
  "A kitty finished a subtask another handed it, or could not":
    "Um kitty terminou uma subtarefa que outro lhe passou, ou não conseguiu",
  "A goal could not finish": "Um objetivo não conseguiu terminar",
  "A kitty's container was brought back": "O contêiner de um kitty foi recuperado",
  "A kitty spent its day's tokens": "Um kitty gastou os tokens do dia",
  "Saved.": "Salvo.",
  "Sent: look at your phone or channel.": "Enviado: olhe seu celular ou seu canal.",
  "It did not go through.": "Não foi enviado.",
  "Summary sent.": "Resumo enviado.",
  "The summary did not go through.": "O resumo não foi enviado.",
  "The kitties work while you are away. Choose how they reach you outside this window: a push to your phone is the quickest, and Telegram, Slack or email work too.":
    "Os kitties trabalham enquanto você está fora. Escolha como eles falam com você fora desta janela: uma notificação no celular é o mais rápido, e Telegram, Slack ou e-mail também servem.",
  "Get alerts on your phone": "Receba alertas no celular",
  "Install the free ntfy app on your phone, choose a topic name and subscribe to it. Then connect the same topic here.":
    "Instale o app grátis ntfy no seu celular, escolha um nome de tópico e se inscreva nele. Depois conecte o mesmo tópico aqui.",
  "Set up phone notifications": "Configurar notificações no celular",
  "Send through": "Enviar por",
  "nowhere: notifications are off": "nenhum lugar: as notificações estão desligadas",
  "Tell me when": "Me avise quando",
  "A summary of the last day, every day at": "Um resumo do último dia, todo dia às",
  none: "nenhum",
  "Send one now": "Enviar um agora",
  "What each kitty did, what it spent, and what waits for your yes. Leave it empty for no summary.":
    "O que cada kitty fez, o que gastou e o que espera o seu sim. Deixe vazio para não receber resumo.",
  "Start it with my day: what is on the calendar, the reminders still to do and the weather, from my connections (calendar, reminders, weather).":
    "Começar com o meu dia: o que tem na agenda, os lembretes pendentes e o tempo, das minhas conexões (agenda, lembretes, tempo).",
  "Let me answer from ntfy: what I write to the topic goes to the kitty that notified me (or the one I name, like luna: …, or all: …), and its answer comes back here.":
    "Responder pelo ntfy: o que eu escrever no tópico vai para o kitty que me avisou (ou para o que eu nomear, como luna: …, ou todos: …), e a resposta volta pelo mesmo caminho.",
  "The topic works as a password: give it 20 characters at least, hard to guess, or protect it with an access token.":
    "O tópico funciona como senha: dê a ele pelo menos 20 caracteres difíceis de adivinhar, ou proteja-o com um token de acesso.",
  "Send a test": "Enviar um teste",

  // Preferences
  "Show technical details": "Mostrar detalhes técnicos",
  "What a kitty did exactly (coordinates, commands, results), the tokens each answer used, and errors as the services wrote them.":
    "O que um kitty fez exatamente (coordenadas, comandos, resultados), os tokens que cada resposta usou e os erros do jeito que os serviços escreveram.",
  Language: "Idioma",
  "The system's language is used until you pick one.":
    "O idioma do sistema é usado até você escolher um.",
  "A desktop or Kitties was updated, or a new version is waiting":
    "Um desktop ou o Kitties foi atualizado, ou há uma versão nova esperando",
  "What a kitty goes on to do in a long task (one every 10 minutes at most)":
    "O que uma kitty vai fazer em uma tarefa longa (no máximo um a cada 10 minutos)",
  "Kitties did not come back in a minute: restart it by hand.":
    "O Kitties não voltou em um minuto: reinicie-o manualmente.",
  "Update Kitties to {version}? It restarts in a moment; the kitties' desktops keep running.":
    "Atualizar o Kitties para {version}? Ele reinicia em instantes; as áreas de trabalho dos kitties continuam funcionando.",
  "Kitties {version}": "Kitties {version}",
  "it is the newest version": "é a versão mais nova",
  "Kitties {version} is available.": "O Kitties {version} está disponível.",
  "Update and restart": "Atualizar e reiniciar",
  "Updated to {version}: restarting…": "Atualizado para {version}: reiniciando…",
  "Look for a new version": "Procurar uma versão nova",
  "Looked {when}.": "Procurada {when}.",
  "Kitties does not look for new versions: turn it on with updates.gateway in the configuration.":
    "O Kitties não procura versões novas: ative com updates.gateway na configuração.",
  "Kitties looks for new versions and installs them by itself when nothing is in use.":
    "O Kitties procura versões novas e as instala sozinho quando nada está em uso.",
  "Kitties looks for new versions and tells you; you decide when to update.":
    "O Kitties procura versões novas e avisa você; você decide quando atualizar.",
  "This update is checked against its checksums only, which do not say who made it: set updates.public_key to require the release key's signature.":
    "Esta atualização é verificada apenas pelas somas de verificação, que não dizem quem a fez: defina updates.public_key para exigir a assinatura da chave dos releases.",
  "{version} did not hold here and Kitties went back to {current}: it is not offered again.":
    "{version} não se sustentou aqui e o Kitties voltou para {current}: não será oferecida de novo.",

  // Network access (local, LAN, WAN)
  "Network access": "Acesso de rede",
  "Where Kitties can be reached from. Now: {addr}.":
    "De onde o Kitties pode ser acessado. Agora: {addr}.",
  Local: "Local",
  LAN: "LAN",
  WAN: "WAN",
  "Only this computer. The safest choice.": "Somente este computador. A opção mais segura.",
  "your network address": "o endereço da sua rede",
  "Other devices in your home or office network.":
    "Outros dispositivos da sua rede de casa ou escritório.",
  "Every network interface: the internet too if this computer is public.":
    "Todas as interfaces de rede: a internet também, se este computador for público.",
  "You are using Kitties from another device: with Local you will not be able to reach it from here. Continue?":
    "Você está usando o Kitties de outro dispositivo: com Local não será possível acessá-lo daqui. Continuar?",
  Continue: "Continuar",
  "Kitties did not answer here after the change: it may now listen only elsewhere. Try {addr}.":
    "O Kitties não respondeu aqui após a mudança: talvez agora ele escute só em outro endereço. Tente {addr}.",
  "WAN listens on every interface": "WAN escuta em todas as interfaces",
  "The page and its token travel over plain http and the token is the only password. Use it only behind a firewall or a VPN, or put https in front.":
    "A página e seu token trafegam por http sem criptografia e o token é a única senha. Use somente atrás de um firewall ou VPN, ou coloque https na frente.",
  "Open to your local network since the first start":
    "Aberto à sua rede local desde a primeira inicialização",
  "Kitties listens on your network so it can be set up from another computer, behind the token. The page travels over plain http: when you are done, choose Local.":
    "O Kitties escuta na sua rede para que você possa configurá-lo de outro computador, protegido pelo token. A página trafega por http sem criptografia: quando terminar, escolha Local.",
  "I understand the risk": "Entendo o risco",
  "Saved: restarting Kitties…": "Salvo: reiniciando o Kitties…",
  "Apply and restart": "Aplicar e reiniciar",
  // Claude connector (MCP)
  "Kitties did not come back after the change: reload this page.":
    "O Kitties não voltou após a mudança: recarregue esta página.",
  "Copied: {what}.": "Copiado: {what}.",
  "the token": "o token",
  "the command": "o comando",
  "This page cannot reach the clipboard: select the text and copy it by hand.":
    "Esta página não acessa a área de transferência: selecione o texto e copie à mão.",
  "A new token replaces the old one and every connection that used it stops working, claude.ai sign-ins too. Continue?":
    "Um novo token substitui o anterior e toda conexão que o usava deixa de funcionar, inclusive os acessos do claude.ai. Continuar?",
  "Rotate token": "Girar token",
  "{name} will lose access at once and has to sign in again. Revoke?":
    "{name} perderá o acesso na hora e terá que entrar de novo. Revogar?",
  Revoke: "Revogar",
  "Claude connector": "Conector do Claude",
  "Lets Claude list, create and drive your kitties through {url}.":
    "Permite que o Claude liste, crie e controle seus kitties por {url}.",
  "Allow commands in a kitty": "Permitir comandos em um kitty",
  "Adds kitty_exec: Claude may run commands inside a kitty's container.":
    "Adiciona kitty_exec: o Claude pode executar comandos dentro do contêiner de um kitty.",
  "Allow Claude to approve": "Permitir que o Claude aprove",
  "Adds approval_approve: Claude may say yes, once, to an action held for you.":
    "Adiciona approval_approve: o Claude pode dizer sim, uma vez, a uma ação retida para você.",
  "Sign-in for claude.ai": "Acesso para o claude.ai",
  "Remote clients sign in with OAuth. It needs https in front (public address).":
    "Clientes remotos entram com OAuth. Precisa de https na frente (endereço público).",
  "Claude (MCP)": "Claude (MCP)",
  "This gives Claude more power": "Isso dá mais poder ao Claude",
  "Commands run inside the kitty and an approval lets a held action go ahead without you. Turn it on only if you trust the client that holds the token.":
    "Os comandos rodam dentro do kitty e uma aprovação deixa uma ação retida seguir sem você. Ative só se confiar no cliente que tem o token.",
  "Let Claude run the fleet as an orchestrator. It answers wherever Kitties does, behind a token of its own.":
    "Deixe o Claude conduzir a frota como orquestrador. Ele responde onde o Kitties responder, com um token próprio.",
  "This computer": "Este computador",
  "Local network": "Rede local",
  "Public address": "Endereço público",
  "the command with the token": "o comando com o token",
  "Run this on the computer where Claude Code is. Copy command puts the token in; it is not shown here.":
    "Execute no computador onde o Claude Code está. Copiar comando coloca o token; aqui ele não é mostrado.",
  "Run this on this computer, where Claude Code is. It reads the token from its file; the token is not shown here.":
    "Execute neste computador, onde o Claude Code está. Ele lê o token do arquivo; aqui ele não é mostrado.",
  "Plain http: the token crosses the network unencrypted. Use it on a network you trust, or put https in front (public address).":
    "http sem criptografia: o token atravessa a rede em claro. Use numa rede de confiança ou coloque https na frente (endereço público).",
  "Connect Claude Code": "Conectar o Claude Code",
  "Copy command": "Copiar comando",
  "Copy token": "Copiar token",
  "Signed-in clients": "Clientes conectados",
  "Nobody has signed in yet.": "Ninguém entrou ainda.",
  "since {when}": "desde {when}",
  Revoked: "Revogado",
  "The connector answers: {n} tools for Claude.":
    "O conector responde: {n} ferramentas para o Claude.",
  "The connector does not answer: {why}": "O conector não responde: {why}",
  "Test connection": "Testar conexão",
  "last used {when}": "último uso {when}",

  // WhatsApp as a linked device
  "Write the number with its country code, like +56 9 1234 5678.":
    "Escreva o número com o código do país, como +55 11 91234 5678.",
  "The phone forgets this device and the kitties stop reading and writing on WhatsApp until you scan a new code. Unlink?":
    "O telefone esquece este dispositivo e os kitties deixam de ler e escrever no WhatsApp até você escanear um código novo. Desvincular?",
  Unlink: "Desvincular",
  "Unlinked.": "Desvinculado.",
  "Sending…": "Enviando…",
  "Sent: look at that phone.": "Enviado: olhe esse telefone.",
  "Not sent: {why}": "Não enviado: {why}",
  "Connecting…": "Conectando…",
  "Waiting for the QR code to be scanned": "Esperando o código QR ser escaneado",
  "The code expired": "O código expirou",
  Connected: "Conectado",
  "Reconnecting…": "Reconectando…",
  "The phone removed this device": "O telefone removeu este dispositivo",
  "It could not connect": "Não foi possível conectar",
  "WhatsApp (linked device)": "WhatsApp (dispositivo conectado)",
  "A number of their own: link a phone as on WhatsApp Web, and the people on the list talk to a kitty from WhatsApp. Off until you turn it on.":
    "Um número próprio: conecte um telefone como no WhatsApp Web e as pessoas da lista conversam com um kitty pelo WhatsApp. Desligado até você ativar.",
  "Not an official WhatsApp API": "Não é uma API oficial do WhatsApp",
  "WhatsApp's terms do not allow unofficial clients, and it may ban the number. Use a spare number, not your own. The official way is the WhatsApp Cloud API connection (Meta Business).":
    "Os termos do WhatsApp não permitem clientes não oficiais e ele pode banir o número. Use um número reserva, não o seu. O caminho oficial é a conexão WhatsApp Cloud API (Meta Business).",
  "Turn on WhatsApp": "Ativar WhatsApp",
  "Messages from numbers not on the list are ignored, and the kitties write only to them.":
    "As mensagens de números fora da lista são ignoradas, e os kitties só escrevem para os da lista.",
  "The kitty that answers": "O kitty que responde",
  "Choose a kitty": "Escolha um kitty",
  "Numbers that may write": "Números que podem escrever",
  Add: "Adicionar",
  "Nobody yet: every message is ignored until you add a number.":
    "Ninguém ainda: toda mensagem é ignorada até você adicionar um número.",
  "Answer in groups when someone mentions the kitty":
    "Responder em grupos quando alguém menciona o kitty",
  "No group has written yet: write in a group with the kitties' number and it shows up here.":
    "Nenhum grupo escreveu ainda: escreva num grupo com o número dos kitties e ele aparece aqui.",
  "Groups that wrote lately. The kitty answers only in the ones you pick.":
    "Grupos que escreveram há pouco. O kitty responde só nos que você escolher.",
  "Messages sent per hour, at most": "Mensagens enviadas por hora, no máximo",
  "{n} messages sent in the last hour.": "{n} mensagens enviadas na última hora.",
  "{n} were held by the hourly limit.": "{n} ficaram retidas pelo limite por hora.",
  "Who answers each one": "Quem responde a cada um",
  "Quiet hours": "Horário de silêncio",
  to: "às",
  "Quiet hours start": "Início do silêncio",
  "Quiet hours end": "Fim do silêncio",
  "Messages then are answered when they end.":
    "As mensagens desse período são respondidas ao terminar.",
  Reconnect: "Reconectar",
  "Quiet hours until {until}: {n} messages wait.": "Silêncio até {until}: {n} mensagens esperam.",
  "Remove {number}": "Remover {number}",
  Message: "Mensagem",
  File: "Arquivo",
  "Sent again.": "Enviada de novo.",
  Resend: "Reenviar",
  "These were not sent: more than {limit} messages went in an hour. Resend one when there is room, or raise the limit above. The list is kept sealed, so a restart does not lose it.":
    "Não foram enviadas: saíram mais de {limit} mensagens em uma hora. Reenvie uma quando houver espaço, ou aumente o limite acima. A lista fica selada, então um reinício não a perde.",
  "Voice notes": "Mensagens de voz",
  "Never: the kitties write": "Nunca: os kitties escrevem",
  "Answer a voice note with one": "Responder uma mensagem de voz com outra",
  "Every answer": "Todas as respostas",
  "Short answers are read aloud; long ones and code still go as text.":
    "As respostas curtas são lidas em voz alta; as longas e o código continuam indo como texto.",
  "Needs an OpenAI profile with a key, for its speech.":
    "Precisa de um perfil da OpenAI com chave, pela voz.",
  "Not an official API": "Não é uma API oficial",
  "Kitties links as WhatsApp Web does. WhatsApp's terms do not allow it, and it may ban the number without warning.":
    "O Kitties se conecta como o WhatsApp Web. Os termos do WhatsApp não permitem isso, e ele pode banir o número sem aviso.",
  "A spare number, and a short list": "Um número reserva e uma lista curta",
  "Use a number that is not your own. Only the numbers and groups you add can talk to the kitties, and the kitties write only to them, a few messages an hour.":
    "Use um número que não seja o seu. Só os números e grupos que você adicionar podem falar com os kitties, e os kitties só escrevem para eles, poucas mensagens por hora.",
  "The session stays here": "A sessão fica aqui",
  "The linked device is sealed in this gateway's vault. Unlink it here or from the phone at any time.":
    "O dispositivo conectado fica selado no cofre deste gateway. Desconecte aqui ou pelo telefone quando quiser.",
  "Before you turn on WhatsApp": "Antes de ativar o WhatsApp",
  "The kitties get a WhatsApp number of their own: the people on your list write to them, and they answer from here.":
    "Os kitties ganham um número de WhatsApp próprio: as pessoas da sua lista escrevem para eles, e eles respondem daqui.",
  "For a business, the official way is the WhatsApp Cloud API connection (Meta Business), in Connections.":
    "Para uma empresa, o caminho oficial é a conexão WhatsApp Cloud API (Meta Business), em Conexões.",
  "Accept and continue": "Aceitar e continuar",
  "Voice note": "Mensagem de voz",
  Reaction: "Reação",
  "Held by the hourly limit ({n})": "Retidas pelo limite por hora ({n})",
  "Clear the list": "Limpar a lista",
  "I understand that WhatsApp may ban this number": "Entendo que o WhatsApp pode banir este número",
  "QR code to link WhatsApp": "Código QR para conectar o WhatsApp",
  "Open WhatsApp on the phone of the kitties' number.":
    "Abra o WhatsApp no telefone do número dos kitties.",
  "Go to Settings, Linked devices, Link a device.":
    "Vá em Configurações, Dispositivos conectados, Conectar um dispositivo.",
  "Point the phone at this code. It changes every few seconds.":
    "Aponte o telefone para este código. Ele muda a cada poucos segundos.",
  "Show a new code": "Mostrar um código novo",
  "{kitty} answers on WhatsApp; each number has its own chat here.":
    "{kitty} responde no WhatsApp; cada número tem seu próprio chat aqui.",
  "{n} messages from other numbers were ignored.":
    "{n} mensagens de outros números foram ignoradas.",
  "Use it from a browser": "Use pelo navegador",
  "The same page as this app. Open it and sign in with the token.":
    "A mesma página deste app. Abra e entre com o token.",
  "Open in the browser": "Abrir no navegador",
  Show: "Mostrar",
  Hide: "Ocultar",
  "Keep the token to yourself: it is the only password of the page.":
    "Guarde o token para você: é a única senha da página.",
  "Make a new token? Every browser and program that uses the old one is signed out; this app takes the new one by itself.":
    "Criar um token novo? Todo navegador e programa que usa o antigo fica de fora; este app pega o novo sozinho.",
  "Make a new token? Every browser and program that uses the old one is signed out, this page too. The new one is shown in the console where Kitties runs (or with kitties token) and in the desktop app.":
    "Criar um token novo? Todo navegador e programa que usa o antigo fica de fora, esta página também. O novo aparece no console onde o Kitties roda (ou com kitties token) e no app de desktop.",
  "Make a new token": "Criar um token novo",
  "Restarting Kitties…": "Reiniciando o Kitties…",
  "If the token got out, a new one signs everybody out.":
    "Se o token vazou, um novo deixa todos de fora.",
  "QR code to open Kitties on a phone": "Código QR para abrir o Kitties num celular",
  "On a phone in the same network, point the camera here: Kitties opens signed in. Anyone who sees this code can get in.":
    "Num celular da mesma rede, aponte a câmera aqui: o Kitties abre já com a sessão iniciada. Quem vir este código pode entrar.",
  "To open it from a phone with a QR code, choose LAN above.":
    "Para abrir num celular com um código QR, escolha LAN acima.",

  // Machines: RAM and swap
  Machines: "Máquinas",
  "{n} MB": "{n} MB",
  "{n} GB": "{n} GB",
  "{used} of {total}": "{used} de {total}",
  None: "Nenhuma",
  RAM: "RAM",
  "Default ({size})": "Padrão ({size})",
  Swap: "Swap",
  "No swap": "Sem swap",
  "{name}'s machine runs in the cloud: its memory is the size of the machine there.":
    "A máquina de {name} roda na nuvem: sua memória é o tamanho da máquina lá.",
  "Memory in use": "Memória em uso",
  "Swap in use": "Swap em uso",
  "Turn the machine on to see the memory it uses.":
    "Ligue a máquina para ver a memória que ela usa.",
  "Holding the most: {list}": "O que mais ocupa: {list}",
  "The new memory applies the next time the machine is turned on.":
    "A nova memória vale a partir da próxima vez que a máquina for ligada.",
  "{name} sees how much memory is free and does not open tabs or programs that do not fit; it closes what it no longer needs.":
    "{name} vê quanta memória está livre e não abre abas nem programas que não cabem; fecha o que não precisa mais.",
  "Each kitty's desktop runs on a machine of its own with so much RAM and swap. Kitties see how much is free and do not open browser tabs or programs that do not fit.":
    "O desktop de cada kitty roda numa máquina própria com certa RAM e swap. Os kitties veem quanto está livre e não abrem abas do navegador nem programas que não cabem.",
  "For every kitty": "Para todos os kitties",
  "The container engine has {engine} in all; the machines that are on share it. {n} machines of {size} add up to {sum}.":
    "O motor de contêineres tem {engine} no total; as máquinas ligadas o compartilham. {n} máquinas de {size} somam {sum}.",
  "A change reaches a machine at once when the engine can; otherwise the next time it is turned on. A machine in the cloud has the size of its provider.":
    "Uma mudança chega à máquina na hora quando o motor consegue; senão, na próxima vez que ela for ligada. Uma máquina na nuvem tem o tamanho do seu provedor.",
  "Each kitty": "Cada kitty",
};

export default dict;
