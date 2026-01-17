package simulation

import (
	"math"
	"sort"
)

func FindTopPathsToTarget(snapshot *GraphSnapshot, targetServiceId string, maxDepth int, maxPaths int) []BrokenPath {
	var paths []BrokenPath
	visited := make(map[string]bool)

	var startNodeIds []string
	for k := range snapshot.Nodes {
		startNodeIds = append(startNodeIds, k)
	}
	sort.Strings(startNodeIds)

	var dfs func(currentId string, currentPath []string, minRate float64)
	dfs = func(currentId string, currentPath []string, minRate float64) {
		if len(paths) >= maxPaths*2 {
			return
		}

		hops := len(currentPath) - 1

		if currentId == targetServiceId && hops >= 1 {

			pathCopy := make([]string, len(currentPath))
			copy(pathCopy, currentPath)
			paths = append(paths, BrokenPath{
				Path:    pathCopy,
				PathRps: minRate,
			})
			return
		}

		if hops >= maxDepth {
			return
		}

		edges := snapshot.OutgoingEdges[currentId]

		sort.Slice(edges, func(i, j int) bool {
			if edges[i].Rate != edges[j].Rate {
				return edges[i].Rate > edges[j].Rate
			}
			return edges[i].Target < edges[j].Target
		})

		for _, edge := range edges {
			if visited[edge.Target] {
				continue
			}

			visited[edge.Target] = true
			newPath := append(currentPath, edge.Target)
			newMinRate := math.Min(minRate, edge.Rate)

			dfs(edge.Target, newPath, newMinRate)

			delete(visited, edge.Target)
		}
	}

	for _, nodeId := range startNodeIds {
		if nodeId == targetServiceId {
			continue
		}
		if len(paths) >= maxPaths*2 {
			break
		}

		for k := range visited {
			delete(visited, k)
		}
		visited[nodeId] = true

		dfs(nodeId, []string{nodeId}, math.Inf(1))
	}

	sort.Slice(paths, func(i, j int) bool {
		return paths[i].PathRps > paths[j].PathRps
	})

	if len(paths) > maxPaths {
		return paths[:maxPaths]
	}
	return paths
}
