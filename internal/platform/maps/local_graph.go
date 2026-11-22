package maps

import (
	"container/heap"
	"fmt"
	"math"
	"strings"

	"cityflow/internal/common/location"
)

// roadGraph is an in-memory directed graph of the local road network. Nodes
// are merged by rounded WGS84 coordinates: OSM ways that meet at an
// intersection share that node's coordinates exactly, so no noding step is
// needed before the merge.
type roadGraph struct {
	nodes []location.Point
	index map[nodeKey]int32
	adj   [][]graphEdge
	names []string
}

type nodeKey struct{ lat, lon int32 }

type graphEdge struct {
	to     int32
	meters float32
	name   int32
	bike   bool
	drive  bool
}

func buildRoadGraph(ways []RoadWay) *roadGraph {
	if len(ways) == 0 {
		return nil
	}
	g := &roadGraph{index: make(map[nodeKey]int32, 400000)}
	nameIndex := make(map[string]int32)
	for _, way := range ways {
		if len(way.Points) < 2 || way.Highway == "" {
			continue
		}
		lowered := strings.ToLower(way.Highway)
		bike := !excludedForBike[lowered]
		drive := !excludedForDrive[lowered]
		if !bike && !drive {
			continue
		}
		name := internName(nameIndex, g, way.Name)
		for i := 1; i < len(way.Points); i++ {
			from, okFrom := g.nodeID(way.Points[i-1])
			to, okTo := g.nodeID(way.Points[i])
			if !okFrom || !okTo || from == to {
				continue
			}
			meters := float32(haversine(way.Points[i-1], way.Points[i]))
			if meters < 0.2 {
				continue
			}
			if way.OneWay <= 0 {
				g.addEdge(to, from, meters, name, bike, drive)
			}
			if way.OneWay >= 0 {
				g.addEdge(from, to, meters, name, bike, drive)
			}
		}
	}
	if len(g.nodes) < 2 {
		return nil
	}
	return g
}

var excludedForBike = map[string]bool{
	"motorway": true, "motorway_link": true, "steps": true, "footway": true,
	"path": true, "pedestrian": true, "corridor": true, "elevator": true,
}

var excludedForDrive = map[string]bool{
	"steps": true, "footway": true, "path": true, "cycleway": true,
	"pedestrian": true, "corridor": true, "elevator": true, "bridleway": true,
}

func internName(index map[string]int32, g *roadGraph, name string) int32 {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	if id, ok := index[name]; ok {
		return id
	}
	id := int32(len(g.names))
	g.names = append(g.names, name)
	index[name] = id
	return id
}

func (g *roadGraph) nodeID(p location.Point) (int32, bool) {
	key := nodeKey{lat: int32(math.Round(p.Latitude * 1e6)), lon: int32(math.Round(p.Longitude * 1e6))}
	if id, ok := g.index[key]; ok {
		return id, true
	}
	id := int32(len(g.nodes))
	g.nodes = append(g.nodes, p)
	g.adj = append(g.adj, nil)
	g.index[key] = id
	return id, true
}

func (g *roadGraph) addEdge(from, to int32, meters float32, name int32, bike, drive bool) {
	g.adj[from] = append(g.adj[from], graphEdge{to: to, meters: meters, name: name, bike: bike, drive: drive})
}

func (g *roadGraph) allowed(e graphEdge, mode string) bool {
	if mode == "driving" {
		return e.drive
	}
	return e.bike
}

// route returns WGS84 coordinates, total meters and simple turn instructions.
// A single best path is produced; callers keep the existing multi-path shape.
func (g *roadGraph) route(from, to location.Point, mode string) ([]location.Point, float64, []string) {
	start, startOffset := g.nearestNode(from)
	end, endOffset := g.nearestNode(to)
	if start < 0 || end < 0 {
		return straightRoute(from, to)
	}
	if start == end {
		return []location.Point{from, to}, haversine(from, to), []string{"起终点距离很近，可直接前往"}
	}
	n := len(g.nodes)
	gScore := make([]float64, n)
	cameFrom := make([]int32, n)
	cameMeters := make([]float32, n)
	cameName := make([]int32, n)
	closed := make([]bool, n)
	for i := range gScore {
		gScore[i] = math.Inf(1)
		cameFrom[i] = -1
		cameName[i] = -1
	}
	gScore[start] = 0
	queue := &nodeQueue{{node: start, g: 0, f: haversine(g.nodes[start], g.nodes[end])}}
	heap.Init(queue)
	expanded := 0
	for queue.Len() > 0 {
		current := heap.Pop(queue).(*nodeQueueItem)
		if closed[current.node] {
			continue
		}
		if current.node == end {
			break
		}
		closed[current.node] = true
		expanded++
		if expanded > 2000000 {
			break
		}
		for _, edge := range g.adj[current.node] {
			if !g.allowed(edge, mode) || closed[edge.to] {
				continue
			}
			tentative := gScore[current.node] + float64(edge.meters)
			if tentative >= gScore[edge.to] {
				continue
			}
			gScore[edge.to] = tentative
			cameFrom[edge.to] = current.node
			cameMeters[edge.to] = edge.meters
			cameName[edge.to] = edge.name
			heap.Push(queue, &nodeQueueItem{node: edge.to, g: tentative, f: tentative + haversine(g.nodes[edge.to], g.nodes[end])})
		}
	}
	if cameFrom[end] < 0 {
		return straightRoute(from, to)
	}
	nodes := make([]int32, 0, 64)
	for at := end; at >= 0; at = cameFrom[at] {
		nodes = append(nodes, at)
		if at == start {
			break
		}
	}
	if len(nodes) < 2 || nodes[len(nodes)-1] != start {
		return straightRoute(from, to)
	}
	coords := make([]location.Point, 0, len(nodes)+2)
	coords = append(coords, from)
	meters := startOffset
	for i := len(nodes) - 1; i >= 0; i-- {
		p := g.nodes[nodes[i]]
		if len(coords) == 0 || coords[len(coords)-1] != p {
			coords = append(coords, p)
		}
		if i < len(nodes)-1 {
			meters += float64(cameMeters[nodes[i]])
		}
	}
	if coords[len(coords)-1] != to {
		coords = append(coords, to)
	}
	meters += endOffset
	return coords, meters, g.instructions(nodes, cameMeters, cameName)
}

func (g *roadGraph) instructions(nodes []int32, meters []float32, names []int32) []string {
	// nodes are ordered target -> start; walking down from the tail gives the
	// actual travel order. names[n] / meters[n] describe the edge entering n.
	parts := make([]string, 0, 8)
	lastName := int32(-1)
	lastMeters := 0.0
	flush := func() {
		if lastName >= 0 && lastMeters >= 1 {
			parts = append(parts, "沿"+g.names[lastName]+"行驶"+distanceText(lastMeters))
		}
		lastMeters = 0
	}
	for i := len(nodes) - 1; i >= 1; i-- {
		edgeName := names[nodes[i-1]]
		if edgeName != lastName {
			flush()
			lastName = edgeName
		}
		lastMeters += float64(meters[nodes[i-1]])
		if len(parts) >= 16 {
			break
		}
	}
	flush()
	if len(parts) > 12 {
		parts = parts[:12]
	}
	if len(parts) == 0 {
		parts = append(parts, "沿本地路网前往目的地")
	}
	return append(parts, "到达目的地附近")
}

func distanceText(meters float64) string {
	if meters < 1000 {
		return fmt.Sprintf("%.0f米", meters)
	}
	return fmt.Sprintf("%.1f公里", meters/1000)
}

func (g *roadGraph) nearestNode(p location.Point) (int32, float64) {
	best := int32(-1)
	bestDistance := math.Inf(1)
	for id, node := range g.nodes {
		d := haversine(p, node)
		if d < bestDistance {
			bestDistance = d
			best = int32(id)
		}
	}
	if best < 0 {
		return -1, 0
	}
	return best, bestDistance
}

func straightRoute(from, to location.Point) ([]location.Point, float64, []string) {
	return []location.Point{from, to}, haversine(from, to), []string{"本地路网未匹配到可行路线，按直线距离估算"}
}

func haversine(a, b location.Point) float64 {
	rad := math.Pi / 180
	h := math.Pow(math.Sin((b.Latitude-a.Latitude)*rad/2), 2) +
		math.Cos(a.Latitude*rad)*math.Cos(b.Latitude*rad)*math.Pow(math.Sin((b.Longitude-a.Longitude)*rad/2), 2)
	return 12742000 * math.Asin(math.Sqrt(math.Min(1, h)))
}

type nodeQueueItem struct {
	node int32
	f    float64
	g    float64
}

type nodeQueue []*nodeQueueItem

func (q nodeQueue) Len() int           { return len(q) }
func (q nodeQueue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q nodeQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *nodeQueue) Push(x any)        { *q = append(*q, x.(*nodeQueueItem)) }
func (q *nodeQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*q = old[:n-1]
	return item
}
