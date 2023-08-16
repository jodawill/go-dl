package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type progressMessage struct {
	bytes   int
	message string
}

type progressWriter struct {
	file_writer      io.Writer
	progress_channel chan<- progressMessage
}

func (progress_writer *progressWriter) Write(data []byte) (n int, err error) {
	n, err = progress_writer.file_writer.Write(data)
	if err == nil {
		progress_writer.progress_channel <- progressMessage{bytes: n}
	}
	return
}

func writeProgressBar(total int, wait_group *sync.WaitGroup, progress_channel <-chan progressMessage) {
	defer wait_group.Done()
	var downloaded int
	for message := range progress_channel {
		downloaded += message.bytes
		if message.message != "" {
			for i := 0; i < 1000; i++ {
				fmt.Printf("\b")
			}
			fmt.Println("\r" + message.message + strings.Repeat(" ", 21))
		}
		fmt.Printf(fmt.Sprintf("\rDownloading... %.2f%%%%", 100*float64(downloaded)/float64(total)))
	}
	fmt.Println()
}
