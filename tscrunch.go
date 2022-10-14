/*
TSCrunch binary cruncher, by Antonio Savona
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/RyanCarrier/dijkstra"
)

type tokenGraph struct {
	wg         sync.WaitGroup
	mg, ms, me sync.Mutex
	graph      map[edge]token
}

type crunchCtx struct {
	QUIET          bool
	STATS          bool
	PRG            bool
	SFX            bool
	INPLACE        bool
	jmp            uint16
	decrunchTo     uint16
	loadTo         uint16
	addr           []byte
	optimalRun     int
	crunchedSize   int
	sourceLen      int
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
	} else {
		fmt.Println("can't read data")
		return nil
	}
}

func save_raw(f string, data []byte) {
	os.WriteFile(f, data, 0666)
}

func fillPrefixArray(data []byte, ctx *crunchCtx) {
	ctx.prefixArray = make(map[[MINLZ]byte][]int)
	for i := 0; i < len(data)-MINLZ; i++ {
		ctx.prefixArray[*(*[MINLZ]byte)(data[i:])] = append(ctx.prefixArray[*(*[MINLZ]byte)(data[i:])], i)
	}
}

func findall(data []byte, prefix []byte, i int, minlz int, ctx *crunchCtx) <-chan int {
	c := make(chan int)
	x0 := max(0, i-LONGLZOFFSET)
	x1 := min(i+minlz-1, len(data))

	if ctx.usePrefixArray {
		parray := ctx.prefixArray[*(*[MINLZ]byte)(prefix[:MINLZ])]
		go func() {
			//binary search to the closest entry on the left
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

			for o := mid; len(parray) > 0 && o >= 0 && parray[o] > x0; o-- {
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
	var i = 0
	var j = 0
	for i < len(src)-1 {
		if src[i] == 0 {
			j = i + 1
			for j < len(src) && src[j] == 0 && j-i < 256 {
				j += 1
			}
			if j-i >= MINRLE {
				zeroruns[j-i] = zeroruns[j-i] + 1
			}
			i = j
		} else {
			i += 1
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
	} else {
		return LONGESTRLE
	}
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

func crunchAtByte(src []byte, i int, tg *tokenGraph, ctx *crunchCtx) {
	rle := RLE(src, i, 0, 0)
	rlesize := min(rle.size, LONGESTRLE)
	//don't compute prefix for same bytes or this will explode
	//start computing for prefixes larger than RLE size
	var lz token
	if rlesize < LONGESTLONGLZ-1 {
		lz = LZ(src, i, 0, 0, max(rlesize+1, MINLZ), ctx)
	} else {
		lz = LZ(src, -1, -1, -1, -1, ctx) // start with a dummy lz
	}

	for size := lz.size; size >= MINLZ && size > rlesize; size-- {
		tg.mg.Lock()
		tg.graph[edge{i, i + size}] = LZ(src, -1, size, lz.offset, MINLZ, ctx)
		tg.mg.Unlock()
	}

	if rle.size > LONGESTRLE {
		tg.mg.Lock()
		tg.graph[edge{i, i + LONGESTRLE}] = RLE(src, -1, LONGESTRLE, src[i])
		tg.mg.Unlock()
	} else {
		for size := rle.size; size >= MINRLE; size-- {
			tg.mg.Lock()
			tg.graph[edge{i, i + size}] = RLE(src, -1, size, src[i])
			tg.mg.Unlock()
		}
	}
	if len(src)-i > 2 {
		lz2 := LZ2(src, i, 0, 0)
		if lz2.size == 2 {
			tg.mg.Lock()
			tg.graph[edge{i, i + 2}] = lz2 //LZ2ID
			tg.mg.Unlock()
		}
	}

	zero := ZERORUN(src, i, ctx.optimalRun)
	if zero.size != 0 {
		tg.mg.Lock()
		tg.graph[edge{i, i + ctx.optimalRun}] = zero
		tg.mg.Unlock()
	}

	tg.wg.Done()
}

func crunch(src []byte, ctx *crunchCtx) []byte {

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

	tgraph := tokenGraph{
		wg:    sync.WaitGroup{},
		mg:    sync.Mutex{},
		ms:    sync.Mutex{},
		me:    sync.Mutex{},
		graph: make(map[edge]token),
	}

	ctx.sourceLen = len(src)

	remainder := []byte{}
	var G = dijkstra.NewGraph()

	if ctx.PRG {
		ctx.addr = src[:2]
		src = src[2:]
		ctx.decrunchTo = uint16(ctx.addr[0]) + 256*uint16(ctx.addr[1])
	}

	for i := 0; i < len(src)+1; i++ {
		G.AddVertex(i)
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

	for i := 0; i < len(src); i++ {
		tgraph.wg.Add(1)
		go crunchAtByte(src, i, &tgraph, ctx)
	}
	tgraph.wg.Wait()

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
	}

	if !ctx.QUIET {
		fmt.Print("Closing Gaps")
	}

	for i := 0; i < len(src); i++ {
		for j := 1; j < min(LONGESTLITERAL+1, len(src)+1-i); j++ {
			key := edge{i, i + j}
			_, haskey := tgraph.graph[key]
			if !haskey {
				lit := LIT(i, j)
				tgraph.graph[key] = lit
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

	for k, t := range tgraph.graph {
		G.AddArc(k.n0, k.n1, tokenCost(k.n0, k.n1, t.tokentype))
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

	best, _ := G.Shortest(0, len(src))

	if !ctx.QUIET {
		if ctx.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
	}
	crunched := make([]byte, 0)
	token_list := make([]token, 0)

	for i := 0; i < len(best.Path)-1; i++ {
		e := edge{best.Path[i], best.Path[i+1]}
		token_list = append(token_list, tgraph.graph[e])
	}

	if ctx.INPLACE {
		safety := len(token_list)
		segment_uncrunched_size := 0
		segment_crunched_size := 0
		total_uncrunched_size := 0
		for i := len(token_list) - 1; i >= 0; i-- {
			segment_crunched_size += len(tokenPayload(src, token_list[i])) //token size
			segment_uncrunched_size += token_list[i].size                  //decrunched token raw size
			if segment_uncrunched_size <= segment_crunched_size+0 {
				safety = i
				total_uncrunched_size += segment_uncrunched_size
				segment_uncrunched_size = 0
				segment_crunched_size = 0
			}
		}
		for _, t := range token_list[:safety] {
			crunched = append(crunched, tokenPayload(src, t)...)
		}
		if total_uncrunched_size > 0 {
			remainder = append(src[len(src)-total_uncrunched_size:], remainder...)
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
		fileLen := len(boot) + len(crunched)
		startAddress := 0x10000 - len(crunched)
		transfAddress := fileLen + 0x6ff

		boot[0x1e] = byte(transfAddress & 0xff) //transfer from
		boot[0x1f] = byte(transfAddress >> 8)

		boot[0x3f] = byte(startAddress & 0xff) //Depack from..
		boot[0x40] = byte(startAddress >> 8)

		boot[0x42] = byte(ctx.decrunchTo & 0xff) //decrunch to..
		boot[0x43] = byte(ctx.decrunchTo >> 8)

		boot[0x7d] = byte(ctx.jmp & 0xff) // Jump to..
		boot[0x7e] = byte(ctx.jmp >> 8)

		boot[0xcf] = byte(ctx.optimalRun - 1)

		crunched = append(boot, crunched...)

		ctx.crunchedSize += len(boot)
		ctx.loadTo = 0x0801
	}

	ctx.decrunchEnd = uint16(int(ctx.decrunchTo) + len(src) - 1)

	if ctx.INPLACE {
		ctx.loadTo = ctx.decrunchEnd - uint16(len(crunched)) + 1
		crunched = append([]byte{byte(ctx.loadTo & 255), byte(ctx.loadTo >> 8)}, crunched...)
	}

	return crunched
}

func usage() {
	fmt.Println("TSCrunch 1.3 - binary cruncher, by Antonio Savona")
	fmt.Println("Usage: tscrunch [-p] [-i] [-q] [-x $addr] infile outfile")
	fmt.Println(" -p  : input file is a prg, first 2 bytes are discarded.")
	fmt.Println(" -x  $addr: creates a self extracting file (forces -p)")
	fmt.Println(" -i  : inplace crunching (forces -p)")
	fmt.Println(" -q  : quiet mode")
}

func main() {
	ctx := crunchCtx{
		//prefix arrays for efficient prefix search don't really improve performance, here
		//due to the small search window.
		usePrefixArray: true,
		STATS:          true,
	}

	var jmp_str string
	flag.BoolVar(&ctx.PRG, "p", false, "")
	flag.BoolVar(&ctx.QUIET, "q", false, "")
	flag.BoolVar(&ctx.INPLACE, "i", false, "")
	flag.StringVar(&jmp_str, "x", "", "")
	flag.Usage = usage
	flag.Parse()

	if jmp_str != "" {
		ctx.SFX = true
		ctx.PRG = true
	}

	if ctx.INPLACE {
		ctx.PRG = true
	}

	if flag.NArg() != 2 {
		usage()
		os.Exit(2)
	}

	if ctx.SFX {
		if jmp_str[0] == '$' {
			jmp, err := strconv.ParseUint(jmp_str[1:], 16, 16)
			if err == nil {
				ctx.jmp = uint16(jmp)
			}
		}
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
