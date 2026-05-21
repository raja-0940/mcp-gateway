package broker

import (
	"context"
	"slices"

	"github.com/Kuadrant/mcp-gateway/internal/broker/upstream"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	listTagsName          = "list_tags"
	filterToolsByTagsName = "filter_tools_by_tags"
)

func (m *mcpBrokerImpl) registerTagsTools() {
	m.listeningMCPServer.AddTool(
		mcp.NewTool(listTagsName,
			mcp.WithDescription("List all tags across registered MCP servers"),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return m.handleListTags(req)
		},
	)

	m.listeningMCPServer.AddTool(
		mcp.NewTool(filterToolsByTagsName,
			mcp.WithDescription("Return tools available through the gateway that match all of the given tags"),
			mcp.WithArray("tags",
				mcp.Description("list of tags to filter by (must not be empty)"),
				mcp.Required(),
			),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return m.handleFilterToolsByTags(req)
		},
	)
}

func (m *mcpBrokerImpl) handleListTags(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	m.mcpLock.RLock()
	visible := m.getVisibleToolNames(req.Header)
	seen := make(map[string]struct{})
	for _, mgr := range m.mcpServers {
		cfg := mgr.Config()
		if len(m.visibleToolNames(cfg.Prefix, mgr, visible)) == 0 {
			continue
		}
		for _, tag := range cfg.Tags {
			seen[tag] = struct{}{}
		}
	}
	m.mcpLock.RUnlock()

	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	slices.Sort(tags)

	return m.marshalToolResult(tags), nil
}

func (m *mcpBrokerImpl) handleFilterToolsByTags(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	rawTags, ok := args["tags"]
	if !ok {
		return mcp.NewToolResultError("missing required parameter: tags"), nil
	}

	rawSlice, ok := rawTags.([]any)
	if !ok {
		return mcp.NewToolResultError("tags must be an array"), nil
	}

	if len(rawSlice) == 0 {
		return mcp.NewToolResultError("tags must not be empty"), nil
	}

	filterTags := make([]string, 0, len(rawSlice))
	for _, v := range rawSlice {
		s, ok := v.(string)
		if !ok {
			return mcp.NewToolResultError("tags must be an array of strings"), nil
		}
		filterTags = append(filterTags, s)
	}

	type serverRef struct {
		tags   []string
		prefix string
		server upstream.ActiveMCPServer
	}
	m.mcpLock.RLock()
	visible := m.getVisibleToolNames(req.Header)
	refs := make([]serverRef, 0, len(m.mcpServers))
	for _, mgr := range m.mcpServers {
		cfg := mgr.Config()
		refs = append(refs, serverRef{
			tags:   cfg.Tags,
			prefix: cfg.Prefix,
			server: mgr,
		})
	}
	m.mcpLock.RUnlock()

	matched := make([]mcp.Tool, 0)
	for _, ref := range refs {
		if !hasAllTags(ref.tags, filterTags) {
			continue
		}
		for _, tool := range ref.server.GetManagedTools() {
			t := tool
			t.Name = ref.prefix + t.Name
			if _, ok := visible[t.Name]; !ok {
				continue
			}
			matched = append(matched, t)
		}
	}

	return m.marshalToolResult(matched), nil
}

func hasAllTags(serverTags, required []string) bool {
	for _, r := range required {
		if !slices.Contains(serverTags, r) {
			return false
		}
	}
	return true
}
