/*
TSCrunch binary cruncher, by Antonio Savona
*/

package main

import (
	"bytes"
	"container/heap"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// ----------------------
// Local Dijkstra Implementation
// ----------------------

// Arc represents an edge from one vertex to another with a weight.
type Arc struct {
	dest   int
	weight int64
}

// Graph holds an adjacency list representation.
type Graph struct {
	arcs map[int][]Arc
	n    int // total number of vertices
}

// NewGraph creates a new graph with n vertices.
func NewGraph(n int) *Graph {
	return &Graph{
		arcs: make(map[int][]Arc, n),
		n:    n,
	}
}

// AddVertex ensures that vertex v exists.
func (g *Graph) AddVertex(v int) {
	if _, ok := g.arcs[v]; !ok {
		g.arcs[v] = []Arc{}
	}
}

// AddArc adds a directed edge from u to v with the given weight.
func (g *Graph) AddArc(u, v int, weight int64) {
	g.arcs[u] = append(g.arcs[u], Arc{dest: v, weight: weight})
}

// Item is an element in the priority queue.
type Item struct {
	vertex   int
	priority int64
	index    int // index in the heap
}

// PriorityQueue implements heap.Interface.
type PriorityQueue []*Item

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].priority < pq[j].priority
}
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// Shortest computes the shortest path from source to target using Dijkstra’s algorithm.
// It returns the path (as a slice of vertex indices), the total cost, and a flag indicating success.
func (g *Graph) Shortest(source, target int) (path []int, cost int64, found bool) {
	const INF = math.MaxInt64
	dist := make([]int64, g.n)
	prev := make([]int, g.n)
	for i := 0; i < g.n; i++ {
		dist[i] = INF
		prev[i] = -1
	}
	dist[source] = 0

	pq := make(PriorityQueue, 0, g.n)
	heap.Init(&pq)
	heap.Push(&pq, &Item{vertex: source, priority: 0})

	for pq.Len() > 0 {
		item := heap.Pop(&pq).(*Item)
		u := item.vertex
		if u == target {
			break
		}
		for _, arc := range g.arcs[u] {
			alt := dist[u] + arc.weight
			if alt < dist[arc.dest] {
				dist[arc.dest] = alt
				prev[arc.dest] = u
				heap.Push(&pq, &Item{vertex: arc.dest, priority: alt})
			}
		}
	}

	if dist[target] == INF {
		return nil, 0, false
	}

	// Reconstruct the path.
	for u := target; u != -1; u = prev[u] {
		path = append(path, u)
	}
	// Reverse the path to get source->target.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path, dist[target], true
}

// ----------------------
// End Local Dijkstra Implementation
// ----------------------

// Go TSCrunch Code

type crunchCtx struct {
	QUIET          bool
	STATS          bool
	PRG            bool
	SFX            bool
	SFXMODE        int
	BLANK          bool
	INPLACE        bool
	jmp            uint16
	decrunchTo     uint16
	loadTo         uint16
	addr           []byte
	optimalRun     int
	crunchedSize   int
	sourceLen      int
	sourceAbsLen   int
	decrunchEnd    uint16
	prefixArray    map[[MINLZ]byte][]int
	usePrefixArray bool
}

type edge struct {
	n0 int
	n1 int
}

type token struct {
	tokentype byte
	size      int
	rlebyte   byte
	offset    int
	i         int
}

type tokenEntry struct {
	e edge
	t token
}

const LONGESTRLE = 64
const LONGESTLONGLZ = 64
const LONGESTLZ = 32
const LONGESTLITERAL = 31
const MINRLE = 2
const MINLZ = 3
const LZOFFSET = 256
const LONGLZOFFSET = 32767
const LZ2OFFSET = 94

const RLEMASK = 0x81
const LZMASK = 0x80
const LITERALMASK = 0x00
const LZ2MASK = 0x00

const TERMINATOR = LONGESTLITERAL + 1

const LZ2ID = 3
const LZID = 2
const RLEID = 1
const LITERALID = 4
const LONGLZID = 5
const ZERORUNID = 6

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func load_raw(f string) []byte {
	data, err := os.ReadFile(f)
	if err == nil {
		return data
	}
	fmt.Println("can't read data")
	return nil
}

func save_raw(f string, data []byte) {
	os.WriteFile(f, data, 0666)
}

func fillPrefixArray(data []byte, ctx *crunchCtx) {
	ctx.prefixArray = make(map[[MINLZ]byte][]int)
	for i := 0; i < len(data)-MINLZ; i++ {
		key := *(*[MINLZ]byte)(data[i:])
		ctx.prefixArray[key] = append(ctx.prefixArray[key], i)
	}
}

func findall(data []byte, prefix []byte, i int, minlz int, ctx *crunchCtx) <-chan int {
	c := make(chan int)
	x0 := max(0, i-LONGLZOFFSET)
	x1 := min(i+minlz-1, len(data))
	if ctx.usePrefixArray {
		parray := ctx.prefixArray[*(*[MINLZ]byte)(prefix[:MINLZ])]
		go func() {
			l := 0
			h := len(parray) - 1
			var mid int
			for l < h {
				mid = (h + l) >> 1
				if parray[mid] < i {
					l = mid + 1
				} else if parray[mid] > i {
					h = mid - 1
				} else {
					h = mid
					l = mid
				}
			}
			for o := mid; o >= 0 && parray[o] > x0; o-- {
				if parray[o] < i && bytes.Equal(data[parray[o]:parray[o]+minlz], prefix) {
					c <- parray[o]
				}
			}
			close(c)
		}()
	} else {
		f := 1
		go func() {
			for f >= 0 {
				f = bytes.LastIndex(data[x0:x1], prefix)
				if f >= 0 {
					c <- f + x0
					x1 = x0 + f + minlz - 1
				}
			}
			close(c)
		}()
	}
	return c
}

func findOptimalZeroRun(src []byte) int {
	zeroruns := make(map[int]int)
	var i, j int
	for i < len(src)-1 {
		if src[i] == 0 {
			j = i + 1
			for j < len(src) && src[j] == 0 && j-i < 256 {
				j++
			}
			if j-i >= MINRLE {
				zeroruns[j-i]++
			}
			i = j
		} else {
			i++
		}
	}
	if len(zeroruns) > 0 {
		bestrun := 0
		bestvalue := 0.0
		for key, amount := range zeroruns {
			currentvalue := float64(key) * math.Pow(float64(amount), 1.1)
			if currentvalue > bestvalue {
				bestrun = key
				bestvalue = currentvalue
			}
		}
		return bestrun
	}
	return LONGESTRLE
}

func tokenCost(n0, n1 int, t byte) int64 {
	size := int64(n1 - n0)
	mdiv := int64(LONGESTLITERAL * (1 << 16))
	switch t {
	case LZID:
		return mdiv*2 + 134 - size
	case LONGLZID:
		return mdiv*3 + 138 - size
	case RLEID:
		return mdiv*2 + 128 - size
	case ZERORUNID:
		return mdiv * 1
	case LZ2ID:
		return mdiv*1 + 132 - size
	case LITERALID:
		return mdiv*(size+1) + 130 - size
	default:
		os.Exit(-1)
	}
	return 0
}

func tokenPayload(src []byte, t token) []byte {
	n0 := t.i
	n1 := t.i + t.size
	switch t.tokentype {
	case LZID:
		return []byte{byte(LZMASK | (((t.size - 1) << 2) & 0x7f) | 2), byte(t.offset & 0xff)}
	case LONGLZID:
		negoffset := (0 - t.offset)
		return []byte{byte(LZMASK | (((t.size-1)>>1)<<2)&0x7f), byte(negoffset & 0xff), byte(((negoffset >> 8) & 0x7f) | (((t.size - 1) & 1) << 7))}
	case RLEID:
		return []byte{RLEMASK | byte(((t.size-1)<<1)&0x7f), t.rlebyte}
	case ZERORUNID:
		return []byte{RLEMASK}
	case LZ2ID:
		return []byte{LZ2MASK | byte(0x7f-t.offset)}
	default:
		return append([]byte{byte(LITERALMASK | t.size)}, src[n0:n1]...)
	}
}

func LZ(src []byte, i int, size int, offset int, minlz int, ctx *crunchCtx) token {
	var lz token
	lz.tokentype = LZID
	lz.i = i
	if i >= 0 {
		bestpos := i - 1
		bestlen := 0
		if len(src)-i >= minlz {
			prefixes := findall(src, src[i:i+minlz], i, minlz, ctx)
			for j := range prefixes {
				l := minlz
				for i+l < len(src) && l < LONGESTLONGLZ && src[j+l] == src[i+l] {
					l++
				}
				if (l > bestlen && (i-j < LZOFFSET || i-bestpos >= LZOFFSET || l > LONGESTLZ)) || (l > bestlen+1) {
					bestpos = j
					bestlen = l
				}
			}
		}
		lz.size = bestlen
		lz.offset = i - bestpos
	} else {
		lz.size = size
		lz.offset = offset
	}
	if lz.size > LONGESTLZ || lz.offset >= LZOFFSET {
		lz.tokentype = LONGLZID
	}
	return lz
}

func RLE(src []byte, i int, size int, rlebyte byte) token {
	var rle token
	rle.tokentype = RLEID
	rle.i = i
	if i >= 0 {
		rle.rlebyte = src[i]
		x := 0
		for i+x < len(src) && x < LONGESTRLE+1 && src[i+x] == src[i] {
			x++
		}
		rle.size = x
	} else {
		rle.size = size
		rle.rlebyte = rlebyte
	}
	return rle
}

func ZERORUN(src []byte, i int, optimalRun int) token {
	var zero token
	zero.tokentype = ZERORUNID
	zero.i = i
	zero.rlebyte = 0
	zero.size = 0
	if i >= 0 {
		var x int
		for x = 0; x < optimalRun && i+x < len(src) && src[i+x] == 0; x++ {
		}
		if x == optimalRun {
			zero.size = optimalRun
		}
	}
	return zero
}

func LZ2(src []byte, i int, size int, offset int) token {
	var lz2 token
	lz2.tokentype = LZ2ID
	lz2.offset = -1
	lz2.size = -1
	lz2.i = i
	if i >= 0 {
		if i+2 < len(src) {
			leftbound := max(0, i-LZ2OFFSET)
			lpart := src[leftbound : i+1]
			o := bytes.LastIndex(lpart, src[i:i+2])
			if o >= 0 {
				lz2.offset = i - (o + leftbound)
				lz2.size = 2
			}
		}
	} else {
		lz2.size = size
		lz2.offset = offset
	}
	return lz2
}

func LIT(i int, size int) token {
	var lit token
	lit.tokentype = LITERALID
	lit.size = size
	lit.i = i
	return lit
}

// crunchAtByteWorker processes a single source position and returns any tokens found.
func crunchAtByteWorker(src []byte, i int, ctx *crunchCtx) []tokenEntry {
	entries := []tokenEntry{}
	rle := RLE(src, i, 0, 0)
	rlesize := min(rle.size, LONGESTRLE)
	var lz, lz2 token
	if rlesize < LONGESTLONGLZ-1 {
		lz = LZ(src, i, 0, 0, max(rlesize+1, MINLZ), ctx)
	} else {
		lz = LZ(src, -1, -1, -1, -1, ctx)
	}
	if len(src)-i > 2 {
		lz2 = LZ2(src, i, 0, 0)
	}
	zero := ZERORUN(src, i, ctx.optimalRun)
	for size := lz.size; size >= MINLZ && size > rlesize; size-- {
		tokenCopy := LZ(src, -1, size, lz.offset, MINLZ, ctx)
		entries = append(entries, tokenEntry{e: edge{i, i + size}, t: tokenCopy})
	}
	if rle.size > LONGESTRLE {
		entries = append(entries, tokenEntry{e: edge{i, i + LONGESTRLE}, t: RLE(src, -1, LONGESTRLE, src[i])})
	} else {
		for size := rle.size; size >= MINRLE; size-- {
			entries = append(entries, tokenEntry{e: edge{i, i + size}, t: RLE(src, -1, size, src[i])})
		}
	}
	if lz2.size == 2 {
		entries = append(entries, tokenEntry{e: edge{i, i + 2}, t: lz2})
	}
	if zero.size != 0 {
		entries = append(entries, tokenEntry{e: edge{i, i + ctx.optimalRun}, t: zero})
	}
	return entries
}

func crunch(src []byte, ctx *crunchCtx) []byte {
	// Boot blocks.
	var boot = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA2, 0xCF, 0xBD, 0x1A, 0x08, 0x95, 0x00, 0xCA, 0xD0,
		0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8,
		0xD0, 0xF7, 0xC6, 0x07, 0xA9, 0x06, 0xC7, 0x04, 0x90, 0xEF, 0xA0, 0x00,
		0xB3, 0x24, 0x30, 0x29, 0xC9, 0x20, 0xB0, 0x47, 0xE6, 0x24, 0xD0, 0x02,
		0xE6, 0x25, 0xB9, 0xFF, 0xFF, 0x99, 0xFF, 0xFF, 0xC8, 0xCA, 0xD0, 0xF6,
		0x98, 0xAA, 0xA0, 0x00, 0x65, 0x27, 0x85, 0x27, 0xB0, 0x77, 0x8A, 0x65,
		0x24, 0x85, 0x24, 0x90, 0xD7, 0xE6, 0x25, 0xB0, 0xD3, 0x4B, 0x7F, 0x90,
		0x3A, 0xF0, 0x6B, 0xA2, 0x02, 0x85, 0x59, 0xC8, 0xB1, 0x24, 0xA4, 0x59,
		0x91, 0x27, 0x88, 0x91, 0x27, 0xD0, 0xFB, 0xA9, 0x00, 0xB0, 0xD5, 0xA9,
		0x37, 0x85, 0x01, 0x58, 0x4C, 0x61, 0x00, 0xF0, 0xF6, 0x09, 0x80, 0x65,
		0x27, 0x85, 0xA1, 0xA5, 0x28, 0xE9, 0x00, 0x85, 0xA2, 0xB1, 0xA1, 0x91,
		0x27, 0xC8, 0xB1, 0xA1, 0x91, 0x27, 0x98, 0xAA, 0x88, 0xF0, 0xB1, 0x4A,
		0x85, 0xA6, 0xC8, 0xA5, 0x27, 0x90, 0x33, 0xF1, 0x24, 0x85, 0xA1, 0xA5,
		0x28, 0xE9, 0x00, 0x85, 0xA2, 0xA2, 0x02, 0xA0, 0x00, 0xB1, 0xA1, 0x91,
		0x27, 0xC8, 0xB1, 0xA1, 0x91, 0x27, 0xC8, 0xB9, 0xA1, 0x00, 0x91, 0x27,
		0xC0, 0x00, 0xD0, 0xF6, 0x98, 0xA0, 0x00, 0xB0, 0x83, 0xE6, 0x28, 0x18,
		0x90, 0x84, 0xA0, 0xFF, 0x84, 0x59, 0xA2, 0x01, 0xD0, 0x96, 0x71, 0x24,
		0x85, 0xA1, 0xC8, 0xB3, 0x24, 0x09, 0x80, 0x65, 0x28, 0x85, 0xA2, 0xE0,
		0x80, 0x26, 0xA6, 0xA2, 0x03, 0xD0, 0xC4,
	}

	var blank_boot = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA9, 0x0B, 0x8D, 0x11, 0xD0, 0xA2, 0xCF, 0xBD, 0x1F,
		0x08, 0x95, 0x00, 0xCA, 0xD0, 0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00,
		0x10, 0x9D, 0x00, 0xFF, 0xE8, 0xD0, 0xF7, 0xC6, 0x07, 0xA9, 0x06, 0xC7,
		0x04, 0x90, 0xEF, 0xA0, 0x00, 0xB3, 0x24, 0x30, 0x29, 0xC9, 0x20, 0xB0,
		0x47, 0xE6, 0x24, 0xD0, 0x02, 0xE6, 0x25, 0xB9, 0xFF, 0xFF, 0x99, 0xFF,
		0xFF, 0xC8, 0xCA, 0xD0, 0xF6, 0x98, 0xAA, 0xA0, 0x00, 0x65, 0x27, 0x85,
		0x27, 0xB0, 0x77, 0x8A, 0x65, 0x24, 0x85, 0x24, 0x90, 0xD7, 0xE6, 0x25,
		0xB0, 0xD3, 0x4B, 0x7F, 0x90, 0x3A, 0xF0, 0x6B, 0xA2, 0x02, 0x85, 0x59,
		0xC8, 0xB1, 0x24, 0xA4, 0x59, 0x91, 0x27, 0x88, 0x91, 0x27, 0xD0, 0xFB,
		0xA9, 0x00, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58, 0x4C, 0x61, 0x00,
		0xF0, 0xF6, 0x09, 0x80, 0x65, 0x27, 0x85, 0xA1, 0xA5, 0x28, 0xE9, 0x00,
		0x85, 0xA2, 0xB1, 0xA1, 0x91, 0x27, 0xC8, 0xB1, 0xA1, 0x91, 0x27, 0x98,
		0xAA, 0x88, 0xF0, 0xB1, 0x4A, 0x85, 0xA6, 0xC8, 0xA5, 0x27, 0x90, 0x33,
		0xF1, 0x24, 0x85, 0xA1, 0xA5, 0x28, 0xE9, 0x00, 0x85, 0xA2, 0xA2, 0x02,
		0xA0, 0x00, 0xB1, 0xA1, 0x91, 0x27, 0xC8, 0xB1, 0xA1, 0x91, 0x27, 0xC8,
		0xB9, 0xA1, 0x00, 0x91, 0x27, 0xC0, 0x00, 0xD0, 0xF6, 0x98, 0xA0, 0x00,
		0xB0, 0x83, 0xE6, 0x28, 0x18, 0x90, 0x84, 0xA0, 0xFF, 0x84, 0x59, 0xA2,
		0x01, 0xD0, 0x96, 0x71, 0x24, 0x85, 0xA1, 0xC8, 0xB3, 0x24, 0x09, 0x80,
		0x65, 0x28, 0x85, 0xA2, 0xE0, 0x80, 0x26, 0xA6, 0xA2, 0x03, 0xD0, 0xC4,
	}

	var boot2 = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA9, 0x34, 0x85, 0x01, 0xA2, 0xD3, 0xBD, 0x1F, 0x08,
		0x9D, 0xFB, 0x00, 0xCA, 0xD0, 0xF7, 0x4C, 0x00, 0x01, 0xAA, 0xAA, 0xAA,
		0xAA, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8, 0xD0, 0xF7, 0xCE, 0x05,
		0x01, 0xA9, 0x06, 0xCF, 0x02, 0x01, 0x90, 0xED, 0xA0, 0x00, 0xB3, 0xFC,
		0x30, 0x27, 0xC9, 0x20, 0xB0, 0x45, 0xE6, 0xFC, 0xD0, 0x02, 0xE6, 0xFD,
		0xB1, 0xFC, 0x91, 0xFE, 0xC8, 0xCA, 0xD0, 0xF8, 0x98, 0xAA, 0xA0, 0x00,
		0x65, 0xFE, 0x85, 0xFE, 0xB0, 0x77, 0x8A, 0x65, 0xFC, 0x85, 0xFC, 0x90,
		0xD9, 0xE6, 0xFD, 0xB0, 0xD5, 0x4B, 0x7F, 0x90, 0x3A, 0xF0, 0x6B, 0xA2,
		0x02, 0x85, 0xF9, 0xC8, 0xB1, 0xFC, 0xA4, 0xF9, 0x91, 0xFE, 0x88, 0x91,
		0xFE, 0xD0, 0xFB, 0xA5, 0xF9, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58,
		0x4C, 0x5F, 0x01, 0xF0, 0xF6, 0x09, 0x80, 0x65, 0xFE, 0x85, 0xFA, 0xA5,
		0xFF, 0xE9, 0x00, 0x85, 0xFB, 0xB1, 0xFA, 0x91, 0xFE, 0xC8, 0xB1, 0xFA,
		0x91, 0xFE, 0x98, 0xAA, 0x88, 0xF0, 0xB1, 0x4A, 0x8D, 0xA4, 0x01, 0xC8,
		0xA5, 0xFE, 0x90, 0x32, 0xF1, 0xFC, 0x85, 0xFA, 0xA5, 0xFF, 0xE9, 0x00,
		0x85, 0xFB, 0xA2, 0x02, 0xA0, 0x00, 0xB1, 0xFA, 0x91, 0xFE, 0xC8, 0xB1,
		0xFA, 0x91, 0xFE, 0xC8, 0xB1, 0xFA, 0x91, 0xFE, 0xC0, 0x00, 0xD0, 0xF7,
		0x98, 0xA0, 0x00, 0xB0, 0x83, 0xE6, 0xFF, 0x18, 0x90, 0x84, 0xA0, 0xFF,
		0x84, 0xF9, 0xA2, 0x01, 0xD0, 0x96, 0x71, 0xFC, 0x85, 0xFA, 0xC8, 0xB3,
		0xFC, 0x09, 0x80, 0x65, 0xFF, 0x85, 0xFB, 0xE0, 0x80, 0x2E, 0xA4, 0x01,
		0xA2, 0x03, 0xD0, 0xC4,
	}

	// Create a graph with len(src)+1 vertices.
	g := NewGraph(len(src) + 1)
	for i := 0; i < len(src)+1; i++ {
		g.AddVertex(i)
	}

	ctx.sourceLen = len(src)
	ctx.sourceAbsLen = ctx.sourceLen

	remainder := []byte{}
	if ctx.PRG {
		ctx.addr = src[:2]
		src = src[2:]
		ctx.decrunchTo = uint16(ctx.addr[0]) + 256*uint16(ctx.addr[1])
		ctx.sourceAbsLen -= 2
	}

	if ctx.INPLACE {
		remainder = src[len(src)-1:]
		src = src[:len(src)-1]
	}

	ctx.optimalRun = findOptimalZeroRun(src)
	if ctx.usePrefixArray {
		fillPrefixArray(src, ctx)
	}

	if !ctx.QUIET {
		fmt.Print("Populating LZ layer")
	}
	tm := time.Now()

	// --- Worker pool with collector goroutine ---
	numWorkers := runtime.GOMAXPROCS(0)
	jobs := make(chan int, numWorkers*2)
	results := make(chan tokenEntry, numWorkers*4)

	// Collector: merge results concurrently into tokenMap.
	tokenMap := make(map[edge]token)
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for entry := range results {
			tokenMap[entry.e] = entry.t
		}
	}()

	// Launch workers.
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				entries := crunchAtByteWorker(src, i, ctx)
				for _, entry := range entries {
					results <- entry
				}
			}
		}()
	}

	// Send jobs.
	for i := 0; i < len(src); i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)
	collectorWg.Wait()
	// --- End worker pool ---

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Closing Gaps")
	}
	// Fill gaps with literal tokens.
	for i := 0; i < len(src); i++ {
		for j := 1; j < min(LONGESTLITERAL+1, len(src)+1-i); j++ {
			key := edge{i, i + j}
			if _, exists := tokenMap[key]; !exists {
				tokenMap[key] = LIT(i, j)
			}
		}
	}

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Populating Graph")
	}
	tm = time.Now()
	for k, t := range tokenMap {
		g.AddArc(k.n0, k.n1, tokenCost(k.n0, k.n1, t.tokentype))
	}

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Computing shortest path")
	}
	tm = time.Now()
	bestPath, _, found := g.Shortest(0, len(src))
	if !found {
		fmt.Println("No valid path found")
		os.Exit(1)
	}
	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
	}

	crunched := make([]byte, 0)
	token_list := make([]token, 0)
	for i := 0; i < len(bestPath)-1; i++ {
		e := edge{bestPath[i], bestPath[i+1]}
		token_list = append(token_list, tokenMap[e])
	}

	if ctx.INPLACE {
		safety := len(token_list)
		segmentUncrunchedSize := 0
		segmentCrunchedSize := 0
		totalUncrunchedSize := 0
		for i := len(token_list) - 1; i >= 0; i-- {
			segmentCrunchedSize += len(tokenPayload(src, token_list[i]))
			segmentUncrunchedSize += token_list[i].size
			if segmentUncrunchedSize <= segmentCrunchedSize {
				safety = i
				totalUncrunchedSize += segmentUncrunchedSize
				segmentUncrunchedSize = 0
				segmentCrunchedSize = 0
			}
		}
		for _, t := range token_list[:safety] {
			crunched = append(crunched, tokenPayload(src, t)...)
		}
		if totalUncrunchedSize > 0 {
			remainder = append(src[len(src)-totalUncrunchedSize:], remainder...)
		}
		crunched = append(crunched, TERMINATOR)
		crunched = append(crunched, remainder[1:]...)
		crunched = append(remainder[:1], crunched...)
		crunched = append([]byte{byte(ctx.optimalRun - 1)}, crunched...)
		crunched = append(ctx.addr, crunched...)
	} else {
		for _, t := range token_list {
			crunched = append(crunched, tokenPayload(src, t)...)
		}
		crunched = append(crunched, TERMINATOR)
		if !ctx.SFX {
			crunched = append([]byte{byte(ctx.optimalRun - 1)}, crunched...)
		}
	}

	ctx.crunchedSize = len(crunched)
	if ctx.SFX {
		if ctx.SFXMODE == 0 {
			gap := 0
			if ctx.BLANK {
				gap = 5
				boot = blank_boot
			}
			fileLen := len(boot) + len(crunched)
			startAddress := 0x10000 - len(crunched)
			transfAddress := fileLen + 0x6ff

			boot[0x1e+gap] = byte(transfAddress & 0xff)
			boot[0x1f+gap] = byte(transfAddress >> 8)
			boot[0x3f+gap] = byte(startAddress & 0xff)
			boot[0x40+gap] = byte(startAddress >> 8)
			boot[0x42+gap] = byte(ctx.decrunchTo & 0xff)
			boot[0x43+gap] = byte(ctx.decrunchTo >> 8)
			boot[0x7d+gap] = byte(ctx.jmp & 0xff)
			boot[0x7e+gap] = byte(ctx.jmp >> 8)
			boot[0xcf+gap] = byte(ctx.optimalRun - 1)
		} else {
			boot = boot2
			fileLen := len(boot) + len(crunched)
			startAddress := 0x10000 - len(crunched)
			transfAddress := fileLen + 0x6ff

			boot[0x26] = byte(transfAddress & 0xff)
			boot[0x27] = byte(transfAddress >> 8)
			boot[0x21] = byte(startAddress & 0xff)
			boot[0x22] = byte(startAddress >> 8)
			boot[0x23] = byte(ctx.decrunchTo & 0xff)
			boot[0x24] = byte(ctx.decrunchTo >> 8)
			boot[0x85] = byte(ctx.jmp & 0xff)
			boot[0x86] = byte(ctx.jmp >> 8)
			boot[0xd7] = byte(ctx.optimalRun - 1)
		}
		crunched = append(boot, crunched...)
		ctx.crunchedSize += len(boot)
		ctx.loadTo = 0x0801
	}

	ctx.decrunchEnd = uint16(int(ctx.decrunchTo) + ctx.sourceAbsLen - 1)
	if ctx.INPLACE {
		ctx.loadTo = ctx.decrunchEnd - uint16(len(crunched)) + 1
		crunched = append([]byte{byte(ctx.loadTo & 255), byte(ctx.loadTo >> 8)}, crunched...)
	}
	return crunched
}

func usage() {
	fmt.Println("TSCrunch 1.3 - binary cruncher, by Antonio Savona")
	fmt.Println("Usage: tscrunch [-p] [-i] [-q] [-x[2] $addr] infile outfile")
	fmt.Println(" -p  : input file is a prg, first 2 bytes are discarded.")
	fmt.Println(" -x  $addr: creates a self extracting file (forces -p)")
	fmt.Println(" -x2 $addr: creates a self extracting file with sfx code in stack (forces -p)")
	fmt.Println(" -b  : blanks screen during decrunching (only with -x)")
	fmt.Println(" -i  : inplace crunching (forces -p)")
	fmt.Println(" -q  : quiet mode")
}

func main() {
	ctx := crunchCtx{
		usePrefixArray: true,
		STATS:          true,
	}
	var jmp_str string
	var jmp_str2 string
	flag.BoolVar(&ctx.PRG, "p", false, "")
	flag.BoolVar(&ctx.QUIET, "q", false, "")
	flag.BoolVar(&ctx.INPLACE, "i", false, "")
	flag.StringVar(&jmp_str, "x", "", "")
	flag.BoolVar(&ctx.BLANK, "b", false, "")
	flag.StringVar(&jmp_str2, "x2", "", "")
	flag.Usage = usage
	flag.Parse()

	if jmp_str != "" {
		ctx.SFX = true
		ctx.PRG = true
		ctx.SFXMODE = 0
	}
	if jmp_str2 != "" {
		ctx.SFX = true
		ctx.PRG = true
		ctx.SFXMODE = 1
		jmp_str = jmp_str2
	}
	if ctx.INPLACE {
		ctx.PRG = true
	}
	if flag.NArg() != 2 {
		usage()
		os.Exit(2)
	}
	if ctx.SFX {
		if len(jmp_str) == 0 {
			usage()
			os.Exit(2)
		}
		var jmp uint64
		var err error
		// Check if the argument starts with '$'
		if jmp_str[0] == '$' {
			jmp, err = strconv.ParseUint(jmp_str[1:], 16, 16)
		} else if len(jmp_str) > 1 && (jmp_str[:2] == "0x" || jmp_str[:2] == "0X") {
			// Check for the 0x or 0X prefix
			jmp, err = strconv.ParseUint(jmp_str[2:], 16, 16)
		} else {
			// Otherwise, assume it's a decimal value.
			jmp, err = strconv.ParseUint(jmp_str, 10, 16)
		}
		if err != nil {
			fmt.Printf("Invalid jump address: %v\n", err)
			usage()
			os.Exit(2)
		}
		ctx.jmp = uint16(jmp)
		if ctx.jmp == 0 {
			usage()
			os.Exit(2)
		}
	}

	ifidx := flag.NArg() - 2
	ofidx := flag.NArg() - 1

	src := load_raw(flag.Args()[ifidx])
	crunched := crunch(src, &ctx)
	save_raw(flag.Args()[ofidx], crunched)

	if !ctx.QUIET {
		ratio := (float32(ctx.crunchedSize) * 100.0 / float32(ctx.sourceLen))
		prg := "RAW"
		dest_prg := "RAW"
		if ctx.PRG {
			prg = "PRG"
		}
		if ctx.SFX || ctx.INPLACE {
			dest_prg = "prg"
		}
		fmt.Printf("Input file  %s: %s, $%04x - $%04x : %d bytes\n",
			prg, flag.Args()[ifidx], ctx.decrunchTo, ctx.decrunchEnd, ctx.sourceLen)
		fmt.Printf("Output file %s: %s, $%04x - $%04x : %d bytes\n",
			dest_prg, flag.Args()[ofidx], ctx.loadTo, ctx.crunchedSize+int(ctx.loadTo)-1, ctx.crunchedSize)
		fmt.Printf("Crunched to %.2f%% of original size\n", ratio)
	}
}
