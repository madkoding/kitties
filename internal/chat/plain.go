package chat

import (
	"regexp"
	"strings"
)

// The person reads what went wrong in plain words; the error as the service wrote it stays in
// the turn's detail, for whoever asks to see technical details.

var httpStatus = regexp.MustCompile(`\bHTTP (\d{3})\b`)

// Plain says an error the way a person who is not technical understands it, and what they can
// do about it. An error it does not recognise is kept as it is, which is better than a guess.
func Plain(msg string) string {
	low := strings.ToLower(msg)
	if m := httpStatus.FindStringSubmatch(msg); m != nil {
		switch code := m[1]; {
		case code == "401":
			return "the service did not accept the key or the sign-in: connect it again in Settings, Connections"
		case code == "403":
			return "the account is not allowed to do that: its permissions or its plan do not include it"
		case code == "404":
			return "the service did not find it"
		case code == "429":
			return "the service asks to wait a moment: too many requests in a short time"
		case code[0] == '5':
			return "the service is having trouble right now: try again in a while"
		}
	}
	switch {
	case strings.Contains(low, "not signed in"), strings.Contains(low, "could not be renewed"):
		return "the account is not connected any more: sign in again in Settings, Connections"
	case strings.Contains(low, "unauthorized"), strings.Contains(low, "invalid api key"), strings.Contains(low, "incorrect api key"), strings.Contains(low, "invalid_auth"):
		return "the service did not accept the key or the sign-in: connect it again in Settings, Connections"
	case strings.Contains(low, "context canceled"):
		return "it was stopped"
	case strings.Contains(low, "deadline exceeded"), strings.Contains(low, "timeout"), strings.Contains(low, "timed out"):
		return "the service took too long to answer: try again in a while"
	case strings.Contains(low, "connection refused"), strings.Contains(low, "no such host"), strings.Contains(low, "network is unreachable"), strings.Contains(low, "dial tcp"):
		return "the service could not be reached: check the internet connection, or that it is running"
	}
	return msg
}
