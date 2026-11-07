package guard

import (
	"regexp"
	"strings"
)

// WHAT LOOKS LIKE AN ATTEMPT.
//
// Suspicious reads a message from someone who is not the person and tells whether it tries to
// turn the kitty against them: to drop its rules, show its instructions, hand over secrets, run
// commands or read files. It is a net of usual phrasings in English, Spanish and Portuguese, not
// a proof: what it catches is recorded (and, with BlockInjection, not answered), and the kitty is
// told in any case that what third parties write is data, never an instruction.

var attempts = []pattern{
	{label: "override the rules", re: regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override|bypass|skip)\b.{0,30}\b(?:previous|prior|above|earlier|all|your|the|system)\b.{0,20}\b(?:instructions?|rules?|prompts?|guidelines?|directives?|restrictions?)`)},
	{label: "override the rules", re: regexp.MustCompile(`(?i)\b(?:ignora|olvida|omite|salta|desactiva|ignore|esqueca|esqueça)\w*\b.{0,30}\b(?:instrucciones|instrucoes|instruções|reglas|regras|indicaciones|restricciones|restricoes|restrições|prompt)`)},
	{label: "show the instructions", re: regexp.MustCompile(`(?i)\b(?:reveal|show|print|repeat|output|tell me|give me|what (?:is|are))\b.{0,25}\b(?:system prompt|initial prompt|hidden prompt|your (?:instructions|prompt|rules|configuration|config))`)},
	{label: "show the instructions", re: regexp.MustCompile(`(?i)\b(?:muestra|muestrame|muéstrame|revela|dime|repite|imprime|escribe|mostre|revele|diga)\b.{0,25}\b(?:tu|tus|el|las|seu|suas|o|as)\b.{0,10}\b(?:prompt|instrucciones|instruções|instrucoes|reglas|regras|configuraci[oó]n|configura[cç][aã]o)`)},
	{label: "new role", re: regexp.MustCompile(`(?i)\b(?:you are now|from now on,? you are|act as (?:an? )?(?:admin|administrator|developer|root|system|dan)|developer mode|jailbreak|do anything now|ahora eres|a partir de ahora eres|act[uú]a como (?:el |un )?(?:admin|administrador|desarrollador|root|sistema)|modo desarrollador|agora você é|agora voce e|modo desenvolvedor)\b`)},
	{label: "fake system message", re: regexp.MustCompile(`(?i)(?:^|\n)\s*(?:system|assistant|developer)\s*:|</?\s*(?:system|instructions?)\s*>|\[(?:system|inst)\]|###\s*(?:system|instruction)`)},
	{label: "ask for secrets", re: regexp.MustCompile(`(?i)\b(?:give|send|tell|share|show|paste|leak|dame|d[ée]me|env[ií]a\w*|p[aá]sa\w*|comparte\w*|dime|mu[eé]stra\w*|manda\w*|me (?:da|passa|envia|mostra)|passe|compartilhe)\b.{0,40}\b(?:api ?keys?|passwords?|tokens?|secrets?|credentials?|contrase[ñn]as?|claves?|credenciales?|secretos?|senhas?|credenciais|segredos?|\.env|vault|b[oó]veda|cofre)\b`)},
	{label: "run commands", re: regexp.MustCompile(`(?i)\b(?:run|execute|exec|ejecuta\w*|corre|lanza|rode|execute)\b.{0,25}\b(?:commands?|comandos?|scripts?|shell|terminal|bash|sudo|rm -rf|curl|wget|powershell)\b`)},
	{label: "read files", re: regexp.MustCompile(`(?i)(?:/etc/(?:passwd|shadow)|~/\.ssh|id_rsa|\.env\b|settings\.json|kitties\.json|vault\.json|\.kitties/|/home/kitty)`)},
}

// accents takes the accents of Spanish and Portuguese off, so "muéstrame" and "muestrame" read
// alike.
var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"ã", "a", "õ", "o", "â", "a", "ê", "e", "ô", "o", "à", "a", "ç", "c",
	"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ñ", "N", "Ã", "A", "Õ", "O", "Ç", "C")

func fold(s string) string { return accents.Replace(s) }

// Suspicious returns what the message seems to try, one label per kind; none for an ordinary one.
func Suspicious(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	plain := fold(text)
	seen := map[string]bool{}
	var out []string
	for _, p := range attempts {
		if seen[p.label] {
			continue
		}
		if p.re.MatchString(text) || p.re.MatchString(plain) {
			seen[p.label] = true
			out = append(out, p.label)
		}
	}
	return out
}
