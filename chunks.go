package main

import (
	"errors"
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

func fetchChunk(connection connection, chunk chunk, progress_channel chan progressMessage) (err error) {
	req, err := http.NewRequest("GET", connection.url, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))

	response, err := connection.client.Do(req)

	if err != nil {
		return errors.New(fmt.Sprintf("requested for %s failed: %s", connection.url, err.Error()))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New(fmt.Sprintf("requested for %s failed with code %v", connection.url, response.StatusCode))
	}

	defer response.Body.Close()

	file, err := os.Create(chunk.filename)
	defer file.Close()

	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to create file %s", chunk.filename))
	}

	progress_writer := &progressWriter{
		file_writer:      io.MultiWriter(file),
		progress_channel: progress_channel,
	}

	_, err = io.Copy(progress_writer, response.Body)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to write to file %s: %s", chunk.filename, err.Error()))
	}

	return
}

func initializeChunks(download_attributes attributes) (chunks []chunk) {
	chunk_size := 512 * 1024
	for i := 0; i*chunk_size < download_attributes.size; i++ {
		chunk := chunk{
			filename: fmt.Sprintf("%s.part", uuid.New().String()),
			start:    i * chunk_size,
			end:      (i+1)*chunk_size - 1,
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func chunkWorker(connection connection, progress_channel chan progressMessage, queue chan chunk, chunk_counter_channel chan struct{}) (err error) {
	backoff := 1
	for chunk := range queue {
		err = fetchChunk(connection, chunk, progress_channel)
		if err != nil {
			progress_channel <- progressMessage{message: fmt.Sprintf("WARNING: failed to download chunk: %s", err.Error())}
			queue <- chunk
			time.Sleep(time.Duration(backoff) * time.Second)
			if backoff <= 32 {
				backoff *= 2
			}
		} else {
			chunk_counter_channel <- struct{}{}
			backoff = 1
		}
	}
	return err
}
