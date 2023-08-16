package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type attributes struct {
	size        int
	connections []connection
	checksum    string
}

type connection struct {
	client *http.Client
	url    string
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
	length_string := response.Header.Get("Content-Length")
	size, _ = strconv.Atoi(length_string)
	checksum = parseEtag(response.Header.Get("Etag"))

	return size, checksum, client, err
}

func getDownloadProperties(urls []string) attributes {
	download_attributes := attributes{}
	for _, url := range urls {
		size, checksum, client, err := getFilePropertiesFromURL(url)

		if err != nil {
			fmt.Println(fmt.Sprintf("WARNING: Not using %s because head request failed: %s", url, err.Error()))
			continue
		}

		if download_attributes.checksum != "" && checksum != "" && checksum != download_attributes.checksum {
			fmt.Println(fmt.Sprintf("WARNING: Checksum for %s does not match what was found on previous url. Ignoring this source.", url))
			continue
		}

		if download_attributes.size != 0 && size != download_attributes.size {
			fmt.Println(fmt.Sprintf("WARNING: Size for %s (%v) does not match what was found on previous url (%v). Ignoring this source.", url, size, download_attributes.size))
			continue
		}

		download_attributes.size = size
		download_attributes.checksum = checksum
		connection := connection{
			client: client,
			url:    url,
		}
		download_attributes.connections = append(download_attributes.connections, connection)
	}
	return download_attributes
}

func fetchFile(chunks []chunk, download_attributes attributes) (err error) {
	wait_group := sync.WaitGroup{}
	progress_wait_group := sync.WaitGroup{}
	progress_channel := make(chan progressMessage)
	queue := make(chan chunk)

	for _, connection := range download_attributes.connections {
		wait_group.Add(1)
		go chunkWorker(connection, &wait_group, progress_channel, queue)
	}

	go writeProgressBar(download_attributes.size, &progress_wait_group, progress_channel)
	progress_wait_group.Add(1)

	for _, chunk := range chunks {
		queue <- chunk
	}
	close(queue)

	wait_group.Wait()
	close(progress_channel)
	progress_wait_group.Wait()

	return err
}

func checksumMatches(filename string, desired_checksum string) (ok bool, err error) {
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
	return sum == desired_checksum, nil
}

func mergeFiles(destination string, chunks []chunk) (err error) {
	out_file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer out_file.Close()

	for _, chunk := range chunks {
		in_file, err := os.Open(chunk.filename)
		if err != nil {
			return err
		}

		_, err = io.Copy(out_file, in_file)
		if err != nil {
			return err
		}
		in_file.Close()

		os.Remove(chunk.filename)
	}
	return err
}

func displayFileInfo(download_attributes attributes) {
	fmt.Println("=============== File Information ==============")
	fmt.Println("File size:", download_attributes.size)
	fmt.Println("Checksum:", download_attributes.checksum)
	fmt.Println(fmt.Sprintf("Connections: %v", len(download_attributes.connections)))
	fmt.Println("===============================================")
}

func main() {
	urls, destination := parseParams()
	download_attributes := getDownloadProperties(urls)
	displayFileInfo(download_attributes)
	chunks := initializeChunks(download_attributes)

	err := fetchFile(chunks, download_attributes)
	if err != nil {
		panic(fmt.Sprintf("ERROR: Failed to fetch file: %s", err.Error()))
	}

	fmt.Println("Merging temporary files into", destination)
	err = mergeFiles(destination, chunks)
	if err != nil {
		panic("ERROR: Failed to merge chunk files:", err)
	}

	ok, err := checksumMatches(destination, download_attributes.checksum)
	if err != nil {
		panic("ERROR: Failed to compare checksums:", err)
	}
	if ok {
		fmt.Println("Checksum passed. Download successful!")
	} else {
		fmt.Println(fmt.Sprintf("Download failed; checksums don't match"))
	}
}
