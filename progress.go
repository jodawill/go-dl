package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type progressMessage struct {
	bytes    int
	message  string
	sourceID int
}

type progressWriter struct {
	fileWriter      io.Writer
	progressChannel chan<- progressMessage
	sourceID        int
}

func (progressWriter *progressWriter) Write(data []byte) (n int, err error) {
	n, err = progressWriter.fileWriter.Write(data)
	if err == nil {
		progressWriter.progressChannel <- progressMessage{bytes: n, sourceID: progressWriter.sourceID}
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

func writeProgressBar(downloadAttributes attributes, total int, waitGroup *sync.WaitGroup, progressChannel <-chan progressMessage) {
	defer waitGroup.Done()
	var downloaded int

	sources := make([]source, len(downloadAttributes.connections))
	for _, connection := range downloadAttributes.connections {
		sources[connection.id] = source{
			id:         connection.id,
			downloaded: 0,
		}
	}

	resetChannel := make(chan struct{})
	speed := "--"
	var lastBytes int
	lastTime := time.Now()

	go func() {
		for {
			time.Sleep(2 * time.Second)
			resetChannel <- struct{}{}
		}
	}()

	for message := range progressChannel {
		downloaded += message.bytes
		sources[message.sourceID].downloaded += message.bytes

		select {
		case _ = <-resetChannel:
			speedBytes := (downloaded - lastBytes) / int((time.Now().UnixNano()-lastTime.UnixNano())/1000000000)
			speed = formatBytes(speedBytes) + "/s"
			lastBytes = downloaded
			lastTime = time.Now()
		default:

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
