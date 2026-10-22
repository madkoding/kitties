package kitty

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// Avatar is the pixel cat that stands for a kitty in the interface: a breed (its shape and
// markings) and a coat colour. The drawing itself lives in the page; this is the choice.
type Avatar struct {
	Breed string `yaml:"breed" json:"breed"`
	Color string `yaml:"color" json:"color"`
}

// Breeds are the pixel cats the page can draw.
var Breeds = []string{"tabby", "shorthair", "tuxedo", "fluffy", "siamese"}

// Colors are the coats, by name; the page holds the palette.
var Colors = []string{"orange", "gray", "black", "white", "cream", "brown", "blue", "pink", "mint", "lilac"}

// DefaultAvatar picks a breed and a colour from the kitty's name, so two kitties rarely look alike.
func DefaultAvatar(name string) Avatar {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	n := h.Sum32()
	return Avatar{Breed: Breeds[n%uint32(len(Breeds))], Color: Colors[(n/7)%uint32(len(Colors))]}
}

// Normalize fills what is empty from the name and checks what is set.
func (a Avatar) Normalize(name string) (Avatar, error) {
	def := DefaultAvatar(name)
	out := Avatar{Breed: strings.ToLower(strings.TrimSpace(a.Breed)), Color: strings.ToLower(strings.TrimSpace(a.Color))}
	if out.Breed == "" {
		out.Breed = def.Breed
	}
	if out.Color == "" {
		out.Color = def.Color
	}
	if !contains(Breeds, out.Breed) {
		return Avatar{}, fmt.Errorf("avatar.breed: %q is not one of %s", out.Breed, strings.Join(Breeds, ", "))
	}
	if !contains(Colors, out.Color) {
		return Avatar{}, fmt.Errorf("avatar.color: %q is not one of %s", out.Color, strings.Join(Colors, ", "))
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
