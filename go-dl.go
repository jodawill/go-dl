package main

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
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

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type chunk struct {
	filename string
	start    int
	end      int
}

func fetchChunk(connection connection, chunk chunk, progress_channel chan int) (err error) {
	req, err := http.NewRequest("GET", connection.url, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))

	response, err := connection.client.Do(req)

	if err != nil {
		return errors.New(fmt.Sprintf("requested for %s failed: %s", connection.url, err.Error()))
	}

	defer response.Body.Close()

	file, err := os.Create(chunk.filename)
	defer file.Close()

	if err != nil {
		fmt.Println(fmt.Sprintf("ERROR: Failed to create file %s", chunk.filename))
		os.Exit(1)
	}

	progress_writer := &progressWriter{
		file_writer:      io.MultiWriter(file),
		progress_channel: progress_channel,
	}

	_, err = io.Copy(progress_writer, response.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("ERROR: Failed to write to file %s: %s", chunk.filename, err.Error()))
		os.Exit(1)
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

func chunkWorker(connection connection, wait_group *sync.WaitGroup, progress_channel chan int, queue chan chunk) (err error) {
	defer wait_group.Done()
	for chunk := range queue {
		err = fetchChunk(connection, chunk, progress_channel)
	}
	return err
}

func fetchFile(chunks []chunk, download_attributes attributes) (err error) {
	wait_group := sync.WaitGroup{}
	progress_wait_group := sync.WaitGroup{}
	progress_channel := make(chan int)
	queue := make(chan chunk)

	for i := 0; i < len(download_attributes.connections); i++ {
		wait_group.Add(1)
		go chunkWorker(download_attributes.connections[i], &wait_group, progress_channel, queue)
	}

	go writeProgressBar(download_attributes.size, &progress_wait_group, progress_channel)
	progress_wait_group.Add(1)

	for i := 0; i < len(chunks); i++ {
		queue <- chunks[i]
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

func parseParams() (urls stringSliceFlag, destination string) {
	flag.StringVar(&destination, "dest", "download.dat", "Destination filename")
	flag.Var(&urls, "url", "Add a source URL for the download")
	flag.Parse()

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Println("ERROR: No source URLs provided")
		os.Exit(1)
	}

	return urls, destination
}

func main() {
	urls, destination := parseParams()
	download_attributes := getDownloadProperties(urls)
	displayFileInfo(download_attributes)
	chunks := initializeChunks(download_attributes)

	err := fetchFile(chunks, download_attributes)
	if err != nil {
		fmt.Println(fmt.Sprintf("ERROR: Failed to fetch file: %s", err.Error()))
	}

	fmt.Println("Merging temporary files into", destination)
	err = mergeFiles(destination, chunks)
	if err != nil {
		fmt.Println("ERROR: Failed to merge chunk files:", err)
	}

	ok, err := checksumMatches(destination, download_attributes.checksum)
	if err != nil {
		fmt.Println("ERROR: Failed to compare checksums:", err)
	}
	if ok {
		fmt.Println("Checksum passed. Download successful!")
	} else {
		fmt.Println(fmt.Sprintf("Download failed; checksums don't match"))
	}
}
