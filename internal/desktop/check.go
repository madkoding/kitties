package desktop

import "strings"

// checks are what a screen says when something there is for a person to pass and not for a
// kitty: a check that a human is present, or a code that only the person receives. Each is
// what an OCR of the screen reads, lower-cased, and the reason the kitty gives when it asks.
var checks = []struct {
	words  []string
	reason string
}{
	{[]string{"captcha", "i'm not a robot", "i am not a robot", "no soy un robot", "verify you are human", "verify you're human",
		"are you a robot", "unusual traffic", "confirm you are human", "confirma que eres humano", "não sou um robô"},
		"a check that a human is there (a CAPTCHA)"},
	{[]string{"verification code", "security code", "two-factor", "2-step verification", "authenticator app", "one-time code",
		"código de verificación", "código de seguridad", "verificación en dos pasos", "autenticación de dos factores", "código de verificação"},
		"a code that only you receive"},
}

// NeedsPerson reads what the screen says (an OCR of it) and tells whether it is stopped by
// something only a person can pass, and what.
func NeedsPerson(screen string) (reason string, ok bool) {
	screen = strings.ToLower(screen)
	for _, c := range checks {
		for _, w := range c.words {
			if strings.Contains(screen, w) {
				return c.reason, true
			}
		}
	}
	return "", false
}
