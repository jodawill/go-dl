package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type attributes struct {
	size        int
	connections []connection
	checksum    string
}

type connection struct {
	client *http.Client
	url    string
	id     int
}

func parseEtag(etag string) (checksum string) {
	if strings.HasPrefix(etag, "W/") {
		panic("ERROR: Checksum is a weak validator. Exiting because we cannot guarantee a consistent download.")
	}

	checksum = strings.Trim(etag, "\"")

	re := regexp.MustCompile(`-\d+$`)
	if re.MatchString(checksum) {
		checksum = ""
	}

	return checksum
}

func getFilePropertiesFromURL(url string) (size int, checksum string, client *http.Client, err error) {
	client = &http.Client{}
	req, err := http.NewRequest("HEAD", url, nil)

	if err != nil {
		return size, checksum, client, err
	}
	response, err := client.Do(req)
	if err != nil {
		return size, checksum, client, err
	}
	lengthString := response.Header.Get("Content-Length")
	size, _ = strconv.Atoi(lengthString)
	checksum = parseEtag(response.Header.Get("Etag"))

	return size, checksum, client, err
}

func getDownloadProperties(urls []string) attributes {
	downloadAttributes := attributes{}
	for i, url := range urls {
		size, checksum, client, err := getFilePropertiesFromURL(url)

		if err != nil {
			fmt.Println(fmt.Sprintf("WARNING: Not using %s because head request failed: %s", url, err.Error()))
			continue
		}

		if downloadAttributes.checksum != "" && checksum != "" && checksum != downloadAttributes.checksum {
			fmt.Println(fmt.Sprintf("WARNING: Checksum for %s does not match what was found on previous url. Ignoring this source.", url))
			continue
		}

		if downloadAttributes.size != 0 && size != downloadAttributes.size {
			fmt.Println(fmt.Sprintf("WARNING: Size for %s (%v) does not match what was found on previous url (%v). Ignoring this source.", url, size, downloadAttributes.size))
			continue
		}

		downloadAttributes.size = size
		downloadAttributes.checksum = checksum
		connection := connection{
			client: client,
			url:    url,
			id:     i,
		}
		downloadAttributes.connections = append(downloadAttributes.connections, connection)
	}
	return downloadAttributes
}

func fetchFile(chunks []chunk, downloadAttributes attributes) (err error) {
	progressWaitGroup := sync.WaitGroup{}
	progressChannel := make(chan progressMessage)
	chunkCounterChannel := make(chan struct{})
	queue := make(chan chunk, len(chunks))
	defer close(queue)

	for _, chunk := range chunks {
		queue <- chunk
	}

	for _, connection := range downloadAttributes.connections {
		go chunkWorker(connection, progressChannel, queue, chunkCounterChannel)
	}

	go writeProgressBar(downloadAttributes, downloadAttributes.size, &progressWaitGroup, progressChannel)
	progressWaitGroup.Add(1)

	for i := 0; i < len(chunks); i++ {
		<-chunkCounterChannel
	}

	close(progressChannel)
	progressWaitGroup.Wait()

	return err
}

func checksumMatches(filename string, desiredChecksum string) (ok bool, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hash := md5.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return false, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	return sum == desiredChecksum, nil
}

func mergeFiles(destination string, chunks []chunk) (err error) {
	outFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer outFile.Close()

	for _, chunk := range chunks {
		inFile, err := os.Open(chunk.filename)
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, inFile)
		if err != nil {
			return err
		}
		inFile.Close()
	}
	return err
}

func removeChunkFiles(chunks []chunk) {
	for _, chunk := range chunks {
		os.Remove(chunk.filename)
	}
}

func displayFileInfo(downloadAttributes attributes) {
	fmt.Println("=============== File Information ==============")
	fmt.Println("File size:", downloadAttributes.size)
	fmt.Println("Checksum:", downloadAttributes.checksum)
	fmt.Println(fmt.Sprintf("Connections: %v", len(downloadAttributes.connections)))
	fmt.Println("===============================================")
}

func main() {
	urls, destination, chunkSize := parseParams()
	downloadAttributes := getDownloadProperties(urls)
	displayFileInfo(downloadAttributes)
	chunks := initializeChunks(downloadAttributes, chunkSize)
	defer removeChunkFiles(chunks)

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range signalChannel {
			removeChunkFiles(chunks)
			fmt.Println()
			os.Exit(0)
		}
	}()

	err := fetchFile(chunks, downloadAttributes)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to fetch file: %s", err.Error()))
	}

	fmt.Println("Merging temporary files into", destination)
	err = mergeFiles(destination, chunks)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to merge chunk files: %s", err.Error()))
	}

	if downloadAttributes.checksum == "" {
		fmt.Println("No checksum available. Assuming a successful download.")
		return
	}

	ok, err := checksumMatches(destination, downloadAttributes.checksum)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to compare checksums: %s", err.Error()))
	}
	if ok {
		fmt.Println("Checksum passed. Download successful!")
	} else {
		panic(fmt.Sprintf("Download failed; checksums don't match"))
	}
}
