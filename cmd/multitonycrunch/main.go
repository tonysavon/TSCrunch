/*
TonyCrunch is a fork of Antonio Savona's TSCrunch.
Refactoring, including fast mode and multi-hack by burg.
*/
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/staD020/TSCrunch"
)

func usage() {
	fmt.Printf("TSCrunch %s - binary cruncher, by Antonio Savona\n", TSCrunch.Version)
	fmt.Println("Multi-hack and fast mode by burg, quickly compile multiple files")
	fmt.Println("Usage: tscrunch [-p] [-i] [-f] [-q] infile infile infile")
	fmt.Println(" -p  : input file is a prg, first 2 bytes are discarded.")
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

	inFiles := flag.Args()
	if len(inFiles) < 1 {
		return fmt.Errorf("not enough args")
	}

	crunchFiles(opt, inFiles)

	if !opt.QUIET {
		fmt.Printf("elapsed: %s\n", time.Since(t0))
	}
	return nil
}

func crunchFiles(opt TSCrunch.Options, ff []string) {
	wg := &sync.WaitGroup{}
	wg.Add(len(ff))
	for _, file := range ff {
		go func(file string) {
			defer wg.Done()
			t1 := time.Now()
			in, err := os.Open(file)
			if err != nil {
				log.Printf("error: %v\n", err)
				return
			}
			defer in.Close()
			t, err := TSCrunch.New(opt, in)
			if err != nil {
				log.Printf("error: %v\n", err)
				return
			}
			f, err := os.Create(file + ".lz")
			if err != nil {
				log.Printf("error: %v\n", err)
				return
			}
			defer f.Close()
			_, err = t.WriteTo(f)
			if err != nil {
				log.Printf("error: %v\n", err)
				return
			}

			if !opt.QUIET {
				fmt.Printf("crunching %q took %s\n\n", file, time.Since(t1))
			}
		}(file)
	}
	wg.Wait()
}
