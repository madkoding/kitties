package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Bridge speaks MCP over stdio (one JSON message per line, as clients that launch a command
// expect) and forwards each message to a server over Streamable HTTP. A message that cannot be
// delivered is answered with a JSON-RPC error, so the client never waits for nothing.
func Bridge(ctx context.Context, in io.Reader, out io.Writer, url, token string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxBody)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		reply, err := forward(ctx, client, url, token, line)
		if err != nil {
			reply = failure(line, err)
		}
		if len(reply) == 0 {
			continue
		}
		if _, werr := out.Write(append(reply, '\n')); werr != nil {
			return werr
		}
	}
	return sc.Err()
}

// forward posts one message and returns the answer (empty for a notification).
func forward(ctx context.Context, client *http.Client, url, token string, msg []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody*4))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil, nil
	case resp.StatusCode == http.StatusOK:
		return bytes.TrimSpace(compact(body)), nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("the gateway refused the token (401)")
	}
	return nil, fmt.Errorf("the gateway answered %d", resp.StatusCode)
}

// compact puts a JSON answer on one line, as the stdio transport needs.
func compact(b []byte) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return []byte(strings.Join(strings.Fields(string(b)), " "))
	}
	return buf.Bytes()
}

// failure is the JSON-RPC error for a message that went wrong; a notification gets none.
func failure(msg []byte, cause error) []byte {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(msg, &req) != nil || req.ID == nil {
		return nil
	}
	out, _ := json.Marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{codeInternalBridge, cause.Error()}})
	return out
}

// codeInternalBridge is the JSON-RPC "internal error" code, used for a gateway that cannot be reached.
const codeInternalBridge = -32603
