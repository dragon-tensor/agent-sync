package context

import (
	"fmt"
	"sort"
	"strings"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type GraphNode struct {
	Entity     types.Entity `json:"entity"`
	Neighbors  []GraphEdge  `json:"neighbors"`
	Degree     int          `json:"degree"`
}

type GraphEdge struct {
	TargetID    string  `json:"target_id"`
	TargetName  string  `json:"target_name"`
	RelationType string `json:"relation_type"`
	Weight      float64 `json:"weight"`
	Evidence    string  `json:"evidence,omitempty"`
}

type KnowledgeGraph struct {
	Nodes map[string]*GraphNode `json:"nodes"`
	Edges []GraphEdge          `json:"edges"`
}

type GraphQuery struct {
	db *db.DB
}

func NewGraphQuery(database *db.DB) *GraphQuery {
	return &GraphQuery{db: database}
}

func (g *GraphQuery) BuildGraph() (*KnowledgeGraph, error) {
	entities, err := g.db.ListEntities("", 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}

	graph := &KnowledgeGraph{
		Nodes: make(map[string]*GraphNode),
	}

	entityMap := make(map[string]types.Entity)
	for _, e := range entities {
		entityMap[e.ID] = e
		graph.Nodes[e.ID] = &GraphNode{
			Entity:    e,
			Neighbors: []GraphEdge{},
		}
	}

	seenEdge := make(map[string]bool)
	for _, e := range entities {
		relations, err := g.db.ListEntityRelations(e.ID)
		if err != nil {
			continue
		}
		for _, rel := range relations {
			targetID := rel.TargetEntityID
			if targetID == e.ID {
				targetID = rel.SourceEntityID
			}
			target, ok := entityMap[targetID]
			if !ok {
				continue
			}
			edge := GraphEdge{
				TargetID:     targetID,
				TargetName:   target.Name,
				RelationType: rel.RelationType,
				Weight:       rel.Weight,
				Evidence:     rel.Evidence,
			}
			if node, ok := graph.Nodes[e.ID]; ok {
				node.Neighbors = append(node.Neighbors, edge)
				node.Degree = len(node.Neighbors)
			}
			// Count each relation once in the global edge list.
			if !seenEdge[rel.ID] {
				seenEdge[rel.ID] = true
				graph.Edges = append(graph.Edges, edge)
			}
		}
	}

	return graph, nil
}

func (g *GraphQuery) GetNeighbors(entityID string) ([]GraphEdge, error) {
	relations, err := g.db.ListEntityRelations(entityID)
	if err != nil {
		return nil, err
	}

	entities, err := g.db.ListEntities("", 10000, 0)
	if err != nil {
		return nil, err
	}
	entityMap := make(map[string]types.Entity)
	for _, e := range entities {
		entityMap[e.ID] = e
	}

	var edges []GraphEdge
	seen := make(map[string]bool)
	for _, rel := range relations {
		targetID := rel.TargetEntityID
		if targetID == entityID {
			targetID = rel.SourceEntityID
		}
		if seen[targetID] {
			continue
		}
		seen[targetID] = true
		target, ok := entityMap[targetID]
		if !ok {
			continue
		}
		edges = append(edges, GraphEdge{
			TargetID:     targetID,
			TargetName:   target.Name,
			RelationType: rel.RelationType,
			Weight:       rel.Weight,
			Evidence:     rel.Evidence,
		})
	}
	return edges, nil
}

func (g *GraphQuery) FindPath(sourceID, targetID string, maxDepth int) ([][]GraphEdge, error) {
	if maxDepth <= 0 {
		maxDepth = 4
	}

	graph, err := g.BuildGraph()
	if err != nil {
		return nil, err
	}

	if _, ok := graph.Nodes[sourceID]; !ok {
		return nil, fmt.Errorf("source entity not found")
	}
	if _, ok := graph.Nodes[targetID]; !ok {
		return nil, fmt.Errorf("target entity not found")
	}

	type pathNode struct {
		edges []GraphEdge
		lastID string
	}

	var paths [][]GraphEdge
	visited := map[string]bool{sourceID: true}
	queue := []pathNode{{edges: []GraphEdge{}, lastID: sourceID}}

	for len(queue) > 0 && len(paths) < 5 {
		current := queue[0]
		queue = queue[1:]

		if len(current.edges) >= maxDepth {
			continue
		}

		node, ok := graph.Nodes[current.lastID]
		if !ok {
			continue
		}

		for _, edge := range node.Neighbors {
			if visited[edge.TargetID] {
				continue
			}

			newPath := append([]GraphEdge{}, current.edges...)
			newPath = append(newPath, edge)

			if edge.TargetID == targetID {
				paths = append(paths, newPath)
				continue
			}

			visited[edge.TargetID] = true
			queue = append(queue, pathNode{edges: newPath, lastID: edge.TargetID})
		}
	}

	return paths, nil
}

func (g *GraphQuery) GetClusters(minSize int) ([][]types.Entity, error) {
	graph, err := g.BuildGraph()
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	var clusters [][]types.Entity

	for id := range graph.Nodes {
		if visited[id] {
			continue
		}

		cluster := []types.Entity{}
		queue := []string{id}
		visited[id] = true

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			node := graph.Nodes[current]
			cluster = append(cluster, node.Entity)

			for _, edge := range node.Neighbors {
				if !visited[edge.TargetID] {
					visited[edge.TargetID] = true
					queue = append(queue, edge.TargetID)
				}
			}
		}

		if len(cluster) >= minSize {
			clusters = append(clusters, cluster)
		}
	}

	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i]) > len(clusters[j])
	})

	return clusters, nil
}

func (g *GraphQuery) GetStats() map[string]int {
	graph, err := g.BuildGraph()
	if err != nil {
		return map[string]int{"error": 0}
	}

	nodeCount := len(graph.Nodes)
	edgeCount := len(graph.Edges)

	isolated := 0
	for _, node := range graph.Nodes {
		if node.Degree == 0 {
			isolated++
		}
	}

	maxDegree := 0
	for _, node := range graph.Nodes {
		if node.Degree > maxDegree {
			maxDegree = node.Degree
		}
	}

	clusters, _ := g.GetClusters(2)

	return map[string]int{
		"nodes":        nodeCount,
		"edges":        edgeCount,
		"isolated":     isolated,
		"max_degree":   maxDegree,
		"clusters":     len(clusters),
	}
}

func (g *GraphQuery) TextTree(entityID string, depth int) string {
	if depth <= 0 {
		depth = 2
	}

	entities, err := g.db.ListEntities("", 10000, 0)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	entityMap := make(map[string]types.Entity)
	for _, e := range entities {
		entityMap[e.ID] = e
	}

	_, ok := entityMap[entityID]
	if !ok {
		return "Entity not found"
	}

	var buildTree func(id string, currentDepth int, visited map[string]bool) string
	buildTree = func(id string, currentDepth int, visited map[string]bool) string {
		if currentDepth > depth {
			return ""
		}

		ent, ok := entityMap[id]
		if !ok {
			return ""
		}

		prefix := strings.Repeat("  ", currentDepth)
		marker := "├─ "
		if currentDepth == 0 {
			marker = ""
			prefix = ""
		}

		eType := colorForType(ent.EntityType)
		result := fmt.Sprintf("%s%s%s [%s] (%.0f%%)\n", prefix, marker, ent.Name, eType, ent.Confidence*100)

		if currentDepth < depth {
			neighborEdges, _ := g.GetNeighbors(id)
			for i, edge := range neighborEdges {
				if visited[edge.TargetID] {
					continue
				}
				visited[edge.TargetID] = true

				childPrefix := strings.Repeat("  ", currentDepth+1)
				if i < len(neighborEdges)-1 {
					result += fmt.Sprintf("%s│ %s (%s)\n", childPrefix, edge.TargetName, edge.RelationType)
					result += buildTree(edge.TargetID, currentDepth+2, visited)
				} else {
					result += fmt.Sprintf("%s└─ %s (%s)\n", childPrefix, edge.TargetName, edge.RelationType)
					result += buildTree(edge.TargetID, currentDepth+2, visited)
				}
			}
		}

		return result
	}

	return buildTree(entityID, 0, map[string]bool{entityID: true})
}

func (g *GraphQuery) Summary() string {
	stats := g.GetStats()

	var b strings.Builder
	b.WriteString("Knowledge Graph Summary\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")
	b.WriteString(fmt.Sprintf("  Nodes:       %d\n", stats["nodes"]))
	b.WriteString(fmt.Sprintf("  Edges:       %d\n", stats["edges"]))
	b.WriteString(fmt.Sprintf("  Isolated:    %d\n", stats["isolated"]))
	b.WriteString(fmt.Sprintf("  Clusters:    %d\n", stats["clusters"]))
	b.WriteString(fmt.Sprintf("  Max degree:  %d\n", stats["max_degree"]))
	return b.String()
}

func colorForType(eType types.EntityType) string {
	switch eType {
	case types.EntityDecision:
		return "DECISION"
	case types.EntityFact:
		return "FACT"
	case types.EntityCode:
		return "CODE"
	case types.EntityPreference:
		return "PREF"
	case types.EntityGoal:
		return "GOAL"
	default:
		return "ENTITY"
	}
}
