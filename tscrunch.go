/*
TSCrunch binary cruncher, by Antonio Savona
*/

package main

import (
	"bytes"
	"dijkstra" //go get github.com/RyanCarrier/dijkstra
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
)

type crunchCtx struct {
	QUIET      bool
	PRG        bool
	SFX        bool
	INPLACE    bool
	jmp        uint16
	decrunchTo uint16
	loadTo     uint16
	addr       []byte
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
const LZOFFSET = 32767
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

var boot = []byte{

	0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
	0x00, 0x00, 0x78, 0xA2, 0xC9, 0xBD, 0x1A, 0x08, 0x95, 0x00, 0xCA, 0xD0,
	0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8,
	0xD0, 0xF7, 0xC6, 0x04, 0xC6, 0x07, 0xA5, 0x04, 0xC9, 0x07, 0xB0, 0xED,
	0xA0, 0x00, 0xB3, 0x21, 0x30, 0x21, 0xC9, 0x20, 0xB0, 0x3F, 0xA8, 0xB9,
	0xFF, 0xFF, 0x88, 0x99, 0xFF, 0xFF, 0xD0, 0xF7, 0x8A, 0xE8, 0x65, 0x25,
	0x85, 0x25, 0xB0, 0x77, 0x8A, 0x65, 0x21, 0x85, 0x21, 0x90, 0xDF, 0xE6,
	0x22, 0xB0, 0xDB, 0x4B, 0x7F, 0x90, 0x3A, 0xF0, 0x6B, 0xA2, 0x02, 0x85,
	0x53, 0xC8, 0xB1, 0x21, 0xA4, 0x53, 0x91, 0x25, 0x88, 0x91, 0x25, 0xD0,
	0xFB, 0xA9, 0x00, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58, 0x4C, 0x5B,
	0x00, 0xF0, 0xF6, 0x09, 0x80, 0x65, 0x25, 0x85, 0x9B, 0xA5, 0x26, 0xE9,
	0x00, 0x85, 0x9C, 0xB1, 0x9B, 0x91, 0x25, 0xC8, 0xB1, 0x9B, 0x91, 0x25,
	0x98, 0xAA, 0x88, 0xF0, 0xB1, 0x4A, 0x85, 0xA0, 0xC8, 0xA5, 0x25, 0x90,
	0x33, 0xF1, 0x21, 0x85, 0x9B, 0xA5, 0x26, 0xE9, 0x00, 0x85, 0x9C, 0xA2,
	0x02, 0xA0, 0x00, 0xB1, 0x9B, 0x91, 0x25, 0xC8, 0xB1, 0x9B, 0x91, 0x25,
	0xC8, 0xB9, 0x9B, 0x00, 0x91, 0x25, 0xC0, 0x00, 0xD0, 0xF6, 0x98, 0xA0,
	0x00, 0xB0, 0x83, 0xE6, 0x26, 0x18, 0x90, 0x84, 0xA0, 0xFF, 0x84, 0x53,
	0xA2, 0x01, 0xD0, 0x96, 0x71, 0x21, 0x85, 0x9B, 0xC8, 0xB3, 0x21, 0x09,
	0x80, 0x65, 0x26, 0x85, 0x9C, 0xE0, 0x80, 0x26, 0xA0, 0xA2, 0x03, 0xD0,
	0xC4,
}

var wg sync.WaitGroup
var mg, ms, me sync.Mutex

var starts = make(map[int]bool)
var ends = make(map[int]bool)
var graph = make(map[edge]token)

var optimalRun int = 0

func usage() {
	fmt.Println("TSCrunch 1.3 - binary cruncher, by Antonio Savona")
	fmt.Println("Usage: tscrunch [-p] [-i] [-q] [-x $addr] infile outfile")
	fmt.Println(" -p  : input file is a prg, first 2 bytes are discarded.")
	fmt.Println(" -x  $addr: creates a self extracting file (forces -p)")
	fmt.Println(" -i  : inplace crunching (forces -p)")
	fmt.Println(" -q  : quiet mode")
}

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

func findall(data []byte, prefix []byte, i int, minlz int) <-chan int {
	c := make(chan int)
	x0 := max(0, i-LZOFFSET)
	x1 := min(i+minlz-1, len(data))
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
		return mdiv*3 + 134 - size
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

	if t.tokentype == LZID {
		return []byte{byte(LZMASK | (((t.size - 1) << 2) & 0x7f) | 2), byte(t.offset & 0xff)}
	} else if t.tokentype == LONGLZID {
		negoffset := (0 - t.offset)
		return []byte{byte(LZMASK | (((t.size-1)>>1)<<2)&0x7f), byte(negoffset & 0xff), byte(((negoffset >> 8) & 0x7f) | (((t.size - 1) & 1) << 7))}
	} else if t.tokentype == RLEID {
		return []byte{RLEMASK | byte(((t.size-1)<<1)&0x7f), t.rlebyte}
	} else if t.tokentype == ZERORUNID {
		return []byte{RLEMASK}
	} else if t.tokentype == LZ2ID {
		return []byte{LZ2MASK | byte(0x7f-t.offset)}
	} else {
		return append([]byte{byte(LITERALMASK | t.size)}, src[n0:n1]...)
	}
}

func LZ(src []byte, i int, size int, offset int, minlz int) token {
	var lz token
	lz.tokentype = LZID
	lz.i = i
	if i >= 0 {

		bestpos := i - 1
		bestlen := 0

		if len(src)-i >= minlz {
			prefixes := findall(src, src[i:i+minlz], i, minlz)
			for j := range prefixes {
				l := minlz
				for i+l < len(src) && l < LONGESTLONGLZ && src[j+l] == src[i+l] {
					l++
				}
				if l > bestlen {
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
	if lz.size > LONGESTLZ || lz.offset >= 256 {
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
		for i+x < len(src) && x < LONGESTRLE && src[i+x] == src[i] {
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

func crunchAtByte(src []byte, i int) {
	rle := RLE(src, i, 0, 0)
	//don't compute prefix for same bytes or this will explode
	//start computing for prefixes larger than RLE
	var lz token
	if rle.size < LONGESTLONGLZ-1 {
		lz = LZ(src, i, 0, 0, rle.size+1)
	} else {
		lz = LZ(src, -1, -1, -1, -1) // start with a dummy lz
	}

	if lz.size >= MINLZ || rle.size >= MINRLE {
		ms.Lock()
		starts[i] = true
		ms.Unlock()
	}

	for size := lz.size; size >= MINLZ && size > rle.size; size-- {
		me.Lock()
		ends[i+size] = true
		me.Unlock()

		mg.Lock()
		graph[edge{i, i + size}] = LZ(src, -1, size, lz.offset, MINLZ)
		mg.Unlock()
	}

	for size := rle.size; size >= MINRLE; size-- {
		me.Lock()
		ends[i+size] = true
		me.Unlock()

		mg.Lock()
		graph[edge{i, i + size}] = RLE(src, -1, size, src[i])
		mg.Unlock()
	}

	if len(src)-i > 2 {
		lz2 := LZ2(src, i, 0, 0)
		if lz2.size == 2 {
			mg.Lock()
			graph[edge{i, i + 2}] = lz2 //LZ2ID
			mg.Unlock()

			ms.Lock()
			starts[i] = true
			ms.Unlock()

			me.Lock()
			ends[i+2] = true
			me.Unlock()
		}
	}

	zero := ZERORUN(src, i, optimalRun)
	if zero.size != 0 {
		mg.Lock()
		graph[edge{i, i + optimalRun}] = zero
		mg.Unlock()

		ms.Lock()
		starts[i] = true
		ms.Unlock()

		me.Lock()
		ends[i+optimalRun] = true
		me.Unlock()
	}

	wg.Done()
}

func crunch(src []byte, ctx crunchCtx) []byte {

	remainder := []byte{}

	var G = dijkstra.NewGraph()

	for i := 0; i < len(src)+1; i++ {
		G.AddVertex(i)
	}

	if ctx.INPLACE {
		remainder = src[len(src)-1:]
		src = src[:len(src)-1]
	}

	optimalRun = findOptimalZeroRun(src)

	if !ctx.QUIET {
		fmt.Println("Populating LZ layer")
	}

	for i := 0; i < len(src); i++ {
		wg.Add(1)
		go crunchAtByte(src, i)
	}
	wg.Wait()

	starts[len(src)] = true
	ends[0] = true
	starts_ := make([]int, 0, len(starts))
	ends_ := make([]int, 0, len(ends))
	for k := range starts {
		starts_ = append(starts_, k)
	}
	for k := range ends {
		ends_ = append(ends_, k)
	}

	sort.Ints(starts_)
	sort.Ints(ends_)

	if !ctx.QUIET {
		fmt.Println("Closing Gaps")
	}

	e, s := 0, 0
	for e < len(ends_) && s < len(starts_) {
		end := ends_[e]
		if end < starts_[s] {
			//bridge
			for starts_[s]-end >= LONGESTLITERAL {
				key := edge{end, end + LONGESTLITERAL}
				_, haskey := graph[key]
				if !haskey {
					lit := LIT(end, LONGESTLITERAL)
					lit.size = LONGESTLITERAL
					graph[key] = lit
				}
				end += LONGESTLITERAL
			}
			s0 := s
			for s0 < len(starts_) && starts_[s0]-end < LONGESTLITERAL {
				key := edge{end, starts_[s0]}
				_, haskey := graph[key]
				if !haskey {
					lit := LIT(end, starts_[s0]-end)
					lit.size = starts_[s0] - end
					graph[key] = lit
				}
				s0++
			}
			e++
		} else {
			s++
		}
	}

	if !ctx.QUIET {
		fmt.Println("Populating Graph")
	}

	for k, t := range graph {
		G.AddArc(k.n0, k.n1, tokenCost(k.n0, k.n1, t.tokentype))
	}

	if !ctx.QUIET {
		fmt.Println("Computing shortest path")
	}

	best, _ := G.Shortest(0, len(src))

	crunched := make([]byte, 0)
	token_list := make([]token, 0)

	for i := 0; i < len(best.Path)-1; i++ {
		e := edge{best.Path[i], best.Path[i+1]}
		token_list = append(token_list, graph[e])
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
		crunched = append([]byte{byte(optimalRun - 1)}, crunched...)
		crunched = append(ctx.addr, crunched...)

	} else {
		for _, t := range token_list {
			crunched = append(crunched, tokenPayload(src, t)...)
		}
		crunched = append(crunched, TERMINATOR)
		if !ctx.SFX {
			crunched = append([]byte{byte(optimalRun - 1)}, crunched...)
		}
	}

	return crunched
}

func main() {
	var ctx crunchCtx
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

	sourceLen := len(src)

	if ctx.PRG {
		ctx.addr = src[:2]
		src = src[2:]

		ctx.decrunchTo = uint16(ctx.addr[0]) + 256*uint16(ctx.addr[1])
	}

	crunched := crunch(src, ctx)
	crunchedSize := len(crunched)

	if ctx.SFX {
		fileLen := len(boot) + len(crunched)
		startAddress := 0x10000 - len(crunched)
		transfAddress := fileLen + 0x6ff

		boot[0x1e] = byte(transfAddress & 0xff) //transfer from
		boot[0x1f] = byte(transfAddress >> 8)

		boot[0x3c] = byte(startAddress & 0xff) //Depack from..
		boot[0x3d] = byte(startAddress >> 8)

		boot[0x40] = byte(ctx.decrunchTo & 0xff) //decrunch to..
		boot[0x41] = byte(ctx.decrunchTo >> 8)

		boot[0x77] = byte(ctx.jmp & 0xff) // Jump to..
		boot[0x78] = byte(ctx.jmp >> 8)

		boot[0xc9] = byte(optimalRun - 1)

		crunched = append(boot, crunched...)

		crunchedSize += len(boot)
		ctx.loadTo = 0x0801
	}

	decrunchEnd := uint16(int(ctx.decrunchTo) + len(src) - 1)

	if ctx.INPLACE {
		ctx.loadTo = decrunchEnd - uint16(len(crunched)) + 1
		crunched = append([]byte{byte(ctx.loadTo & 255), byte(ctx.loadTo >> 8)}, crunched...)
	}

	save_raw(flag.Args()[ofidx], crunched)

	if !ctx.QUIET {
		ratio := (float32(crunchedSize) * 100.0 / float32(sourceLen))
		prg := "RAW"
		dest_prg := "RAW"
		if ctx.PRG {
			prg = "PRG"
		}
		if ctx.SFX || ctx.INPLACE {
			dest_prg = "prg"
		}
		fmt.Printf("input file  %s: %s, $%04x - $%04x : %d bytes\n",
			prg, flag.Args()[ifidx], ctx.decrunchTo, decrunchEnd, sourceLen)
		fmt.Printf("output file %s: %s, $%04x - $%04x : %d bytes\n",
			dest_prg, flag.Args()[ofidx], ctx.loadTo, crunchedSize+int(ctx.loadTo)-1, crunchedSize)
		fmt.Printf("crunched to %.2f%% of original size\n", ratio)
	}
}
