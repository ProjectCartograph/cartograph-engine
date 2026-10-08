package evaluate

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client is a Server reached over MCP's streamable HTTP. It names itself
// in its User-Agent, as every client is named in the trace, so an
// evaluator's own calls are told apart from the agent's.
type Client struct {
	session *sdk.ClientSession
}

// Dial connects to an MCP endpoint (http://host:port/api/v1/mcp) as the
// client named name.
func Dial(ctx context.Context, endpoint, name string) (*Client, error) {
	hc := &http.Client{Transport: named{name: name, next: http.DefaultTransport}}
	c := sdk.NewClient(&sdk.Implementation{Name: name, Version: "1"}, nil)
	s, err := c.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: hc}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", endpoint, err)
	}
	return &Client{session: s}, nil
}

// Close ends the session.
func (c *Client) Close() error { return c.session.Close() }

// Instructions are the server's instructions, as an agent reads them on
// connecting.
func (c *Client) Instructions() string {
	if r := c.session.InitializeResult(); r != nil {
		return r.Instructions
	}
	return ""
}

// Tools are the server's tools, each a name and its description.
func (c *Client) Tools(ctx context.Context) ([][2]string, error) {
	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, [2]string{t.Name, t.Description})
	}
	return out, nil
}

// Call calls one tool and answers its text; a tool's refusal is an error
// with the text it refused with.
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	res, err := c.session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, content := range res.Content {
		if t, ok := content.(*sdk.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	if res.IsError {
		return "", fmt.Errorf("%s", b.String())
	}
	return b.String(), nil
}

// named sets a request's User-Agent to the client's name.
type named struct {
	name string
	next http.RoundTripper
}

func (n named) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", n.name+"/1 (Cartograph MCP)")
	return n.next.RoundTrip(r)
}
