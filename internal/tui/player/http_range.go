package player

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type httpRangeReadSeeker struct {
	mu      sync.Mutex
	url     string
	headers map[string]string
	client  *http.Client
	size    int64
	pos     int64
	closed  bool
}

func openHTTPRangeReader(rawURL string, headers map[string]string) (*httpRangeReadSeeker, error) {
	r := &httpRangeReadSeeker{url: rawURL, headers: headers, client: http.DefaultClient}
	resp, err := r.request(0, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("server does not support byte ranges (status %d)", resp.StatusCode)
	}
	start, _, size, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil || start != 0 {
		return nil, fmt.Errorf("invalid byte range probe response")
	}
	r.size = size
	return r, nil
}

func (r *httpRangeReadSeeker) request(start, end int64) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range r.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("Accept-Encoding", "identity")
	return r.client.Do(req)
}

func (r *httpRangeReadSeeker) Read(dst []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}
	end := min(r.pos+int64(len(dst))-1, r.size-1)
	resp, err := r.request(r.pos, end)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("byte range request failed (status %d)", resp.StatusCode)
	}
	start, rangeEnd, size, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil || start != r.pos || rangeEnd < start || rangeEnd > end || size != r.size {
		return 0, fmt.Errorf("invalid byte range response")
	}
	requested := int(rangeEnd - start + 1)
	n, readErr := io.ReadFull(resp.Body, dst[:requested])
	r.pos += int64(n)
	if readErr == io.ErrUnexpectedEOF {
		return n, fmt.Errorf("truncated byte range response")
	}
	if readErr != nil {
		return n, readErr
	}
	return n, nil
}

func (r *httpRangeReadSeeker) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.pos, io.ErrClosedPipe
	}
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return r.pos, fmt.Errorf("invalid seek whence")
	}
	if next < 0 {
		return r.pos, fmt.Errorf("negative seek position")
	}
	r.pos = next
	return next, nil
}

func (r *httpRangeReadSeeker) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

func parseContentRange(value string) (start, end, size int64, err error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	positions := strings.Split(parts[0], "-")
	if len(positions) != 2 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	start, err = strconv.ParseInt(positions[0], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	end, err = strconv.ParseInt(positions[1], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	size, err = strconv.ParseInt(parts[1], 10, 64)
	return start, end, size, err
}
