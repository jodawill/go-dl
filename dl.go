// TODO: Use semaphores to limit the number of concurrent downloads
// TODO: Allow retrying failed chunks
package main

import (
  "crypto/md5"
  "encoding/hex"
  "errors"
  "flag"
  "fmt"
  "github.com/google/uuid"
  "io"
  "os"
  "net/http"
  "strconv"
  "strings"
  "sync"
)

func downloadPart(connection connection, filename string, start int, end int) (err error) {
  req, err := http.NewRequest("GET", connection.url, nil)
  req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))

  response, err := connection.client.Do(req)

  if err != nil {
    return errors.New(fmt.Sprintf("requested for %s failed: %s", connection.url, err.Error()))
  }

  defer response.Body.Close()

  file, err := os.Create(filename)
  defer file.Close()

  if err != nil {
    fmt.Println(fmt.Sprintf("ERROR: Failed to create file %s", filename))
    os.Exit(1)
  }

  _, err = io.Copy(file, response.Body)
  if err != nil {
    fmt.Println(fmt.Sprintf("ERROR: Failed to write to file %s: %s", filename, err.Error()))
    os.Exit(1)
  }

  return
}

type connection struct {
  client *http.Client
  url string
}

type attributes struct {
  size int
  connections []connection
  checksum string
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
      url: url,
    }
    download_attributes.connections = append(download_attributes.connections, connection)
  }
  return download_attributes
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
  checksum = response.Header.Get("Etag")
  if strings.HasPrefix(checksum, "W/") {
    fmt.Println("ERROR: Checksum is a weak validator. Exiting because we cannot guarantee a consistent download.")
    os.Exit(1)
  }
  checksum = strings.Trim(checksum, "\"")
  return size, checksum, client, err
}

func mergeFiles(destination string, chunks []string) (err error) {
  out_file, err := os.Create(destination)
  if err != nil {
    return err
  }
  defer out_file.Close()

  for _, source := range chunks {
    in_file, err := os.Open(source)
    if err != nil {
      return err
    }
    defer in_file.Close()

    _, err = io.Copy(out_file, in_file)
    if err != nil {
      return err
    }
    in_file.Close()

    os.Remove(source)
  }
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

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
  return fmt.Sprintf("%v", *s)
}

func (s *stringSliceFlag) Set(value string) error {
  *s = append(*s, value)
  return nil
}

func printProgressBar(total int, progress_channel chan struct{}) {
  var progress int
  for range progress_channel {
    fmt.Printf("\rProgress: %.2f%%", float64(progress) / float64(total))
    progress++
    if progress == total {
      fmt.Println()
    }
  }
}

func main() {
  var destination string
  var urls stringSliceFlag
  var chunk_size int

  flag.StringVar(&destination, "dest", "download.dat", "Destination filename")
  flag.IntVar(&chunk_size, "chunk-size", 512*1024, "Size of download chunks in bytes")
  flag.Var(&urls, "url", "Add a source URL for the download")
  flag.Parse()

  if len(urls) == 0 {
    fmt.Println("ERROR: No source URLs provided")
    os.Exit(1)
  }

  download_attributes := getDownloadProperties(urls)

  fmt.Println("File size:", download_attributes.size)
  fmt.Println("Checksum:", download_attributes.checksum)
  fmt.Println(fmt.Sprintf("%v connections established", len(download_attributes.connections)))

  chunk_count := download_attributes.size/chunk_size
  if download_attributes.size % chunk_size > 0 {
    chunk_count++
  }
  chunks := make([]string, chunk_count)

  wait_group := sync.WaitGroup{}
  var mu sync.Mutex
  progress_channel := make(chan struct{})

  go printProgressBar(chunk_count, progress_channel)

  for chunk := 0; chunk < chunk_count; chunk++ {
    wait_group.Add(1)
    go func(chunk int, chunks []string) {
      connection := download_attributes.connections[chunk % len(download_attributes.connections)]
      filename := fmt.Sprintf("%s.part", uuid.New().String())
      err := downloadPart(
        connection,
        filename,
        chunk*chunk_size,
        (chunk+1)*chunk_size-1,
      )

      if err != nil {
        fmt.Println(fmt.Sprintf("ERROR: Chunk %v failed on %s: %s", chunk+1, connection.url, err.Error()))
        os.Exit(1)
      }

      mu.Lock()
      chunks[chunk] = filename
      mu.Unlock()
      progress_channel <- struct{}{}
      wait_group.Done()
    }(chunk, chunks)
  }
  wait_group.Wait()
  close(progress_channel)

  err := mergeFiles(destination, chunks)
  if err != nil {
    fmt.Println("ERROR: Failed to merge chunk files:", err)
  }

  if len(download_attributes.checksum) == 0 {
    fmt.Println("Download complete. No checksum available.")
    return
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
