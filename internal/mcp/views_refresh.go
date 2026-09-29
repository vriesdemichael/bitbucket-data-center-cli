package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keeping a view current (ADR-101). A view draws what its result carries,
// then, while it is on screen, calls refresh_view with the show call it
// answers and the fingerprint of what it draws. The tool reads the view's data
// again and sends it only when the fingerprint has changed.
//
// MCP Apps offers the tool to views and not to the model (visibility "app"),
// so it takes nothing from the model's tool list or its attention.

// RefreshViewInput is the show call a view answers, and the fingerprint of the
// data it draws. The call's arguments stay at the top level, where the scope
// binds project and repo as it does for show.
type RefreshViewInput struct {
	ShowInput
	Since string `json:"since,omitempty" jsonschema:"The fingerprint of the data the view draws; the answer carries data only when it differs"`
}

// RefreshViewOutput says whether the view's data changed. The data itself, when
// it did, travels in the result's _meta, where show puts it.
type RefreshViewOutput struct {
	Changed     bool   `json:"changed" jsonschema:"True when the data differs from what the view draws; the result then carries it"`
	Fingerprint string `json:"fingerprint" jsonschema:"The fingerprint of the data as it is now"`
	GeneratedAt string `json:"generated_at" jsonschema:"When the data was read, as an RFC 3339 time"`
	// LimitReached is for kind pull_requests, as it is for show.
	LimitReached bool `json:"limit_reached" jsonschema:"True when a list stopped at limit, so there may be more than the view shows"`
}

func specRefreshView() Spec {
	tool := &mcp.Tool{
		Name: "refresh_view",
		Description: "Called by bb's views, not by the model: reads what a view shows, for a view to open it or to keep " +
			"current. With since, the fingerprint of the data a view draws, it answers with the data only when that differs.",
		Annotations: readOnly("Refresh a view"),
		InputSchema: refreshViewInputSchema(showKinds),
		Meta:        appOnlyToolMeta(),
	}
	titled(tool)

	return Spec{
		Tool: tool,
		Asks: AsksNever,
		// It goes with show, whatever --tools names: it is part of the views,
		// not of what the model is offered.
		Needs: []string{"show"},
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, refreshViewHandler(clients, allOffers()))
		},
		RegisterExposed: func(server *mcp.Server, opts ServerOptions, exposed map[string]bool) {
			offers := offersFor(opts, exposed)
			offered := *tool
			offered.InputSchema = refreshViewInputSchema(offers.Kinds)
			mcp.AddTool(server, &offered, refreshViewHandler(opts.Clients, offers))
		},
	}
}

func refreshViewInputSchema(kinds []string) *jsonschema.Schema {
	schema, err := jsonschema.For[RefreshViewInput](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving input schema for RefreshViewInput: %v", err))
	}
	shown := showInputSchema(kinds)
	for name, property := range shown.Properties {
		schema.Properties[name] = property
	}
	return schema
}

// appOnlyToolMeta keeps a tool from the model: MCP Apps offers it to the views
// of the page it names, and a host lists it for no model. The flat
// ui/resourceUri key is left out, so a host from before MCP Apps had
// visibility does not take the tool for one that shows a view.
func appOnlyToolMeta() mcp.Meta {
	return mcp.Meta{
		"ui": map[string]any{"resourceUri": viewURI, "visibility": []string{"app"}},
	}
}

func refreshViewHandler(c Clients, offers viewOffers) mcp.ToolHandlerFor[RefreshViewInput, RefreshViewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RefreshViewInput) (*mcp.CallToolResult, RefreshViewOutput, error) {
		if !offers.kind(in.Kind) {
			return nil, RefreshViewOutput{}, fmt.Errorf("refresh_view cannot refresh %q here; it refreshes %s", in.Kind, strings.Join(offers.Kinds, ", "))
		}
		if err := checkShowInput(in.ShowInput); err != nil {
			return nil, RefreshViewOutput{}, err
		}

		payload, people, summary, err := buildView(ctx, c, in.ShowInput, offers)
		if err != nil {
			return nil, RefreshViewOutput{}, fmt.Errorf("refresh failed: %w", err)
		}
		out := RefreshViewOutput{
			Changed:      in.Since == "" || payload.Fingerprint != in.Since,
			Fingerprint:  payload.Fingerprint,
			GeneratedAt:  payload.GeneratedAt,
			LimitReached: payload.LimitReached,
		}
		if !out.Changed {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
				Text: "Unchanged since the view's data was read.",
			}}}, out, nil
		}

		payload.Avatars = fetchAvatars(ctx, c, people)
		payload.Offers = &offers
		withHighlights(&payload, offers)
		// A view that holds nothing yet is opening this, rather than finding
		// what it holds changed.
		text := summary.changed()
		if in.Since == "" {
			text = summary.opened()
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
			Meta:    mcp.Meta{viewPayloadKey: payload},
		}, out, nil
	}
}
