package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// ErrInvalidKey is returned when the provider refuses the API key (HTTP 401 or 403).
var ErrInvalidKey = errors.New("the API key was rejected")

// ListModels asks the provider for the models a key can use. It is also the cheapest way to
// check the key: a provider answers it with 401/403 for a key it does not know, and it costs no
// tokens. rt replaces the HTTP transport when not nil (tests use it).
func ListModels(ctx context.Context, opts Options, rt http.RoundTripper) ([]string, error) {
	kind, ok := Lookup(opts.Kind)
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", opts.Kind)
	}
	if kind.Family == FamilyCLI {
		return nil, fmt.Errorf("%s has no model list", kind.Name)
	}
	if opts.APIKey == "" && kind.KeyEnv != "" {
		opts.APIKey = os.Getenv(kind.KeyEnv)
	}
	if opts.APIKey == "" && !kind.NoKey {
		return nil, fmt.Errorf("%w for %s: paste it first", ErrNoKey, kind.Name)
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = kind.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("provider %s needs a base_url", kind.Name)
	}
	if err := checkBase(base); err != nil {
		return nil, err
	}
	path, headers := "/models", map[string]string{}
	switch kind.Family {
	case FamilyAnthropic:
		path = "/v1/models?limit=1000"
		headers["x-api-key"] = opts.APIKey
		headers["anthropic-version"] = "2023-06-01"
	case FamilyGemini:
		path = "/v1beta/models?pageSize=1000"
		headers["x-goog-api-key"] = opts.APIKey
	default:
		if opts.APIKey != "" {
			headers["Authorization"] = "Bearer " + opts.APIKey
		}
	}
	for k, v := range opts.Headers {
		headers[k] = v
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: rt}
	if rt == nil {
		client.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kitties")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		// Gemini answers a malformed or unknown key with 400 API_KEY_INVALID.
		resp.StatusCode == http.StatusBadRequest && strings.Contains(string(data), "API_KEY_INVALID"):
		return nil, fmt.Errorf("%w by %s (HTTP %d): check that you copied it whole", ErrInvalidKey, kind.Name, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, &HTTPError{Code: resp.StatusCode, Body: string(data)}
	}
	return parseModels(kind.Family, data)
}

// checkBase refuses an address the model list should never be asked from: not http(s), with
// credentials in it, or a link-local IP (the cloud metadata service). The base_url comes from
// the page, so it is checked before anything is sent to it.
func checkBase(base string) error {
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return fmt.Errorf("the server address must start with http:// or https://")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("the server address is not valid")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && (ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return fmt.Errorf("the server address is not allowed")
	}
	return nil
}

// parseModels reads the ids out of a model list, sorted and without repeats.
func parseModels(family string, data []byte) ([]string, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name    string   `json:"name"`
			ID      string   `json:"id"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("the model list is not JSON: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range body.Data {
		add(m.ID)
	}
	for _, m := range body.Models { // Gemini names them models/<id>; Ollama's own list uses name
		if family == FamilyGemini && len(m.Methods) > 0 && !contains(m.Methods, "generateContent") {
			continue
		}
		id := m.ID
		if id == "" {
			id = strings.TrimPrefix(m.Name, "models/")
		}
		add(id)
	}
	sort.Strings(out)
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
