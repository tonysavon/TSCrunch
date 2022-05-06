/*
TSCrunch binary cruncher, by Antonio Savona
*/

package TSCrunch

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/RyanCarrier/dijkstra"
)

type Options struct {
	QUIET      bool
	PRG        bool
	SFX        bool
	INPLACE    bool
	STATS      bool
	JumpTo     string
	jmp        uint16
	decrunchTo uint16
	loadTo     uint16
	addr       []byte
}

type tsc struct {
	options        Options
	src            []byte
	starts         map[int]bool
	startsMtx      sync.Mutex
	ends           map[int]bool
	endsMtx        sync.Mutex
	graph          map[edge]token
	graphMtx       sync.Mutex
	wg             sync.WaitGroup
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

func New(opt Options, r io.Reader) (*tsc, error) {
	if opt.JumpTo != "" {
		opt.SFX = true
		opt.loadTo = 0x0801
		opt.PRG = true
	}
	if opt.INPLACE {
		opt.PRG = true
	}
	if opt.SFX {
		if opt.JumpTo[0] == '$' {
			jmp, err := strconv.ParseUint(opt.JumpTo[1:], 16, 16)
			if err != nil {
				return nil, fmt.Errorf("unable to parse jump address %q: %w", opt.JumpTo, err)
			}
			opt.jmp = uint16(jmp)
		}
		if opt.jmp == 0 {
			return nil, fmt.Errorf("incorrect jump address %q", opt.JumpTo)
		}
	}
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("ReadAll failed for r %v", r)
	}
	if opt.PRG {
		opt.addr = src[:2]
		src = src[2:]
		opt.decrunchTo = uint16(opt.addr[0]) + 256*uint16(opt.addr[1])
	}

	t := &tsc{
		options: opt,
		src:     src,
		starts:  make(map[int]bool),
		ends:    make(map[int]bool),
		graph:   make(map[edge]token),
		//prefix arrays for efficient prefix search don't really improve performance, here
		//due to the small search window.
		usePrefixArray: true,
	}
	return t, nil
}

func (t *tsc) WriteTo(w io.Writer) (int64, error) {
	buf := t.crunch()
	decrunchEnd := uint16(int(t.options.decrunchTo) + len(t.src) - 1)
	if t.options.INPLACE {
		t.options.loadTo = decrunchEnd - uint16(len(buf)) + 1
		buf = append([]byte{byte(t.options.loadTo & 0xff), byte(t.options.loadTo >> 8)}, buf...)
	}

	n, err := w.Write(buf)
	if err != nil {
		return int64(n), err
	}

	if !t.options.QUIET {
		ratio := float32(len(buf)) * 100.0 / float32(len(t.src))
		srcPrg := "RAW"
		destPrg := "RAW"
		if t.options.PRG {
			srcPrg = "PRG"
		}
		if t.options.SFX || t.options.INPLACE {
			destPrg = "PRG"
		}
		fmt.Printf("input file  %s, $%04x - $%04x : %d bytes\n",
			srcPrg, t.options.decrunchTo, decrunchEnd, len(t.src))
		fmt.Printf("output file %s, $%04x - $%04x : %d bytes\n",
			destPrg, t.options.loadTo, len(buf)+int(t.options.loadTo)-1, len(buf))
		fmt.Printf("crunched to %.2f%% of original size\n", ratio)
	}

	return int64(n), err
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

func (t *tsc) fillPrefixArray() {
	t.prefixArray = make(map[[MINLZ]byte][]int)
	for i := 0; i < len(t.src)-MINLZ; i++ {
		t.prefixArray[*(*[MINLZ]byte)(t.src[i:])] = append(t.prefixArray[*(*[MINLZ]byte)(t.src[i:])], i)
	}
}

func (t *tsc) findall(prefix []byte, i int, minlz int) <-chan int {
	c := make(chan int)
	x0 := max(0, i-LZOFFSET)
	x1 := min(i+minlz-1, len(t.src))

	if t.usePrefixArray {
		parray := t.prefixArray[*(*[MINLZ]byte)(prefix[:MINLZ])]
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
				if parray[o] < i && bytes.Equal(t.src[parray[o]:parray[o]+minlz], prefix) {
					c <- parray[o]
				}
			}
			close(c)
		}()
	} else {
		f := 1
		go func() {
			for f >= 0 {
				f = bytes.LastIndex(t.src[x0:x1], prefix)
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

func (t *tsc) findOptimalZeroRun() int {
	zeroruns := make(map[int]int)
	var i = 0
	var j = 0
	for i < len(t.src)-1 {
		if t.src[i] == 0 {
			j = i + 1
			for j < len(t.src) && t.src[j] == 0 && j-i < 256 {
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

func (ts *tsc) tokenPayload(t token) []byte {

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
		return append([]byte{byte(LITERALMASK | t.size)}, ts.src[n0:n1]...)
	}
}

func (t *tsc) LZ(i int, size int, offset int, minlz int) token {
	var lz token
	lz.tokentype = LZID
	lz.i = i
	if i >= 0 {

		bestpos := i - 1
		bestlen := 0

		if len(t.src)-i >= minlz {
			prefixes := t.findall(t.src[i:i+minlz], i, minlz)
			for j := range prefixes {
				l := minlz
				for i+l < len(t.src) && l < LONGESTLONGLZ && t.src[j+l] == t.src[i+l] {
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

func (t *tsc) RLE(i int, size int, rlebyte byte) token {
	var rle token
	rle.tokentype = RLEID
	rle.i = i
	if i >= 0 {
		rle.rlebyte = t.src[i]
		x := 0
		for i+x < len(t.src) && x < LONGESTRLE && t.src[i+x] == t.src[i] {
			x++
		}
		rle.size = x
	} else {
		rle.size = size
		rle.rlebyte = rlebyte
	}
	return rle
}

func (t *tsc) ZERORUN(i int) token {
	var zero token
	zero.tokentype = ZERORUNID

	zero.i = i
	zero.rlebyte = 0
	zero.size = 0

	if i >= 0 {
		var x int
		for x = 0; x < t.optimalRun && i+x < len(t.src) && t.src[i+x] == 0; x++ {
		}
		if x == t.optimalRun {
			zero.size = t.optimalRun
		}
	}
	return zero
}

func (t *tsc) LZ2(i int, size int, offset int) token {
	var lz2 token
	lz2.tokentype = LZ2ID

	lz2.offset = -1
	lz2.size = -1
	lz2.i = i

	if i >= 0 {
		if i+2 < len(t.src) {
			leftbound := max(0, i-LZ2OFFSET)
			lpart := t.src[leftbound : i+1]
			o := bytes.LastIndex(lpart, t.src[i:i+2])
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

//func crunchAtByte(src []byte, i int, tg *tokenGraph, ctx *crunchCtx) {
func (t *tsc) crunchAtByte(i int) {
	defer t.wg.Done()
	rle := t.RLE(i, 0, 0)
	//don't compute prefix for same bytes or this will explode
	//start computing for prefixes larger than RLE size
	var lz token
	if rle.size < LONGESTLONGLZ-1 {
		lz = t.LZ(i, 0, 0, rle.size+1)
	} else {
		lz = t.LZ(-1, -1, -1, -1) // start with a dummy lz
	}

	if lz.size >= MINLZ || rle.size >= MINRLE {
		t.startsMtx.Lock()
		t.starts[i] = true
		t.startsMtx.Unlock()
	}

	for size := lz.size; size >= MINLZ && size > rle.size; size-- {
		t.endsMtx.Lock()
		t.ends[i+size] = true
		t.endsMtx.Unlock()

		t.graphMtx.Lock()
		t.graph[edge{i, i + size}] = t.LZ(-1, size, lz.offset, MINLZ)
		t.graphMtx.Unlock()
	}

	for size := rle.size; size >= MINRLE; size-- {
		t.endsMtx.Lock()
		t.ends[i+size] = true
		t.endsMtx.Unlock()

		t.graphMtx.Lock()
		t.graph[edge{i, i + size}] = t.RLE(-1, size, t.src[i])
		t.graphMtx.Unlock()
	}

	if len(t.src)-i > 2 {
		lz2 := t.LZ2(i, 0, 0)
		if lz2.size == 2 {
			t.graphMtx.Lock()
			t.graph[edge{i, i + 2}] = lz2 //LZ2ID
			t.graphMtx.Unlock()

			t.startsMtx.Lock()
			t.starts[i] = true
			t.startsMtx.Unlock()

			t.endsMtx.Lock()
			t.ends[i+2] = true
			t.endsMtx.Unlock()
		}
	}

	zero := t.ZERORUN(i)
	if zero.size != 0 {
		t.graphMtx.Lock()
		t.graph[edge{i, i + t.optimalRun}] = zero
		t.graphMtx.Unlock()

		t.startsMtx.Lock()
		t.starts[i] = true
		t.startsMtx.Unlock()

		t.endsMtx.Lock()
		t.ends[i+t.optimalRun] = true
		t.endsMtx.Unlock()
	}
}

func (t *tsc) crunch() []byte {
	t.sourceLen = len(t.src)

	remainder := []byte{}
	var G = dijkstra.NewGraph()

	for i := 0; i < len(t.src)+1; i++ {
		G.AddVertex(i)
	}

	if t.options.INPLACE {
		remainder = t.src[len(t.src)-1:]
		t.src = t.src[:len(t.src)-1]
	}

	t.optimalRun = t.findOptimalZeroRun()

	if t.usePrefixArray {
		t.fillPrefixArray()
	}

	if !t.options.QUIET {
		fmt.Print("Populating LZ layer")
	}

	tm := time.Now()

	for i := 0; i < len(t.src); i++ {
		t.wg.Add(1)
		go t.crunchAtByte(i)
	}
	t.wg.Wait()

	if !t.options.QUIET {
		if t.options.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
	}

	t.starts[len(t.src)] = true
	t.ends[0] = true
	starts_ := make([]int, 0, len(t.starts))
	ends_ := make([]int, 0, len(t.ends))
	for k := range t.starts {
		starts_ = append(starts_, k)
	}
	for k := range t.ends {
		ends_ = append(ends_, k)
	}

	sort.Ints(starts_)
	sort.Ints(ends_)

	if !t.options.QUIET {
		fmt.Print("Closing Gaps")
	}

	e, s := 0, 0
	for e < len(ends_) && s < len(starts_) {
		end := ends_[e]
		if end < starts_[s] {
			//bridge
			for starts_[s]-end >= LONGESTLITERAL {
				key := edge{end, end + LONGESTLITERAL}
				_, haskey := t.graph[key]
				if !haskey {
					lit := LIT(end, LONGESTLITERAL)
					lit.size = LONGESTLITERAL
					t.graph[key] = lit
				}
				end += LONGESTLITERAL
			}

			for s0 := s; s0 < len(starts_) && starts_[s0]-end < LONGESTLITERAL; s0++ {
				key := edge{end, starts_[s0]}
				_, haskey := t.graph[key]
				if !haskey {
					lit := LIT(end, starts_[s0]-end)
					lit.size = starts_[s0] - end
					t.graph[key] = lit
				}
			}
			e++
		} else {
			s++
		}
	}

	if !t.options.QUIET {
		if t.options.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Populating Graph")
	}

	tm = time.Now()

	for k, t := range t.graph {
		G.AddArc(k.n0, k.n1, tokenCost(k.n0, k.n1, t.tokentype))
	}

	if !t.options.QUIET {
		if t.options.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
		fmt.Print("Computing shortest path")
	}

	tm = time.Now()

	best, _ := G.Shortest(0, len(t.src))

	if !t.options.QUIET {
		if t.options.STATS {
			fmt.Println(" ...", time.Since(tm))
		} else {
			fmt.Println()
		}
	}
	crunched := make([]byte, 0)
	token_list := make([]token, 0)

	for i := 0; i < len(best.Path)-1; i++ {
		e := edge{best.Path[i], best.Path[i+1]}
		token_list = append(token_list, t.graph[e])
	}

	if t.options.INPLACE {
		safety := len(token_list)
		segment_uncrunched_size := 0
		segment_crunched_size := 0
		total_uncrunched_size := 0
		for i := len(token_list) - 1; i >= 0; i-- {
			segment_crunched_size += len(t.tokenPayload(token_list[i])) //token size
			segment_uncrunched_size += token_list[i].size               //decrunched token raw size
			if segment_uncrunched_size <= segment_crunched_size+0 {
				safety = i
				total_uncrunched_size += segment_uncrunched_size
				segment_uncrunched_size = 0
				segment_crunched_size = 0
			}
		}
		for _, v := range token_list[:safety] {
			crunched = append(crunched, t.tokenPayload(v)...)
		}
		if total_uncrunched_size > 0 {
			remainder = append(t.src[len(t.src)-total_uncrunched_size:], remainder...)
		}
		crunched = append(crunched, TERMINATOR)
		crunched = append(crunched, remainder[1:]...)
		crunched = append(remainder[:1], crunched...)
		crunched = append([]byte{byte(t.optimalRun - 1)}, crunched...)
		crunched = append(t.options.addr, crunched...)

	} else {
		for _, v := range token_list {
			crunched = append(crunched, t.tokenPayload(v)...)
		}
		crunched = append(crunched, TERMINATOR)
		if !t.options.SFX {
			crunched = append([]byte{byte(t.optimalRun - 1)}, crunched...)
		}
	}

	t.crunchedSize = len(crunched)

	if t.options.SFX {
		boot := newBoot()
		fileLen := len(boot) + len(crunched)
		startAddress := 0x10000 - len(crunched)
		transfAddress := fileLen + 0x6ff

		boot[0x1e] = byte(transfAddress & 0xff) //transfer from
		boot[0x1f] = byte(transfAddress >> 8)

		boot[0x3c] = byte(startAddress & 0xff) //Depack from..
		boot[0x3d] = byte(startAddress >> 8)

		boot[0x40] = byte(t.options.decrunchTo & 0xff) //decrunch to..
		boot[0x41] = byte(t.options.decrunchTo >> 8)

		boot[0x77] = byte(t.options.jmp & 0xff) // Jump to..
		boot[0x78] = byte(t.options.jmp >> 8)

		boot[0xc9] = byte(t.optimalRun - 1)

		crunched = append(boot, crunched...)

		t.crunchedSize += len(boot)
		t.options.loadTo = 0x0801
	}

	t.decrunchEnd = uint16(int(t.options.decrunchTo) + len(t.src) - 1)

	if t.options.INPLACE {
		t.options.loadTo = t.decrunchEnd - uint16(len(crunched)) + 1
		crunched = append([]byte{byte(t.options.loadTo & 255), byte(t.options.loadTo >> 8)}, crunched...)
	}

	return crunched
}

//go:embed "boot.prg"
var bootPrg []byte

func newBoot() []byte {
	boot := make([]byte, len(bootPrg))
	copy(boot, bootPrg)
	return boot
}
