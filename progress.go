package main

import (
	"fmt"
	"io"
	"sync"
)

type progressMessage struct {
	bytes     int
	message   string
	source_id  int
}

type progressWriter struct {
	file_writer      io.Writer
	progress_channel chan<- progressMessage
	source_id        int
}

func (progress_writer *progressWriter) Write(data []byte) (n int, err error) {
	n, err = progress_writer.file_writer.Write(data)
	if err == nil {
		progress_writer.progress_channel <- progressMessage{bytes: n, source_id: progress_writer.source_id}
	}
	return
}

type source struct {
  id          int
	downloaded  int
}

func writeProgressBar(download_attributes attributes, total int, wait_group *sync.WaitGroup, progress_channel <-chan progressMessage) {
	defer wait_group.Done()
	var downloaded int

	sources := make([]source, len(download_attributes.connections))
	for _, connection := range download_attributes.connections {
		sources[connection.id] = source{
			id: connection.id,
			downloaded: 0,
		}
	}

	for message := range progress_channel {
		downloaded += message.bytes
		sources[message.source_id].downloaded += message.bytes

		if message.message != "" {
			fmt.Println("\033[2K\r" + message.message)
		}

		fmt.Printf(fmt.Sprintf("\rDownloading... %.2f%%%%", 100*float64(downloaded)/float64(total)))

		for id, source := range sources {
			fmt.Printf(fmt.Sprintf(" | Source %v: %v bytes", id+1, source.downloaded))
		}
	}
	fmt.Println()
}
