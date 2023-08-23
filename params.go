package main

import (
	"flag"
	"fmt"
	"os"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func parseParams() (urls stringSliceFlag, destination string, chunkSize int) {
	flag.StringVar(&destination, "dest", "download.dat", "Destination filename")
	flag.Var(&urls, "url", "Add a source URL for the download")
	flag.IntVar(&chunkSize, "chunk-size", 512*1024, "Chunk size in bytes")
	flag.Parse()

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Println("ERROR: No source URLs provided")
		os.Exit(1)
	}

	return urls, destination, chunkSize
}
