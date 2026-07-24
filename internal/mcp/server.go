package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/agent-sync/agent-sync/internal/context"
	"github.com/agent-sync/agent-sync/internal/db"
)

type MCPServer struct {
	store *context.Store
	db    *db.DB
}

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

func NewServer(database *db.DB, store *context.Store) *MCPServer {
	return &MCPServer{db: database, store: store}
}

func (s *MCPServer) RunStdio() error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := s.handleRequest(req)
		data, _ := json.Marshal(resp)
		fmt.Println(string(data))
	}
	return scanner.Err()
}

func (s *MCPServer) handleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2025-03-26",
				"serverInfo": map[string]string{
					"name":    "agent-sync",
					"version": "0.1.0",
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			},
		}

	case "tools/list":
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": []MCPTool{
					{
						Name:        "save_context",
						Description: "Save a context entry with content, tags, and source",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"content": map[string]interface{}{"type": "string", "description": "Context content"},
								"summary": map[string]interface{}{"type": "string", "description": "Optional summary"},
								"source":  map[string]interface{}{"type": "string", "description": "Source identifier"},
								"tags":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							},
							"required": []string{"content"},
						},
					},
					{
						Name:        "recall_context",
						Description: "Search stored context entries by query",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{"type": "string", "description": "Search query"},
								"limit": map[string]interface{}{"type": "number", "description": "Max results"},
							},
							"required": []string{"query"},
						},
					},
					{
						Name:        "search_sessions",
						Description: "Search past sessions by query",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{"type": "string", "description": "Search query"},
								"limit": map[string]interface{}{"type": "number", "description": "Max results"},
							},
							"required": []string{"query"},
						},
					},
					{
						Name:        "list_sessions",
						Description: "List recent sessions with optional provider filter",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"provider": map[string]interface{}{"type": "string", "description": "Filter by provider"},
								"limit":    map[string]interface{}{"type": "number", "description": "Max results"},
							},
						},
					},
					{
						Name:        "get_stats",
						Description: "Get sync and context statistics",
						InputSchema: map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
					{
						Name:        "list_entities",
						Description: "List extracted entities, optionally filtered by type",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"type":  map[string]interface{}{"type": "string", "description": "Entity type filter (decision|fact|code_pattern|preference|goal)"},
								"limit": map[string]interface{}{"type": "number", "description": "Max results"},
							},
						},
					},
					{
						Name:        "list_conflicts",
						Description: "List detected entity conflicts",
						InputSchema: map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			},
		}

	case "tools/call":
		return s.handleToolCall(req)

	case "notifications/initialized":
		return MCPResponse{JSONRPC: "2.0"}

	default:
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

func (s *MCPServer) handleToolCall(req MCPRequest) MCPResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "Invalid params")
	}

	switch params.Name {
	case "save_context":
		var args struct {
			Content string   `json:"content"`
			Summary string   `json:"summary"`
			Source  string   `json:"source"`
			Tags    []string `json:"tags"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Source == "" {
			args.Source = "mcp"
		}
		entry, err := s.store.Save(args.Content, args.Summary, args.Source, "", args.Tags)
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"id":      entry.ID,
				"status":  "saved",
				"summary": entry.Summary,
			},
		}

	case "recall_context":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 10
		}
		entries, err := s.store.Search(args.Query, args.Limit)
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"entries": entries,
				"count":   len(entries),
			},
		}

	case "search_sessions":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 10
		}
		messages, err := s.db.SearchMessages(args.Query, args.Limit)
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"messages": messages,
				"count":    len(messages),
			},
		}

	case "list_sessions":
		var args struct {
			Provider string `json:"provider"`
			Limit    int    `json:"limit"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 20
		}
		sessions, err := s.db.ListSessions(args.Provider, args.Limit, 0)
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"sessions": sessions,
				"count":    len(sessions),
			},
		}

	case "get_stats":
		stats := s.db.GetStats()
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  stats,
		}

	case "list_entities":
		var args struct {
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 50
		}
		entities, err := s.db.ListEntities(args.Type, args.Limit, 0)
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"entities": entities,
				"count":    len(entities),
			},
		}

	case "list_conflicts":
		conflicts, err := s.db.ListConflicts()
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"conflicts": conflicts,
				"count":     len(conflicts),
			},
		}

	default:
		return errorResponse(req.ID, -32601, fmt.Sprintf("Tool not found: %s", params.Name))
	}
}

func errorResponse(id interface{}, code int, message string) MCPResponse {
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{
			Code:    code,
			Message: message,
		},
	}
}

func writeJSON(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

func (s *MCPServer) Log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	writeJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "log",
		"params": map[string]string{
			"message": msg,
		},
	})
}

func MCPStdioPath() string {
	return "agent-sync serve --mcp"
}

func (s *MCPServer) HandleToolCall(name string, argsJSON json.RawMessage) (interface{}, error) {
	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  mustMarshal(map[string]interface{}{"name": name, "arguments": argsJSON}),
	}
	resp := s.handleToolCall(req)
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	switch name {
	case "save_context":
		return resp.Result, nil
	case "recall_context":
		return resp.Result, nil
	default:
		return resp.Result, nil
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func (s *MCPServer) RunHTTP(address string) error {
	fmt.Fprintf(os.Stderr, "MCP HTTP server starting on %s\n", address)
	return fmt.Errorf("MCP HTTP mode not implemented yet, use stdio")
}

func (s *MCPServer) HasTool(name string) bool {
	toolNames := []string{"save_context", "recall_context", "search_sessions", "list_sessions", "get_stats", "list_entities", "list_conflicts"}
	for _, t := range toolNames {
		if t == name {
			return true
		}
	}
	return true

}
func (s *MCPServer) GetTools() []MCPTool {
	return []MCPTool{
		{Name: "save_context", Description: "Save a context entry"},
		{Name: "recall_context", Description: "Search stored context"},
		{Name: "search_sessions", Description: "Search past sessions"},
		{Name: "list_sessions", Description: "List recent sessions"},
		{Name: "get_stats", Description: "Get statistics"},
		{Name: "list_entities", Description: "List extracted entities"},
		{Name: "list_conflicts", Description: "List entity conflicts"},
	}
}

func (s *MCPServer) ConnectMCPConfig() map[string]interface{} {
	return map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"agent-sync": map[string]interface{}{
				"command": "agent-sync",
				"args":    []string{"serve", "--mcp"},
			},
		},
	}
}
func (s *MCPServer) HandleHTTP(w interface{}, r interface{}) {
}

func (s *MCPServer) ProcessToolCall(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "save_context":
		content, _ := args["content"].(string)
		summary, _ := args["summary"].(string)
		source, _ := args["source"].(string)
		tagsRaw, _ := args["tags"].([]interface{})
		var tags []string
		for _, t := range tagsRaw {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		entry, err := s.store.Save(content, summary, source, "", tags)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"id":      entry.ID,
			"status":  "saved",
			"summary": entry.Summary,
		}, nil

	case "recall_context":
		query, _ := args["query"].(string)
		limit, _ := args["limit"].(float64)
		entries, err := s.store.Search(query, int(limit))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"entries": entries,
			"count":   len(entries),
		}, nil

	case "search_sessions":
		query, _ := args["query"].(string)
		limit, _ := args["limit"].(float64)
		messages, err := s.db.SearchMessages(query, int(limit))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"messages": messages,
			"count":    len(messages),
		}, nil

	case "list_sessions":
		provider, _ := args["provider"].(string)
		limit, _ := args["limit"].(float64)
		sessions, err := s.db.ListSessions(provider, int(limit), 0)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"sessions": sessions,
			"count":    len(sessions),
		}, nil

	case "get_stats":
		return s.db.GetStats(), nil
	case "list_entities":
		eType, _ := args["type"].(string)
		limit, _ := args["limit"].(float64)
		entities, err := s.db.ListEntities(eType, int(limit), 0)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"entities": entities,
			"count":    len(entities),
		}, nil
	case "list_conflicts":
		conflicts, err := s.db.ListConflicts()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"conflicts": conflicts,
			"count":     len(conflicts),
		}, nil
	}

	return nil, fmt.Errorf("tool not found: %s", name)
}
func (s *MCPServer) CallTool(name string, args json.RawMessage) (interface{}, error) {
	return s.HandleToolCall(name, args)
}

func (s *MCPServer) HandleRequest(req MCPRequest) MCPResponse {
	return s.handleRequest(req)
}
func (s *MCPServer) ProcessRequest(req MCPRequest) MCPResponse {
	return s.handleRequest(req)
}
func (s *MCPServer) ServeStdio() error {
	return s.RunStdio()
}
func (s *MCPServer) Start() error {
	return s.RunStdio()
}
