package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

// ReportsChat makes fakeChats a Reporter: the lead's Reports thread is "reports-<id>".
func (c *fakeChats) ReportsChat(lead *kitty.Kitty) (string, error) {
	id := "reports-" + lead.ID
	if _, ok := c.sessions[id]; !ok {
		c.sessions[id] = &chat.Session{ID: id, Title: chat.ReportsTitle, Members: []string{lead.ID}, Reports: true}
	}
	return id, nil
}

// HandOverDays makes fakeChats count hand-overs per day: one today.
func (c *fakeChats) HandOverDays(n int, now time.Time) []chat.HandOverDay {
	out := make([]chat.HandOverDay, n)
	out[n-1] = chat.HandOverDay{Day: now.Format("2006-01-02"), Count: 1}
	return out
}

func TestLeadRoutes(t *testing.T) {
	srv, f, _ := setup(t)
	f.kitties["b1"] = &kitty.Kitty{ID: "b1", Name: "bo", Status: kitty.StatusRunning}
	if code, m, _ := call(t, srv, "GET", "/v1/lead", "tok", nil); code != 200 || m["lead"] != nil {
		t.Fatalf("no lead = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/lead", "", nil); code != 401 {
		t.Error("the lead needs the token")
	}
	code, m, _ := call(t, srv, "PUT", "/v1/lead", "tok", map[string]string{"kitty": "ana"})
	if lead, _ := m["lead"].(map[string]any); code != 200 || lead["name"] != "ana" || m["reports"] != "reports-a1" {
		t.Fatalf("set = %d %v", code, m)
	}
	// The lead's chart: the team's hand-overs of the last two weeks, a day each.
	if _, m, _ := call(t, srv, "GET", "/v1/lead", "tok", nil); len(m["handover_days"].([]any)) != chartDays {
		t.Errorf("days = %v", m["handover_days"])
	}
	// Another lead takes it from the first.
	call(t, srv, "PUT", "/v1/lead", "tok", map[string]string{"kitty": "bo"})
	if f.kitties["a1"].Lead || !f.kitties["b1"].Lead {
		t.Errorf("one lead: ana %v bo %v", f.kitties["a1"].Lead, f.kitties["b1"].Lead)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/lead", "tok", map[string]string{"kitty": "zed"}); code != 404 {
		t.Errorf("unknown kitty = %d", code)
	}
	if code, m, _ := call(t, srv, "PUT", "/v1/lead", "tok", map[string]string{"kitty": ""}); code != 200 || m["lead"] != nil || f.kitties["b1"].Lead {
		t.Errorf("cleared = %d %v", code, m)
	}
	// The kitty's own edit sets it too.
	if code, m, _ := call(t, srv, "PATCH", "/v1/kitties/ana", "tok", map[string]any{"lead": true}); code != 200 || m["kitty"].(map[string]any)["lead"] != true {
		t.Errorf("patch = %d %v", code, m)
	}
}

func TestMCPLeadTools(t *testing.T) {
	s, f, c := mcpServer(false)
	f.kitties["b1"] = &kitty.Kitty{ID: "b1", Name: "bo"}
	if text, isErr, _ := tool(t, s, "lead_ask", map[string]any{"text": "do it"}); !isErr || !strings.Contains(text, "no lead") {
		t.Errorf("ask without a lead = %q %v", text, isErr)
	}
	if _, isErr, resp := tool(t, s, "chat_create", map[string]any{}); !isErr || !strings.Contains(fmt.Sprint(resp), "members is required") {
		t.Errorf("chat with nobody = %v", resp)
	}
	if text, isErr, _ := tool(t, s, "fleet_lead_set", map[string]any{"kitty": "bo"}); isErr || !strings.Contains(text, `"reports": "reports-b1"`) {
		t.Fatalf("set = %q %v", text, isErr)
	}
	var view struct {
		Lead    *kitty.Kitty `json:"lead"`
		Reports string       `json:"reports"`
	}
	text, _, _ := tool(t, s, "fleet_lead", map[string]any{})
	if err := json.Unmarshal([]byte(text), &view); err != nil || view.Lead == nil || view.Lead.Name != "bo" {
		t.Fatalf("fleet_lead = %q %v", text, err)
	}
	if text, isErr, _ := tool(t, s, "lead_ask", map[string]any{"text": "how is it going?"}); isErr || !strings.Contains(text, "re: how is it going?") || !strings.Contains(text, "reports-b1") {
		t.Errorf("ask = %q %v", text, isErr)
	}
	// A chat with no members is a chat with the lead.
	if text, isErr, _ := tool(t, s, "chat_create", map[string]any{"title": "plan"}); isErr || !strings.Contains(text, `"b1"`) {
		t.Errorf("chat with the lead = %q %v", text, isErr)
	}
	_ = c
	// Claude changes a kitty's rounds as the lead does, within the same cap.
	f.kitties["a1"].Limits = kitty.Limits{MaxTokens: 7}
	if text, isErr, _ := tool(t, s, "kitty_set_limits", map[string]any{"ref": "ana", "max_rounds": 40, "max_seconds": 900}); isErr || f.kitties["a1"].Limits.MaxRounds != 40 || f.kitties["a1"].Limits.MaxSeconds != 900 || f.kitties["a1"].Limits.MaxTokens != 7 {
		t.Errorf("limits = %q %v %+v", text, isErr, f.kitties["a1"].Limits)
	}
	for _, bad := range []map[string]any{{"ref": "ana", "max_rounds": 501}, {"ref": "ana", "max_tokens": 2000001}, {"ref": "ana", "max_seconds": 7201}, {"ref": "ana"}, {"ref": "zed", "max_rounds": 3}} {
		if _, isErr, _ := tool(t, s, "kitty_set_limits", bad); !isErr {
			t.Errorf("%v accepted", bad)
		}
	}
}
