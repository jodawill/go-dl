package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type progressMessage struct {
	bytes     int
	message   string
	source_id int
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
	id         int
	downloaded int
}

func formatBytes(bytes int) (output string) {
	if bytes > 1073741824 {
		return fmt.Sprintf("%.2f GiB", float64(bytes)/1073741824)
	}
	if bytes > 1048576 {
		return fmt.Sprintf("%.2f MiB", float64(bytes)/1048576)
	}
	if bytes > 1024 {
		return fmt.Sprintf("%.2f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%v bytes", bytes)
}

func writeProgressBar(download_attributes attributes, total int, wait_group *sync.WaitGroup, progress_channel <-chan progressMessage) {
	defer wait_group.Done()
	var downloaded int

	sources := make([]source, len(download_attributes.connections))
	for _, connection := range download_attributes.connections {
		sources[connection.id] = source{
			id:         connection.id,
			downloaded: 0,
		}
	}

	reset_channel := make(chan struct{})
	speed := "--"
	var last_bytes int
	last_time := time.Now()

	go func() {
		for {
			time.Sleep(2 * time.Second)
			reset_channel <- struct{}{}
		}
	}()

	for message := range progress_channel {
		downloaded += message.bytes
		sources[message.source_id].downloaded += message.bytes

		select {
		case _ = <-reset_channel:
			speed_bytes := (downloaded-last_bytes)/int((time.Now().UnixNano()-last_time.UnixNano())/1000000000)
			speed = formatBytes(speed_bytes) + "/s"
			last_bytes = downloaded
			last_time = time.Now()
		default:
			;
		}

		if message.message != "" {
			fmt.Println("\033[2K\r" + message.message)
		}

		fmt.Printf(fmt.Sprintf("\033[2K\rDownloading... %.2f%%%% | Speed: %s", 100*float64(downloaded)/float64(total), speed))

		for id, source := range sources {
			fmt.Printf(fmt.Sprintf(" | Source %v: %s", id+1, formatBytes(source.downloaded)))
		}
	}
	fmt.Println()
}
