package main

import (
	"fmt"
	"io"
	"sync"
)

type progressWriter struct {
	file_writer      io.Writer
	progress_channel chan<- int
}

func (progress_writer *progressWriter) Write(data []byte) (n int, err error) {
	n, err = progress_writer.file_writer.Write(data)
	if err == nil {
		progress_writer.progress_channel <- n
	}
	return
}

func writeProgressBar(total int, wait_group *sync.WaitGroup, progress_channel <-chan int) {
	defer wait_group.Done()
	var downloaded int
	for bytes := range progress_channel {
		downloaded += bytes
		fmt.Printf(fmt.Sprintf("\rDownloading... %.2f%%%%", 100*float64(downloaded)/float64(total)))
	}
	fmt.Println()
}
