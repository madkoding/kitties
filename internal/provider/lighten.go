package provider

import (
	"errors"
	"fmt"
	"strings"
)

// A desktop task sends a screenshot after every round, and each one stays in the conversation:
// after a few dozen rounds the request carries tens of megabytes of base64, and providers (or the
// proxies in front of them) cut it off and answer "failed to read request body". The model needs
// the last screens, not all of them, so the older pictures go as a line of text.

const (
	// KeepImages is how many of a conversation's last pictures go as pictures. The cut moves in
	// steps of this size, so between steps the start of the conversation stays the same and the
	// provider's prompt cache keeps serving it: between KeepImages and 2*KeepImages-1 are sent.
	KeepImages = 6
	// BodyBudget is how many bytes of pictures and documents one request carries at most (before
	// base64, which adds a third): well under the 20 to 32 MB that providers take.
	BodyBudget = 12 << 20
)

// lightKeep and lightBudget are the limits of the try after a provider could not read a body.
const (
	lightKeep   = 2
	lightBudget = BodyBudget / 4
)

// leftOut is what replaces the pictures and documents that are not sent.
func leftOut(images, files int) string {
	var what []string
	if images > 0 {
		what = append(what, fmt.Sprintf("%d earlier picture(s)", images))
	}
	if files > 0 {
		what = append(what, fmt.Sprintf("%d earlier document(s)", files))
	}
	return "(" + strings.Join(what, " and ") + " left out to keep the request light)"
}

// lighten returns messages with only their last pictures (keep, in steps of keep) and with at
// most budget bytes of pictures and documents, newest first. The newest picture and the last
// message's documents always go, however big. messages itself is not changed.
func lighten(messages []Message, keep, budget int) []Message {
	total := 0
	for _, m := range messages {
		total += len(m.Images)
	}
	// cut is the ordinal of the first picture sent; it only moves every keep pictures.
	cut := 0
	if keep > 0 && total > keep {
		cut = (total - keep) / keep * keep
	}
	out := make([]Message, len(messages))
	copy(out, messages)
	spent, seen, changed := 0, total, false
	for i := len(out) - 1; i >= 0; i-- {
		m := out[i]
		if len(m.Images) == 0 && len(m.Files) == 0 {
			continue
		}
		var images []Image
		droppedImages := 0
		for j := len(m.Images) - 1; j >= 0; j-- {
			seen--
			img := m.Images[j]
			first := seen == total-1 // the newest picture
			if !first && (seen < cut || spent+len(img.Data) > budget) {
				droppedImages++
				continue
			}
			spent += len(img.Data)
			images = append([]Image{img}, images...)
		}
		var files []File
		droppedFiles := 0
		for j := len(m.Files) - 1; j >= 0; j-- {
			f := m.Files[j]
			if i != len(out)-1 && spent+len(f.Data) > budget {
				droppedFiles++
				continue
			}
			spent += len(f.Data)
			files = append([]File{f}, files...)
		}
		if droppedImages == 0 && droppedFiles == 0 {
			continue
		}
		changed = true
		m.Images, m.Files = images, files
		note := leftOut(droppedImages, droppedFiles)
		if m.Content == "" {
			m.Content = note
		} else {
			m.Content += "\n" + note
		}
		out[i] = m
	}
	if !changed {
		return messages
	}
	return out
}

// bodyUnread reports whether a provider answered that it could not read the request's body: a
// request too big for it, or cut off on the way. Sent again lighter, it usually goes through.
func bodyUnread(err error) bool {
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	if he.Code == 413 {
		return true
	}
	if he.Code != 400 {
		return false
	}
	low := strings.ToLower(he.Body)
	return strings.Contains(low, "failed to read request body") ||
		strings.Contains(low, "request body") && (strings.Contains(low, "too large") || strings.Contains(low, "unexpected eof")) ||
		strings.Contains(low, "request entity too large") || strings.Contains(low, "payload too large")
}
