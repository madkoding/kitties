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
	// Voice is the speech voice its voice notes are read with (alloy, nova, …); empty or
	// "default" takes the gateway's.
	Voice string `yaml:"voice" json:"voice,omitempty"`
}

// Default is the soul every kitty starts from: helpful, kind and adaptable.
func Default(name string) Soul {
	return Soul{
		Name:    name,
		Persona: "a helpful assistant who adapts to what the person in front of it needs",
		Tone:    "warm and simple: everyday words, short and to the point",
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
	if over.Voice != "" {
		out.Voice = over.Voice
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
	// Unattended is true when the kitty works on its own: it is told to see a task through.
	Unattended bool
	// Lead is true for the principal kitty, the one that orchestrates the fleet: it is told to
	// split the person's requests among the others and report back for all of them.
	Lead bool
	// Team describes the other kitties for the lead, one line each (name, what it is good at,
	// whether it has a computer, how it is).
	Team []string
	// LeadName names the fleet's lead for the other kitties, empty when there is none or this
	// kitty is it.
	LeadName string
}

// leadWords is what the principal kitty is told about its role.
const leadWords = "You are the lead kitty: the person's main contact and the orchestrator of the whole fleet. " +
	"When they ask for something, decide who does what: do yourself what is quick or yours, and hand each piece " +
	"another kitty is better placed for to it (@delegate <name> {\"task\": \"...\"}, with everything it needs to know). " +
	"Several pieces may go to several kitties in the same reply: they work on them at the same time. Gather their answers, check them, and give the person one clear report " +
	"of the whole: what was done, by whom, what failed and what is left. The other kitties' reports (goals they " +
	"finished or could not, help they need) reach you in your Reports thread; when the person asks how things are " +
	"going, answer from them. When a kitty needs more for a long task you hand it (rounds, tokens or seconds), " +
	"give them for that task alone with delegate (max_rounds, max_tokens, max_seconds); to change them for good, use " +
	"@task_limits <name> {\"max_rounds\": N}. Say so in your report.\n"

// persistWords is what an unattended kitty is told about finishing what it was asked. It is
// static text, so it sits in the cacheable start of the prompt.
const persistWords = "Work on your own until the whole task is done. Before you start, list in your reply every " +
	"item the task covers (every group, page, file, account, ticket) and keep that list as you go, ticking items " +
	"off; when a request names a set (\"all\", \"each\", a number) you do every one, not a sample. Never stop at a " +
	"halfway point to ask \"should I continue?\": the person already asked you to finish. Ask only when you are truly " +
	"blocked (a login, a code only they get, a choice that changes the goal or cannot be undone), say exactly what " +
	"you need, and carry on with the rest meanwhile. If an item fails, note it, move to the next and report it at the " +
	"end. If you are told you were cut off, read your list and continue from the first item not done. " +
	"Finish with a short report of every item: done, failed, or left.\n"

// plainWords is how every kitty speaks unless its own instructions say otherwise: to someone
// who is not technical. The machinery (connectors, JSON, containers) is for the kitty, not for
// the person.
const plainWords = "Speak to %s as to a friend who is not technical: everyday words, short sentences, no jargon. " +
	"Do not mention tools, connectors, commands, code, JSON, APIs, tokens, containers, coordinates, file paths or " +
	"error codes unless they ask; say what you did and what it means for them (\"I posted it on X\", not the request " +
	"you made). When something fails, say in plain words what happened and what they can do about it. " +
	"Give technical detail only when they ask for it, or when they clearly speak that way themselves.\n"

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
	fmt.Fprintf(&b, plainWords, user)
	if c.Desktop {
		b.WriteString("You run inside your own isolated Linux container with a virtual X display (Xvfb); " +
			"you can open graphical applications and a browser there, and nothing you do touches the host. " +
			"To the person, it is simply your own computer.\n")
		b.WriteString("Your home (/home/kitty) outlives updates of that computer: your settings stay, and so do programs you " +
			"install for yourself in ~/.local or /opt/kitty-apps; Debian packages come back by themselves " +
			"(kitty-install <package>, kitty-install --remove <package>). " +
			"To give the person a file you made (a document, a picture, a spreadsheet), put it in ~/Shared " +
			"(kitty-share <file>): they see it in the app, under Library > Shared, and can preview and download it. " +
			"Tell them its name when you do.\n")
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
	if c.Lead {
		b.WriteString(leadWords)
		if len(c.Team) > 0 {
			b.WriteString("Your team:\n")
			for _, line := range c.Team {
				fmt.Fprintf(&b, "- %s\n", line)
			}
		}
	} else if c.LeadName != "" {
		fmt.Fprintf(&b, "%s is the lead kitty: it coordinates the fleet and reports to %s for everyone, so a task it "+
			"hands you comes from the person; answer it fully, as you would answer them.\n", c.LeadName, user)
	}
	if len(c.Peers) > 0 && !c.Lead {
		fmt.Fprintf(&b, "Other kitties you can hand a subtask to: %s. Write @delegate <name> {\"task\": \"...\"} on its own line "+
			"with everything they need to know; their answer comes back to you and you go on. Use it for work another "+
			"kitty is better placed for, never for the whole task.\n", strings.Join(c.Peers, ", "))
	}
	if c.Unattended {
		b.WriteString(persistWords)
		if c.Workspace != "" {
			b.WriteString("For a long task keep that list in a file called TODO.md in your working folder and update it as you " +
				"go; if you start with no memory of the task, read TODO.md first and continue from it.\n")
		}
	}
	if s.Instructions != "" {
		b.WriteString("\n" + strings.TrimSpace(s.Instructions) + "\n")
	}
	return b.String()
}
