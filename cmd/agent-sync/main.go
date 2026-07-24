package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/agent-sync/agent-sync/internal/api"
	"github.com/agent-sync/agent-sync/internal/config"
	"github.com/agent-sync/agent-sync/internal/context"
	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/internal/groups"
	mcp2 "github.com/agent-sync/agent-sync/internal/mcp"
	"github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/internal/sync/providers"
	"github.com/agent-sync/agent-sync/internal/tui"
	"github.com/agent-sync/agent-sync/pkg/types"
)

var (
	cfg    *config.Config
	dbase  *db.DB
	reg    *sync.Registry
	store     *context.Store
	merge     *context.MergeEngine
	extractor *context.Extractor
	groupsMgr *groups.Manager
	mcpSrv *mcp2.MCPServer
	apiSrv *api.Server
)

func main() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	dbase, err = db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}

	store = context.NewStore(dbase)
	merge = context.NewMergeEngine(dbase, store)
	extractor = context.NewExtractor(dbase)
	groupsMgr = groups.NewManager(dbase)

	reg = sync.NewRegistry(dbase)
	reg.InitDefaultProviders(providers.Config{
		DataDir:        cfg.DataDir,
		ClaudeCodePath: cfg.DefaultPath("claude-code"),
		OpenCodePath:   cfg.DefaultPath("opencode"),
	})

	mcpSrv = mcp2.NewServer(dbase, store)
	apiSrv = api.NewServer(dbase, reg, store, merge, groupsMgr, cfg)

	root := buildRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func buildRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-sync",
		Short: "Universal AI agent context sync & management",
		Long: `Agent Sync — sync, store, merge, and manage context across all your AI agents.

A universal tool that syncs chat histories from Claude Code, OpenCode, Cursor,
and other AI agents, stores a shared context layer, merges context across
sessions, and lets you manage everything via CLI, MCP, or web GUI.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	root.AddCommand(buildSyncCmd())
	root.AddCommand(buildListCmd())
	root.AddCommand(buildShowCmd())
	root.AddCommand(buildSearchCmd())
	root.AddCommand(buildContextCmd())
	root.AddCommand(buildGroupCmd())
	root.AddCommand(buildExportCmd())
	root.AddCommand(buildServeCmd())
	root.AddCommand(buildStatsCmd())
	root.AddCommand(buildConfigCmd())
	root.AddCommand(buildDetectCmd())
	root.AddCommand(buildTUICmd())
	root.AddCommand(buildSnapshotCmd())

	return root
}

func buildSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [provider]",
		Short: "Sync chat histories from AI agents",
		Long:  `Sync chat sessions from all detected providers, or a specific one.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			doExtract, _ := cmd.Flags().GetBool("extract")
			totalEntities := 0
			totalConflicts := 0

			providers := reg.List()
			if len(args) > 0 {
				for _, p := range providers {
					if string(p.Type()) != args[0] {
						continue
					}
					fmt.Printf("Syncing %s...\n", color.CyanString(p.Name()))
					stats, err := p.Sync()
					if err != nil {
						fmt.Printf("  Error: %v\n", err)
						continue
					}
					printSyncStats(stats)
					if doExtract {
						e, c := runExtraction(string(p.Type()))
						totalEntities += e
						totalConflicts += c
					}
					return
				}
				fmt.Printf("Provider %q not found\n", args[0])
				return
			}

			for _, p := range providers {
				ok, err := p.Detect()
				if err != nil || !ok {
					continue
				}
				fmt.Printf("Syncing %s...\n", color.CyanString(p.Name()))
				stats, err := p.Sync()
				if err != nil {
					fmt.Printf("  Error: %v\n", err)
					continue
				}
				printSyncStats(stats)
				if doExtract {
					e, c := runExtraction(string(p.Type()))
					totalEntities += e
					totalConflicts += c
				}
			}

			if doExtract {
				fmt.Printf("\nExtraction: %d entities, %d conflicts\n", totalEntities, totalConflicts)
			}
		},
	}
	cmd.Flags().Bool("extract", false, "Auto-extract entities after sync")
	return cmd
}

func buildListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List synced sessions",
		Long:  `List all synced chat sessions, optionally filtered by provider.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "sessions",
		Short: "List sessions",
		Run: func(cmd *cobra.Command, args []string) {
			provider, _ := cmd.Flags().GetString("provider")
			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = 50
			}
			sessions, err := dbase.ListSessions(provider, limit, 0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(sessions) == 0 {
				fmt.Println("No sessions found. Run 'agent-sync sync' first.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Sessions:"))
			for _, s := range sessions {
				provider := color.YellowString(string(s.Provider))
				title := s.Title
				if len(title) > 60 {
					title = title[:60] + "..."
				}
				msgCount := color.CyanString(fmt.Sprintf("%d msgs", s.MessageCount))
				fmt.Printf("  %s [%s] %s — %s — %s\n",
					s.ID[:8], provider, title, s.StartedAt.Format("Jan 02 15:04"), msgCount)
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "providers",
		Short: "List detected providers",
		Run: func(cmd *cobra.Command, args []string) {
			providers, err := dbase.ListProviders()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(providers) == 0 {
				fmt.Println("No providers configured. Run 'agent-sync detect' to find them.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Providers:"))
			for _, p := range providers {
				status := color.RedString("✗")
				if p.Enabled {
					status = color.GreenString("✓")
				}
				lastSync := "never"
				if p.LastSync != nil {
					lastSync = p.LastSync.Format("Jan 02 15:04")
				}
				fmt.Printf("  %s %s (%s) — %s — last sync: %s\n",
					status, color.CyanString(p.Name), p.Path, string(p.Type), lastSync)
			}
		},
	})
	cmd.PersistentFlags().String("provider", "", "Filter by provider")
	cmd.PersistentFlags().Int("limit", 50, "Max results")
	return cmd
}

func buildShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show session details and messages",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			session, err := dbase.GetSession(args[0])
			if err != nil {
				sessions, err2 := dbase.ListSessions("", 100, 0)
				if err2 != nil {
					fmt.Printf("Error: %v\n", err)
					return
				}
				for _, s := range sessions {
					if strings.HasPrefix(s.ID, args[0]) {
						session = &s
						break
					}
				}
				if session == nil {
					fmt.Printf("Session %q not found\n", args[0])
					return
				}
			}

			fmt.Printf("\n%s\n", color.BlueString("Session:"))
			fmt.Printf("  ID:       %s\n", session.ID)
			fmt.Printf("  Provider: %s\n", color.YellowString(string(session.Provider)))
			fmt.Printf("  Title:    %s\n", session.Title)
			fmt.Printf("  Model:    %s\n", session.Model)
			fmt.Printf("  Started:  %s\n", session.StartedAt.Format("Jan 02 2006 15:04:05"))
			fmt.Printf("  Messages: %d\n", session.MessageCount)

			messages, err := dbase.GetSessionMessages(session.ID)
			if err != nil {
				fmt.Printf("Error loading messages: %v\n", err)
				return
			}

			fmt.Printf("\n%s\n", color.BlueString("Messages:"))
			for _, m := range messages {
				roleColor := color.GreenString
				if m.Role == "assistant" {
					roleColor = color.CyanString
				}
				content := strings.TrimSpace(m.Content)
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				content = strings.ReplaceAll(content, "\n", " ")
				fmt.Printf("  [%s] %s\n", roleColor(m.Role), content)
			}
		},
	}
}

func buildSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across all messages and context",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query := strings.Join(args, " ")
			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = 20
			}

			messages, _ := dbase.SearchMessages(query, limit)
			entries, _ := store.Search(query, limit)

			if len(messages) > 0 {
				fmt.Printf("\n%s\n", color.BlueString("Messages:"))
				for _, m := range messages {
					session, _ := dbase.GetSession(m.SessionID)
					provider := "?"
					if session != nil {
						provider = string(session.Provider)
					}
					content := strings.TrimSpace(m.Content)
					if len(content) > 120 {
						content = content[:120] + "..."
					}
					content = strings.ReplaceAll(content, "\n", " ")
					fmt.Printf("  [%s] %s: %s\n",
						color.YellowString(provider),
						color.CyanString(m.Role),
						content)
				}
			}

			if len(entries) > 0 {
				fmt.Printf("\n%s\n", color.BlueString("Context entries:"))
				for _, e := range entries {
					summary := e.Summary
					if summary == "" {
						content := strings.TrimSpace(e.Content)
						if len(content) > 120 {
							summary = content[:120] + "..."
						} else {
							summary = content
						}
					}
					summary = strings.ReplaceAll(summary, "\n", " ")
					fmt.Printf("  [%s] %s\n", color.YellowString(e.Source), summary)
				}
			}

			if len(messages) == 0 && len(entries) == 0 {
				fmt.Println("No results found.")
			}
		},
	}
}

func buildContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage universal context entries",
		Long:  `Save, search, list, and merge context entries shared across agents.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "save <content>",
		Short: "Save a context entry",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			content := strings.Join(args, " ")
			source, _ := cmd.Flags().GetString("source")
			tags, _ := cmd.Flags().GetStringSlice("tags")
			if source == "" {
				source = "cli"
			}

			entry, err := store.Save(content, "", source, "", tags)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			fmt.Printf("Context saved: %s\n", color.GreenString(entry.ID[:8]))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "search <query>",
		Short: "Search context entries",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query := strings.Join(args, " ")
			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = 20
			}

			entries, err := store.Search(query, limit)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			if len(entries) == 0 {
				fmt.Println("No context entries found.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Context entries:"))
			for _, e := range entries {
				summary := e.Summary
				if summary == "" {
					content := strings.TrimSpace(e.Content)
					if len(content) > 100 {
						summary = content[:100] + "..."
					} else {
						summary = content
					}
				}
				summary = strings.ReplaceAll(summary, "\n", " ")
				tags := ""
				if len(e.Tags) > 0 {
					tags = fmt.Sprintf(" [%s]", strings.Join(e.Tags, ", "))
				}
				fmt.Printf("  %s [%s] %s%s\n",
					e.ID[:8], color.YellowString(e.Source), summary, color.CyanString(tags))
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all context entries",
		Run: func(cmd *cobra.Command, args []string) {
			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = 50
			}

			entries, err := store.List(limit, 0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			if len(entries) == 0 {
				fmt.Println("No context entries. Use 'agent-sync context save' to add one.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Context entries:"))
			for _, e := range entries {
				summary := e.Summary
				if summary == "" {
					content := strings.TrimSpace(e.Content)
					if len(content) > 100 {
						summary = content[:100] + "..."
					} else {
						summary = content
					}
				}
				summary = strings.ReplaceAll(summary, "\n", " ")
				fmt.Printf("  %s [%s] %s\n", e.ID[:8], color.YellowString(e.Source), summary)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "merge",
		Short: "Merge context entries into one",
		Run: func(cmd *cobra.Command, args []string) {
			ids, _ := cmd.Flags().GetStringSlice("ids")
			name, _ := cmd.Flags().GetString("name")
			strategy, _ := cmd.Flags().GetString("strategy")
			if len(ids) < 2 {
				fmt.Println("Need at least 2 entry IDs (--ids id1,id2,...)")
				return
			}
			if name == "" {
				name = fmt.Sprintf("merge-%d", len(ids))
			}
			if strategy == "" {
				strategy = "append"
			}

			merged, err := merge.Merge(ids, name, strategy)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			fmt.Printf("Merged %d entries → %s (%s)\n", len(ids), color.GreenString(merged.ID[:8]), strategy)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "merges",
		Short: "List merge history",
		Run: func(cmd *cobra.Command, args []string) {
			merges, err := merge.ListMerges()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(merges) == 0 {
				fmt.Println("No merges yet. Use 'agent-sync context merge' to create one.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Merge history:"))
			for _, m := range merges {
				fmt.Printf("  %s %s (%d entries, %s) — %s\n",
					m.ID[:8], color.CyanString(m.Name), len(m.ParentIDs), m.Strategy, m.CreatedAt.Format("Jan 02 15:04"))
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "extract [session-id]",
		Short: "Extract entities from sessions",
		Long:  "Run entity extraction on all sessions, or a specific one by ID.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sessions, err := dbase.ListSessions("", 1000, 0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(args) > 0 {
				for _, s := range sessions {
					if strings.HasPrefix(s.ID, args[0]) {
						messages, err := dbase.GetSessionMessages(s.ID)
						if err != nil {
							fmt.Printf("Error: %v\n", err)
							return
						}
						fmt.Printf("Extracting entities from %s...\n", color.CyanString(s.Title))
						result, err := extractor.ExtractFromMessages(&s, messages)
						if err != nil {
							fmt.Printf("Error: %v\n", err)
							return
						}
						fmt.Printf("  Found: %d entities, %d conflicts\n", len(result.Entities), len(result.Conflicts))
						return
					}
				}
				fmt.Printf("Session %q not found\n", args[0])
				return
			}
			total := 0
			conflictCount := 0
			for _, s := range sessions {
				messages, err := dbase.GetSessionMessages(s.ID)
				if err != nil {
					continue
				}
				result, err := extractor.ExtractFromMessages(&s, messages)
				if err != nil {
					continue
				}
				total += len(result.Entities)
				conflictCount += len(result.Conflicts)
			}
			fmt.Printf("Extraction complete: %d entities, %d conflicts\n", total, conflictCount)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "entities [type]",
		Short: "List extracted entities",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			eType := ""
			if len(args) > 0 {
				eType = args[0]
			}
			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = 50
			}
			entities, err := dbase.ListEntities(eType, limit, 0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(entities) == 0 {
				fmt.Println("No entities. Run 'agent-sync context extract' first.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString(fmt.Sprintf("Entities (%d):", len(entities))))
			for _, e := range entities {
				eType := color.YellowString(string(e.EntityType))
				summary := strings.ReplaceAll(e.Summary, "\n", " ")
				if len(summary) > 80 {
					summary = summary[:80] + "..."
				}
				fmt.Printf("  %s [%s] %s (%.0f%%)\n", e.ID[:8], eType, summary, e.Confidence*100)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "conflicts",
		Short: "List extracted entity conflicts",
		Run: func(cmd *cobra.Command, args []string) {
			conflicts, err := dbase.ListConflicts()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(conflicts) == 0 {
				fmt.Println("No conflicts detected.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString(fmt.Sprintf("Conflicts (%d):", len(conflicts))))
			for _, c := range conflicts {
				fmt.Printf("  %s %s\n", color.RedString("✗"), c.Evidence)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "graph",
		Short: "Show knowledge graph summary",
		Run: func(cmd *cobra.Command, args []string) {
			gq := context.NewGraphQuery(dbase)
			fmt.Println(gq.Summary())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "graph-tree <entity-id>",
		Short: "Show entity neighborhood as a tree",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			depth, _ := cmd.Flags().GetInt("depth")
			if depth <= 0 {
				depth = 2
			}
			gq := context.NewGraphQuery(dbase)
			fmt.Println(gq.TextTree(args[0], depth))
		},
	})

	cmd.PersistentFlags().String("source", "", "Source identifier")
	cmd.PersistentFlags().StringSlice("tags", nil, "Tags")
	cmd.PersistentFlags().Int("limit", 50, "Max results")
	cmd.PersistentFlags().StringSlice("ids", nil, "Entry IDs to merge")
	cmd.PersistentFlags().String("name", "", "Merge name")
	cmd.PersistentFlags().String("strategy", "append", "Merge strategy (append|dedup|summarize)")
	cmd.PersistentFlags().Int("depth", 2, "Tree traversal depth for graph-tree")

	return cmd
}

func buildGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage agent groups (selected-universal)",
		Long:  `Create and manage groups of agents that share context (selected-universal).`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new agent group",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			desc, _ := cmd.Flags().GetString("description")
			providers_, _ := cmd.Flags().GetStringSlice("providers")

			group, err := groupsMgr.Create(args[0], desc, providers_)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			fmt.Printf("Group created: %s (%s)\n", color.GreenString(group.Name), group.ID[:8])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List agent groups",
		Run: func(cmd *cobra.Command, args []string) {
			groups, err := groupsMgr.List()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(groups) == 0 {
				fmt.Println("No groups. Use 'agent-sync group create' to make one.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Agent Groups:"))
			for _, g := range groups {
				providers := strings.Join(g.ProviderIDs, ", ")
				if providers == "" {
					providers = "all"
				}
				fmt.Printf("  %s (%s) — %s\n",
					color.CyanString(g.Name), g.ID[:8], providers)
				if g.Description != "" {
					fmt.Printf("    %s\n", g.Description)
				}
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Delete an agent group",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			groups, _ := groupsMgr.List()
			for _, g := range groups {
				if g.ID == args[0] || g.Name == args[0] {
					if err := groupsMgr.Delete(g.ID); err != nil {
						fmt.Printf("Error: %v\n", err)
						return
					}
					fmt.Printf("Deleted group: %s\n", color.RedString(g.Name))
					return
				}
			}
			fmt.Printf("Group %q not found\n", args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add-context <group-name> <context-id>",
		Short: "Add a context entry to a group",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			groups, _ := groupsMgr.List()
			for _, g := range groups {
				if g.Name == args[0] {
					if err := groupsMgr.AddContextToGroup(g.ID, args[1]); err != nil {
						fmt.Printf("Error: %v\n", err)
						return
					}
					fmt.Printf("Context added to group %s\n", color.GreenString(g.Name))
					return
				}
			}
			fmt.Printf("Group %q not found\n", args[0])
		},
	})

	cmd.PersistentFlags().String("description", "", "Group description")
	cmd.PersistentFlags().StringSlice("providers", nil, "Provider IDs to include")

	return cmd
}

func buildExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <session-id> [format]",
		Short: "Export a session",
		Long:  `Export a session to JSON or markdown format.`,
		Args:  cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			format := "json"
			if len(args) > 1 {
				format = args[1]
			}

			session, err := dbase.GetSession(args[0])
			if err != nil {
				fmt.Printf("Session %q not found\n", args[0])
				return
			}

			messages, err := dbase.GetSessionMessages(session.ID)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			switch format {
			case "json":
				data, _ := json.MarshalIndent(map[string]interface{}{
					"session":  session,
					"messages": messages,
				}, "", "  ")
				fmt.Println(string(data))

			case "markdown", "md":
				fmt.Printf("# %s\n\n", session.Title)
				fmt.Printf("**Provider:** %s  \n", session.Provider)
				fmt.Printf("**Model:** %s  \n", session.Model)
				fmt.Printf("**Date:** %s  \n", session.StartedAt.Format("Jan 02 2006 15:04"))
				fmt.Println()
				for _, m := range messages {
					role := "**You**"
					if m.Role == "assistant" {
						role = "**Assistant**"
					}
					fmt.Printf("%s:\n\n%s\n\n", role, m.Content)
				}

			default:
				fmt.Printf("Unknown format: %s (use json, markdown)\n", format)
			}
		},
	}
}

func buildServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start servers (API, MCP, Web GUI)",
		Long:  `Start the API server, MCP server, and optionally the web GUI.`,
		Run: func(cmd *cobra.Command, args []string) {
			mcpMode, _ := cmd.Flags().GetBool("mcp")
			apiOnly, _ := cmd.Flags().GetBool("api")

			if mcpMode {
				fmt.Println(color.GreenString("Starting MCP server (stdio)..."))
				if err := mcpSrv.RunStdio(); err != nil {
					fmt.Fprintf(os.Stderr, "MCP error: %v\n", err)
				}
				return
			}

			if apiOnly {
				if err := apiSrv.ListenAndServe(); err != nil {
					fmt.Fprintf(os.Stderr, "API error: %v\n", err)
				}
				return
			}

			go func() {
				if err := apiSrv.ListenAndServe(); err != nil {
					fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
				}
			}()

			fmt.Printf("\n%s\n", color.GreenString("=== agent-sync ==="))
			fmt.Printf("  API:    http://localhost:%d\n", cfg.APIPort)
			fmt.Printf("  Config: %s\n", cfg.DataDir)
			fmt.Printf("\n%s\n", color.YellowString("Open your browser to the API URL above."))
			fmt.Printf("%s\n", color.YellowString("Press Ctrl+C to stop."))

			select {}
		},
	}

	cmd.Flags().Bool("mcp", false, "Run MCP server (stdio)")
	cmd.Flags().Bool("api", false, "Run API server only")

	return cmd
}

func buildStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show database statistics",
		Run: func(cmd *cobra.Command, args []string) {
			stats := dbase.GetStats()
			fmt.Printf("\n%s\n", color.BlueString("Agent Sync — Statistics"))
			fmt.Printf("  Sessions:           %d\n", stats["total_sessions"])
			fmt.Printf("  Messages:           %d\n", stats["total_messages"])
			fmt.Printf("  Context entries:    %d\n", stats["total_context_entries"])
			fmt.Printf("  Providers:          %d\n", stats["total_providers"])
			fmt.Printf("  Active providers:   %d\n", stats["active_providers"])
			fmt.Printf("  Total tokens:       %d\n", stats["total_tokens"])
		},
	}
}

func buildConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show configuration",
		Run: func(cmd *cobra.Command, args []string) {
			data, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(data))
		},
	}
}

func buildDetectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Detect available providers on this machine",
		Run: func(cmd *cobra.Command, args []string) {
			detected := reg.DetectAll()
			if len(detected) == 0 {
				fmt.Println("No providers detected.")
				fmt.Println("Make sure Claude Code, OpenCode, or Cursor is installed.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString("Detected providers:"))
			for _, p := range detected {
				fmt.Printf("  %s ✓\n", color.CyanString(p.Name()))
			}
		},
	}
}

func buildTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the terminal user interface",
		Long:  `Launch an interactive terminal UI to browse sessions, context, groups, and providers.`,
		Run: func(cmd *cobra.Command, args []string) {
			m := tui.New(dbase, reg, store, merge, groupsMgr)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func buildSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage context snapshots",
		Long:  "Create, list, and export context snapshots for tool handoff.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a snapshot from entities",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			desc, _ := cmd.Flags().GetString("description")
			entityIDs, _ := cmd.Flags().GetStringSlice("entity-ids")
			if len(entityIDs) == 0 {
				fmt.Println("Need at least one --entity-ids")
				return
			}
			snapshot := &types.Snapshot{
				ID:          db.NewID(),
				Name:        args[0],
				Description: desc,
				EntityIDs:   entityIDs,
				CreatedAt:   time.Now(),
			}
			if err := dbase.SaveSnapshot(snapshot); err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			fmt.Printf("Snapshot created: %s (%s)\n", color.GreenString(snapshot.Name), snapshot.ID[:8])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List snapshots",
		Run: func(cmd *cobra.Command, args []string) {
			snapshots, err := dbase.ListSnapshots()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(snapshots) == 0 {
				fmt.Println("No snapshots. Use 'agent-sync snapshot create' to make one.")
				return
			}
			fmt.Printf("\n%s\n", color.BlueString(fmt.Sprintf("Snapshots (%d):", len(snapshots))))
			for _, s := range snapshots {
				fmt.Printf("  %s %s — %d entities — %s\n",
					s.ID[:8], color.CyanString(s.Name), len(s.EntityIDs), s.CreatedAt.Format("Jan 02 2006"))
				if s.Description != "" {
					fmt.Printf("    %s\n", s.Description)
				}
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "export <id>",
		Short: "Export a snapshot as a compact handoff prompt",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			snapshots, err := dbase.ListSnapshots()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			var snapshot *types.Snapshot
			for _, s := range snapshots {
				if s.ID == args[0] || strings.HasPrefix(s.ID, args[0]) {
					snapshot = &s
					break
				}
			}
			if snapshot == nil {
				fmt.Printf("Snapshot %q not found\n", args[0])
				return
			}

			entities, err := dbase.ListEntities("", 1000, 0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			entityMap := make(map[string]types.Entity)
			for _, e := range entities {
				entityMap[e.ID] = e
			}

			fmt.Printf("# Context Snapshot: %s\n\n", snapshot.Name)
			if snapshot.Description != "" {
				fmt.Printf("%s\n\n", snapshot.Description)
			}
			fmt.Printf("## Key Context\n\n")

			for _, eid := range snapshot.EntityIDs {
				e, ok := entityMap[eid]
				if !ok {
					continue
				}
				switch e.EntityType {
				case types.EntityDecision:
					fmt.Printf("- **Decision**: %s\n", e.Summary)
				case types.EntityFact:
					fmt.Printf("- **Fact**: %s\n", e.Summary)
				case types.EntityPreference:
					fmt.Printf("- **Preference**: %s\n", e.Summary)
				case types.EntityGoal:
					fmt.Printf("- **Goal**: %s\n", e.Summary)
				case types.EntityCode:
					fmt.Printf("- **Code pattern**: %s\n", e.Summary)
				default:
					fmt.Printf("- %s\n", e.Summary)
				}
			}

			conflicts, _ := dbase.ListConflicts()
			if len(conflicts) > 0 {
				fmt.Printf("\n## Active Conflicts\n\n")
				for _, c := range conflicts {
					fmt.Printf("- ⚠️ %s\n", c.Evidence)
				}
			}

			fmt.Printf("\n---\n*Exported from agent-sync on %s*\n", time.Now().Format("Jan 02 2006 15:04"))
		},
	})

	cmd.PersistentFlags().String("description", "", "Snapshot description")
	cmd.PersistentFlags().StringSlice("entity-ids", nil, "Entity IDs to include")

	return cmd
}

func printSyncStats(stats *types.SyncStats) {
	if stats == nil {
		return
	}
	fmt.Printf("  Found: %d sessions, %d new, %d new messages\n",
		stats.SessionsFound, stats.SessionsNew, stats.MessagesNew)
}

func runExtraction(source string) (int, int) {
	sessions, err := dbase.ListSessions(source, 1000, 0)
	if err != nil {
		return 0, 0
	}
	total := 0
	for _, s := range sessions {
		messages, err := dbase.GetSessionMessages(s.ID)
		if err != nil {
			continue
		}
		result, err := extractor.ExtractFromMessages(&s, messages)
		if err != nil {
			continue
		}
		total += len(result.Entities)
	}
	graph := context.NewGraphQuery(dbase)
	_, _ = graph.BuildGraph()
	conflicts, _ := dbase.ListConflicts()
	return total, len(conflicts)
}
