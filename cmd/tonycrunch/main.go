/*
TonyCrunch is a fork of Antonio Savona's TSCrunch.
Refactoring, including fast mode by burg.
*/
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"time"

	"github.com/staD020/TSCrunch"
)

func usage() {
	fmt.Printf("TSCrunch %s - binary cruncher, by Antonio Savona\n", TSCrunch.Version)
	fmt.Println("Fast mode by burg")
	fmt.Println("Usage: tscrunch [-p] [-i] [-f] [-q] [-x $081f|0x081f|2079] infile outfile")
	fmt.Println(" -p  : input file is a prg, first 2 bytes are discarded.")
	fmt.Println(" -x  $addr: creates a self extracting file (forces -p)")
	fmt.Println(" -i  : inplace crunching (forces -p)")
	fmt.Println(" -q  : quiet mode")
	fmt.Println(" -f  : fast mode")
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v\n", err)
		usage()
		return
	}
}

func run() error {
	t0 := time.Now()
	opt := TSCrunch.Options{STATS: true}
	var cpuProfile string
	flag.StringVar(&cpuProfile, "cpuprofile", "", "write cpu profile to `file`")
	flag.BoolVar(&opt.PRG, "p", false, "")
	flag.BoolVar(&opt.QUIET, "q", false, "")
	flag.BoolVar(&opt.INPLACE, "i", false, "")
	flag.BoolVar(&opt.Fast, "f", false, "")
	flag.StringVar(&opt.JumpTo, "x", "", "")
	flag.Usage = usage
	flag.Parse()

	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			return fmt.Errorf("could not create CPU profile %q: %w", cpuProfile, err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("could not start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if flag.NArg() != 2 {
		return fmt.Errorf("not enough args")
	}

	inFilename := flag.Args()[0]
	outFilename := flag.Args()[1]
	in, err := os.Open(inFilename)
	if err != nil {
		return err
	}
	defer in.Close()
	t, err := TSCrunch.New(opt, in)
	if err != nil {
		return err
	}
	out, err := os.Create(outFilename)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = t.WriteTo(out)
	if err != nil {
		return err
	}
	if !opt.QUIET {
		fmt.Printf("elapsed: %s\n", time.Since(t0))
	}
	return nil
}
