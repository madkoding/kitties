// Package soul gives each kitty its identity: who it is, how it speaks and what it cares about.
//
// A soul is a small document (YAML) that renders into the system prompt every conversation of the
// kitty starts with. The default soul is helpful, kind and adaptable, and a user's own soul only
// has to say what differs.
package soul

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/madkoding/kitties/internal/yamlx"
)

// Soul is the personality of one kitty.
type Soul struct {
	// Name is how the kitty calls itself. Empty takes the kitty's name.
	Name string `yaml:"name" json:"name,omitempty"`
	// Persona is one paragraph on who the kitty is ("a patient research assistant").
	Persona string `yaml:"persona" json:"persona,omitempty"`
	// Tone is how it speaks ("warm and concise").
	Tone string `yaml:"tone" json:"tone,omitempty"`
	// Traits are short adjectives or habits ("double-checks sources").
	Traits []string `yaml:"traits" json:"traits,omitempty"`
	// Instructions is free text appended verbatim to the prompt.
	Instructions string `yaml:"instructions" json:"instructions,omitempty"`
	// Language is the language the kitty answers in. Empty follows the user.
	Language string `yaml:"language" json:"language,omitempty"`
	// Skills names what the kitty is good at; they are listed to the model and to people.
	Skills []string `yaml:"skills" json:"skills,omitempty"`
}

// Default is the soul every kitty starts from: helpful, kind and adaptable.
func Default(name string) Soul {
	return Soul{
		Name:    name,
		Persona: "a helpful assistant who adapts to what the person in front of it needs",
		Tone:    "kind, clear and to the point",
		Traits:  []string{"helpful", "kind", "adaptable", "honest about what it does not know"},
	}
}

// Load reads a soul from a YAML file.
func Load(path string) (Soul, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Soul{}, fmt.Errorf("read the soul: %w", err)
	}
	return Parse(data)
}

// Parse reads a soul from YAML text.
func Parse(data []byte) (Soul, error) {
	m, err := yamlx.ParseYAML(data)
	if err != nil {
		return Soul{}, fmt.Errorf("the soul is not valid YAML: %w", err)
	}
	var s Soul
	if err := yamlx.Decode(m, &s); err != nil {
		return Soul{}, fmt.Errorf("the soul has a wrong field: %w", err)
	}
	return s, nil
}

// Merge returns base with every field that over sets.
func Merge(base, over Soul) Soul {
	out := base
	if over.Name != "" {
		out.Name = over.Name
	}
	if over.Persona != "" {
		out.Persona = over.Persona
	}
	if over.Tone != "" {
		out.Tone = over.Tone
	}
	if len(over.Traits) > 0 {
		out.Traits = append([]string(nil), over.Traits...)
	}
	if over.Instructions != "" {
		out.Instructions = over.Instructions
	}
	if over.Language != "" {
		out.Language = over.Language
	}
	if len(over.Skills) > 0 {
		out.Skills = append([]string(nil), over.Skills...)
	}
	return out
}

// Context is what the prompt says about where the kitty is and who it talks to.
type Context struct {
	// KittyName is the name the orchestrator gave this kitty.
	KittyName string
	// Desktop is true when the kitty runs inside its own container with a virtual display.
	Desktop bool
	// Workspace is the folder the kitty works in, inside its container.
	Workspace string
	// Group lists the other kitties in a group chat, empty in a one-to-one chat.
	Group []string
	// Connectors lists the external services the kitty may call, by name and description.
	Connectors map[string]string
	// User is how to refer to the person, empty for "the user".
	User string
	// Peers lists the other kitties this one may hand a subtask to.
	Peers []string
}

// SystemPrompt renders the soul into the system prompt of a conversation.
func (s Soul) SystemPrompt(c Context) string {
	name := s.Name
	if name == "" {
		name = c.KittyName
	}
	if name == "" {
		name = "Kitty"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a kitty: an autonomous agent of the Kitties framework.\n", name)
	if s.Persona != "" {
		fmt.Fprintf(&b, "You are %s.\n", strings.TrimSpace(s.Persona))
	}
	if s.Tone != "" {
		fmt.Fprintf(&b, "Your tone is %s.\n", strings.TrimSpace(s.Tone))
	}
	if len(s.Traits) > 0 {
		fmt.Fprintf(&b, "Your traits: %s.\n", strings.Join(s.Traits, ", "))
	}
	if len(s.Skills) > 0 {
		fmt.Fprintf(&b, "You are especially good at: %s.\n", strings.Join(s.Skills, ", "))
	}
	if s.Language != "" {
		fmt.Fprintf(&b, "Always answer in %s.\n", s.Language)
	} else {
		b.WriteString("Answer in the language the person writes in.\n")
	}
	user := c.User
	if user == "" {
		user = "the user"
	}
	fmt.Fprintf(&b, "Be helpful and kind to %s, and adapt to what they need.\n", user)
	if c.Desktop {
		b.WriteString("You run inside your own isolated Linux container with a virtual X display (Xvfb); " +
			"you can open graphical applications and a browser there, and nothing you do touches the host.\n")
	}
	if c.Workspace != "" {
		fmt.Fprintf(&b, "Your working folder is %s.\n", c.Workspace)
	}
	if len(c.Group) > 0 {
		fmt.Fprintf(&b, "You are in a group chat with other kitties: %s. ", strings.Join(c.Group, ", "))
		b.WriteString("Messages from them are labelled with their name. Work as a team: build on what " +
			"others said, do not repeat it, and say briefly when you have nothing to add. " +
			"Address a teammate with @name when a reply is for them.\n")
	}
	if len(c.Connectors) > 0 {
		b.WriteString("You can use these external services by writing a line of the form " +
			"@connector <name> <json arguments> on its own; the result comes back to you as a message:\n")
		names := make([]string, 0, len(c.Connectors))
		for n := range c.Connectors {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "- %s: %s\n", n, c.Connectors[n])
		}
	}
	if len(c.Peers) > 0 {
		fmt.Fprintf(&b, "Other kitties you can hand a subtask to: %s. Write @delegate <name> {\"task\": \"...\"} on its own line "+
			"with everything they need to know; their answer comes back to you and you go on. Use it for work another "+
			"kitty is better placed for, never for the whole task.\n", strings.Join(c.Peers, ", "))
	}
	if s.Instructions != "" {
		b.WriteString("\n" + strings.TrimSpace(s.Instructions) + "\n")
	}
	return b.String()
}
