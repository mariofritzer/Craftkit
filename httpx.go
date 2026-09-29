package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const appName = "CraftKit"

// appVersion is set at build time by the release workflow: -X main.appVersion=1.2.3
var appVersion = "dev"
var userAgent = "CraftKit/" + appVersion + " (+https://github.com/mariofritzer/craftkit)"

var httpClient = &http.Client{Timeout: 60 * time.Second}
var dlClient = &http.Client{Timeout: 30 * time.Minute}

// small in-memory cache for GET requests (metadata only)
var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

type cacheEntry struct {
	at   time.Time
	body []byte
}

type HTTPError struct {
	URL    string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	b := e.Body
	if len(b) > 200 {
		b = b[:200]
	}
	return fmt.Sprintf("HTTP %d bei %s %s", e.Status, e.URL, strings.TrimSpace(b))
}

func getBytes(url string, headers map[string]string, ttl time.Duration) ([]byte, error) {
	key := url
	for k, v := range headers {
		if k != "x-api-key" {
			key += "|" + k + "=" + v
		}
	}
	if ttl > 0 {
		cacheMu.Lock()
		if e, ok := cache[key]; ok && time.Since(e.at) < ttl {
			cacheMu.Unlock()
			return e.body, nil
		}
		cacheMu.Unlock()
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &HTTPError{url, resp.StatusCode, string(body)}
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			return nil, &HTTPError{url, resp.StatusCode, string(body)}
		}
		if ttl > 0 {
			cacheMu.Lock()
			cache[key] = cacheEntry{time.Now(), body}
			cacheMu.Unlock()
		}
		return body, nil
	}
	return nil, lastErr
}

// postJSON sends body as JSON and decodes the response into out.
func postJSON(url string, headers map[string]string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &HTTPError{url, resp.StatusCode, string(b)}
			continue
		}
		if resp.StatusCode != 200 {
			return &HTTPError{url, resp.StatusCode, string(b)}
		}
		return json.Unmarshal(b, out)
	}
	return lastErr
}

func getJSON(url string, headers map[string]string, ttl time.Duration, out any) error {
	b, err := getBytes(url, headers, ttl)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("Antwort von %s nicht lesbar: %w", url, err)
	}
	return nil
}

// Hash describes an expected checksum for a download.
type Hash struct {
	Algo  string `json:"algo"` // "sha1" or "sha512"
	Value string `json:"value"`
}

// download fetches url into dest atomically, verifying the hash if given.
// progress is called with (done, total) bytes; total may be -1.
func download(url, dest string, want *Hash, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		lastErr = downloadOnce(url, dest, want, progress)
		if lastErr == nil {
			return nil
		}
		var he *HTTPError
		if errors.As(lastErr, &he) && he.Status >= 400 && he.Status < 500 && he.Status != 429 {
			return lastErr
		}
	}
	return lastErr
}

func downloadOnce(url, dest string, want *Hash, progress func(done, total int64)) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return &HTTPError{url, resp.StatusCode, string(b)}
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var h hash.Hash
	if want != nil && want.Value != "" {
		switch want.Algo {
		case "sha1":
			h = sha1.New()
		case "sha512":
			h = sha512.New()
		}
	}
	var w io.Writer = f
	if h != nil {
		w = io.MultiWriter(f, h)
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 150*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if h != nil {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, want.Value) {
			os.Remove(tmp)
			return fmt.Errorf("Prüfsumme stimmt nicht für %s (Datei beschädigt?)", filepath.Base(dest))
		}
	}
	os.Remove(dest)
	return os.Rename(tmp, dest)
}
