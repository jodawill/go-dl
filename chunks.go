package main

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"os"
	"time"
)

type chunk struct {
	filename string
	start    int
	end      int
}

func fetchChunk(connection connection, chunk chunk, progressChannel chan progressMessage) (err error) {
	req, err := http.NewRequest("GET", connection.url, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))

	response, err := connection.client.Do(req)

	if err != nil {
		return fmt.Errorf("requested for %s failed: %s", connection.url, err.Error())
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("requested for %s failed with code %v", connection.url, response.StatusCode)
	}

	defer response.Body.Close()

	file, err := os.Create(chunk.filename)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to create file %s", chunk.filename))
	}
	defer file.Close()

	progressWriter := &progressWriter{
		fileWriter:      io.MultiWriter(file),
		progressChannel: progressChannel,
		sourceID:        connection.id,
	}

	_, err = io.Copy(progressWriter, response.Body)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to write to file %s: %s", chunk.filename, err.Error()))
	}

	return
}

func initializeChunks(downloadAttributes attributes, chunkSize int) (chunks []chunk) {
	for i := 0; i*chunkSize < downloadAttributes.size; i++ {
		chunk := chunk{
			filename: fmt.Sprintf("%s.part", uuid.New().String()),
			start:    i * chunkSize,
			end:      (i+1)*chunkSize - 1,
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func chunkWorker(connection connection, progressChannel chan progressMessage, queue chan chunk, chunkCounterChannel chan struct{}) (err error) {
	backoff := 1
	for chunk := range queue {
		err = fetchChunk(connection, chunk, progressChannel)
		if err != nil {
			progressChannel <- progressMessage{message: fmt.Sprintf("WARNING: failed to download chunk: %s", err.Error())}
			queue <- chunk
			time.Sleep(time.Duration(backoff) * time.Second)
			if backoff <= 32 {
				backoff <<= 1
			}
		} else {
			chunkCounterChannel <- struct{}{}
			backoff = 1
		}
	}
	return err
}
