/*
TSCrunch v1.3.2 binary cruncher, by Antonio Savona
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

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

type lzCandidates struct {
	short token
	long  token
}

// shortestPathForward computes the exact shortest path in input-position
// order. transitions contains only specialized tokens; literal transitions are
// generated directly while visiting each position. A specialized token still
// suppresses the literal with the same start and end, exactly as the old gap
// closing pass did.
//
// Every token edge moves strictly forward, so positions are already a
// topological ordering and a priority queue is unnecessary. Equal total-cost
// arrivals prefer the predecessor with lower accumulated cost, matching the
// order in which Dijkstra settled them; equal predecessor costs keep the first
// arrival, retaining the original strict-less tie rule.
func shortestPathForward(transitions []token, starts []int, inputSize int) ([]token, int64, bool) {
	const unreachable = int64(math.MaxInt64)

	cost := make([]int64, inputSize+1)
	predecessor := make([]int, inputSize+1)
	descriptor := make([]token, inputSize+1)

	for i := range cost {
		cost[i] = unreachable
		predecessor[i] = -1
	}
	cost[0] = 0
	relax := func(position, destination int, transitionCost int64, transition token) {
		candidateCost := cost[position] + transitionCost
		better := candidateCost < cost[destination]
		if candidateCost == cost[destination] {
			previous := predecessor[destination]
			better = previous >= 0 && cost[position] < cost[previous]
		}
		if better {
			cost[destination] = candidateCost
			predecessor[destination] = position
			descriptor[destination] = transition
		}
	}

	for position := 0; position < inputSize; position++ {
		if cost[position] == unreachable {
			continue
		}
		var specializedLiteralLengths uint32
		for index := starts[position]; index < starts[position+1]; index++ {
			transition := transitions[index]
			destination := position + transition.size
			relax(position, destination, tokenCost(position, destination, transition.tokentype), transition)
			if transition.size <= LONGESTLITERAL {
				specializedLiteralLengths |= uint32(1) << transition.size
			}
		}
		literalLimit := min(LONGESTLITERAL, inputSize-position)
		for size := 1; size <= literalLimit; size++ {
			destination := position + size
			if specializedLiteralLengths&(uint32(1)<<size) != 0 {
				continue
			}
			literal := LIT(position, size)
			relax(position, destination, tokenCost(position, destination, literal.tokentype), literal)
		}
	}

	if cost[inputSize] == unreachable {
		return nil, 0, false
	}

	tokenCount := 0
	for position := inputSize; position > 0; position = predecessor[position] {
		if predecessor[position] < 0 {
			return nil, 0, false
		}
		tokenCount++
	}
	tokens := make([]token, tokenCount)
	for position, i := inputSize, tokenCount-1; position > 0; position, i = predecessor[position], i-1 {
		tokens[i] = descriptor[position]
	}
	return tokens, cost[inputSize], true
}

const LONGESTRLE = 64
const LONGESTLONGLZ = 64
const LONGESTLZ = 32
const LONGESTLITERAL = 31
const MINRLE = 2
const MINLZ = 3
const MINLZOFFSET = 2
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
	for i := 0; i+MINLZ <= len(data); i++ {
		key := *(*[MINLZ]byte)(data[i:])
		ctx.prefixArray[key] = append(ctx.prefixArray[key], i)
	}
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

func makeLZ(i int, size int, offset int, tokenType byte) token {
	return token{tokentype: tokenType, i: i, size: size, offset: offset}
}

func considerLZCandidate(src []byte, i, j, minlz int, candidates *lzCandidates) {
	offset := i - j
	if offset < MINLZOFFSET {
		return
	}
	length := minlz
	for i+length < len(src) && length < LONGESTLONGLZ && src[j+length] == src[i+length] {
		length++
	}
	if offset < LZOFFSET {
		shortLength := min(length, LONGESTLZ)
		if shortLength > candidates.short.size ||
			(shortLength == candidates.short.size && offset < candidates.short.offset) {
			candidates.short = makeLZ(i, shortLength, offset, LZID)
		}
	}
	if length > candidates.long.size ||
		(length == candidates.long.size && offset < candidates.long.offset) {
		candidates.long = makeLZ(i, length, offset, LONGLZID)
	}
}

// findLZCandidates keeps the two cost classes independent. A nearby match is
// eligible for the two-byte short token through length 32. Every valid offset,
// including a nearby one, is also eligible for the three-byte long token
// through length 64. Equal-length ties choose the nearest source explicitly.
func findLZCandidates(src []byte, i int, minlz int, ctx *crunchCtx) lzCandidates {
	candidates := lzCandidates{
		short: makeLZ(i, 0, 0, LZID),
		long:  makeLZ(i, 0, 0, LONGLZID),
	}
	if i < 0 || i+minlz > len(src) {
		return candidates
	}
	prefix := src[i : i+minlz]
	x0 := max(0, i-LONGLZOFFSET)
	if ctx.usePrefixArray {
		key := *(*[MINLZ]byte)(prefix)
		parray := ctx.prefixArray[key]
		for o := sort.SearchInts(parray, i) - 1; o >= 0 && parray[o] >= x0; o-- {
			j := parray[o]
			if j+minlz <= len(src) && bytes.Equal(src[j:j+minlz], prefix) {
				considerLZCandidate(src, i, j, minlz, &candidates)
			}
		}
	} else {
		x1 := min(i+minlz-1, len(src))
		for {
			found := bytes.LastIndex(src[x0:x1], prefix)
			if found < 0 {
				break
			}
			considerLZCandidate(src, i, x0+found, minlz, &candidates)
			x1 = x0 + found + minlz - 1
		}
	}
	return candidates
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
		if i+2 <= len(src) {
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
	var lz lzCandidates
	var lz2 token
	if rlesize < LONGESTLONGLZ-1 {
		lz = findLZCandidates(src, i, max(rlesize+1, MINLZ), ctx)
	}
	if len(src)-i >= 2 {
		lz2 = LZ2(src, i, 0, 0)
	}
	zero := ZERORUN(src, i, ctx.optimalRun)
	for size := lz.short.size; size >= MINLZ && size > rlesize; size-- {
		tokenCopy := makeLZ(i, size, lz.short.offset, LZID)
		entries = append(entries, tokenEntry{e: edge{i, i + size}, t: tokenCopy})
	}
	// A long edge ending where a short edge ends is strictly more expensive.
	// Only expose long prefixes that reach a destination unavailable to short.
	longMinimum := max(max(MINLZ, rlesize+1), lz.short.size+1)
	for size := lz.long.size; size >= longMinimum; size-- {
		tokenCopy := makeLZ(i, size, lz.long.offset, LONGLZID)
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

	// The old edge map retained the last token written for each destination.
	// Compact the row in place with the same rule before storing it.
	var slotByLength [257]uint16
	compacted := entries[:0]
	for _, entry := range entries {
		size := entry.t.size
		if slot := slotByLength[size]; slot != 0 {
			compacted[int(slot)-1] = entry
		} else {
			slotByLength[size] = uint16(len(compacted) + 1)
			compacted = append(compacted, entry)
		}
	}
	return compacted
}

func crunch(src []byte, ctx *crunchCtx) []byte {
	// Boot blocks.
	var boot = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA2, 0xCC, 0xBD, 0x1A, 0x08, 0x95, 0x00, 0xCA, 0xD0,
		0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8,
		0xD0, 0xF7, 0xC6, 0x07, 0xA9, 0x06, 0xC7, 0x04, 0x90, 0xEF, 0xA0, 0x00,
		0xB3, 0x24, 0x30, 0x29, 0xC9, 0x20, 0xB0, 0x47, 0xE6, 0x24, 0xD0, 0x02,
		0xE6, 0x25, 0xB9, 0xFF, 0xFF, 0x99, 0xFF, 0xFF, 0xC8, 0xCA, 0xD0, 0xF6,
		0x98, 0xAA, 0xA0, 0x00, 0x65, 0x27, 0x85, 0x27, 0xB0, 0x74, 0x8A, 0x65,
		0x24, 0x85, 0x24, 0x90, 0xD7, 0xE6, 0x25, 0xB0, 0xD3, 0x4B, 0x7F, 0x90,
		0x39, 0xF0, 0x68, 0xA2, 0x02, 0x85, 0x59, 0xC8, 0xB1, 0x24, 0xA4, 0x59,
		0x91, 0x27, 0x88, 0x91, 0x27, 0xD0, 0xFB, 0xA9, 0x00, 0xB0, 0xD5, 0xA9,
		0x37, 0x85, 0x01, 0x58, 0x4C, 0x61, 0x00, 0xF0, 0xF6, 0x09, 0x80, 0x65,
		0x27, 0x85, 0xA0, 0xA5, 0x28, 0xE9, 0x00, 0x85, 0xA1, 0xB1, 0xA0, 0x91,
		0x27, 0xC8, 0xB1, 0xA0, 0x91, 0x27, 0x98, 0xAA, 0xD0, 0xB0, 0x4A, 0x85,
		0xA5, 0xC8, 0xA5, 0x27, 0x90, 0x31, 0xF1, 0x24, 0x85, 0xA0, 0xA5, 0x28,
		0xE9, 0x00, 0x85, 0xA1, 0xA2, 0x02, 0xA0, 0x00, 0xB1, 0xA0, 0x91, 0x27,
		0xC8, 0xB1, 0xA0, 0x91, 0x27, 0xC8, 0xB9, 0xA0, 0x00, 0x91, 0x27, 0xC0,
		0x00, 0xD0, 0xF6, 0x98, 0xB0, 0x84, 0xE6, 0x28, 0x18, 0x90, 0x87, 0xA0,
		0xFF, 0x84, 0x59, 0xA2, 0x01, 0xD0, 0x99, 0x71, 0x24, 0x85, 0xA0, 0xC8,
		0xB3, 0x24, 0x09, 0x80, 0x65, 0x28, 0x85, 0xA1, 0xE0, 0x80, 0x26, 0xA5,
		0xA2, 0x03, 0xD0, 0xC6,
	}

	var blank_boot = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA9, 0x0B, 0x8D, 0x11, 0xD0, 0xA2, 0xCC, 0xBD, 0x1F,
		0x08, 0x95, 0x00, 0xCA, 0xD0, 0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00,
		0x10, 0x9D, 0x00, 0xFF, 0xE8, 0xD0, 0xF7, 0xC6, 0x07, 0xA9, 0x06, 0xC7,
		0x04, 0x90, 0xEF, 0xA0, 0x00, 0xB3, 0x24, 0x30, 0x29, 0xC9, 0x20, 0xB0,
		0x47, 0xE6, 0x24, 0xD0, 0x02, 0xE6, 0x25, 0xB9, 0xFF, 0xFF, 0x99, 0xFF,
		0xFF, 0xC8, 0xCA, 0xD0, 0xF6, 0x98, 0xAA, 0xA0, 0x00, 0x65, 0x27, 0x85,
		0x27, 0xB0, 0x74, 0x8A, 0x65, 0x24, 0x85, 0x24, 0x90, 0xD7, 0xE6, 0x25,
		0xB0, 0xD3, 0x4B, 0x7F, 0x90, 0x39, 0xF0, 0x68, 0xA2, 0x02, 0x85, 0x59,
		0xC8, 0xB1, 0x24, 0xA4, 0x59, 0x91, 0x27, 0x88, 0x91, 0x27, 0xD0, 0xFB,
		0xA9, 0x00, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58, 0x4C, 0x61, 0x00,
		0xF0, 0xF6, 0x09, 0x80, 0x65, 0x27, 0x85, 0xA0, 0xA5, 0x28, 0xE9, 0x00,
		0x85, 0xA1, 0xB1, 0xA0, 0x91, 0x27, 0xC8, 0xB1, 0xA0, 0x91, 0x27, 0x98,
		0xAA, 0xD0, 0xB0, 0x4A, 0x85, 0xA5, 0xC8, 0xA5, 0x27, 0x90, 0x31, 0xF1,
		0x24, 0x85, 0xA0, 0xA5, 0x28, 0xE9, 0x00, 0x85, 0xA1, 0xA2, 0x02, 0xA0,
		0x00, 0xB1, 0xA0, 0x91, 0x27, 0xC8, 0xB1, 0xA0, 0x91, 0x27, 0xC8, 0xB9,
		0xA0, 0x00, 0x91, 0x27, 0xC0, 0x00, 0xD0, 0xF6, 0x98, 0xB0, 0x84, 0xE6,
		0x28, 0x18, 0x90, 0x87, 0xA0, 0xFF, 0x84, 0x59, 0xA2, 0x01, 0xD0, 0x99,
		0x71, 0x24, 0x85, 0xA0, 0xC8, 0xB3, 0x24, 0x09, 0x80, 0x65, 0x28, 0x85,
		0xA1, 0xE0, 0x80, 0x26, 0xA5, 0xA2, 0x03, 0xD0, 0xC6,
	}

	var boot2 = []byte{
		0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
		0x00, 0x00, 0x78, 0xA9, 0x34, 0x85, 0x01, 0xA2, 0xD0, 0xBD, 0x1F, 0x08,
		0x9D, 0xFB, 0x00, 0xCA, 0xD0, 0xF7, 0x4C, 0x00, 0x01, 0xAA, 0xAA, 0xAA,
		0xAA, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8, 0xD0, 0xF7, 0xCE, 0x05,
		0x01, 0xA9, 0x06, 0xCF, 0x02, 0x01, 0x90, 0xED, 0xA0, 0x00, 0xB3, 0xFC,
		0x30, 0x27, 0xC9, 0x20, 0xB0, 0x45, 0xE6, 0xFC, 0xD0, 0x02, 0xE6, 0xFD,
		0xB1, 0xFC, 0x91, 0xFE, 0xC8, 0xCA, 0xD0, 0xF8, 0x98, 0xAA, 0xA0, 0x00,
		0x65, 0xFE, 0x85, 0xFE, 0xB0, 0x74, 0x8A, 0x65, 0xFC, 0x85, 0xFC, 0x90,
		0xD9, 0xE6, 0xFD, 0xB0, 0xD5, 0x4B, 0x7F, 0x90, 0x39, 0xF0, 0x68, 0xA2,
		0x02, 0x85, 0xF9, 0xC8, 0xB1, 0xFC, 0xA4, 0xF9, 0x91, 0xFE, 0x88, 0x91,
		0xFE, 0xD0, 0xFB, 0xA5, 0xF9, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58,
		0x4C, 0x5F, 0x01, 0xF0, 0xF6, 0x09, 0x80, 0x65, 0xFE, 0x85, 0xFA, 0xA5,
		0xFF, 0xE9, 0x00, 0x85, 0xFB, 0xB1, 0xFA, 0x91, 0xFE, 0xC8, 0xB1, 0xFA,
		0x91, 0xFE, 0x98, 0xAA, 0xD0, 0xB0, 0x4A, 0x8D, 0xA3, 0x01, 0xC8, 0xA5,
		0xFE, 0x90, 0x30, 0xF1, 0xFC, 0x85, 0xFA, 0xA5, 0xFF, 0xE9, 0x00, 0x85,
		0xFB, 0xA2, 0x02, 0xA0, 0x00, 0xB1, 0xFA, 0x91, 0xFE, 0xC8, 0xB1, 0xFA,
		0x91, 0xFE, 0xC8, 0xB1, 0xFA, 0x91, 0xFE, 0xC0, 0x00, 0xD0, 0xF7, 0x98,
		0xB0, 0x84, 0xE6, 0xFF, 0x18, 0x90, 0x87, 0xA0, 0xAA, 0x84, 0xF9, 0xA2,
		0x01, 0xD0, 0x99, 0x71, 0xFC, 0x85, 0xFA, 0xC8, 0xB3, 0xFC, 0x09, 0x80,
		0x65, 0xFF, 0x85, 0xFB, 0xE0, 0x80, 0x2E, 0xA3, 0x01, 0xA2, 0x03, 0xD0,
		0xC6,
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

	// Generate one deterministic candidate row per source position.
	numWorkers := runtime.GOMAXPROCS(0)
	jobs := make(chan int, numWorkers*2)
	rows := make([][]tokenEntry, len(src))

	// Launch workers.
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rows[i] = crunchAtByteWorker(src, i, ctx)
			}
		}()
	}

	// Send jobs.
	for i := 0; i < len(src); i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	// --- End worker pool ---

	starts := make([]int, len(src)+1)
	transitionCount := 0
	for position, row := range rows {
		starts[position] = transitionCount
		transitionCount += len(row)
	}
	starts[len(src)] = transitionCount
	transitions := make([]token, 0, transitionCount)
	for _, row := range rows {
		for _, entry := range row {
			transitions = append(transitions, entry.t)
		}
	}
	rows = nil

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Computing optimal parse")
	}
	tm = time.Now()
	token_list, _, found := shortestPathForward(transitions, starts, len(src))
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
			boot[0xcc+gap] = byte(ctx.optimalRun - 1)
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
			boot[0xd4] = byte(ctx.optimalRun - 1)
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
	fmt.Println("TSCrunch 1.3.2 - binary cruncher, by Antonio Savona")
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
