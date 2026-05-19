package slm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

// download fetches url to dst and returns the hex SHA256 and byte count of the download.
// Partial files are deleted on any failure.
// progress receives (downloaded, total) bytes; total is -1 if the server doesn't report Content-Length.
func download(ctx context.Context, url, dst string, progress func(downloaded, total int64)) (sha256hex string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("slm: download: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("slm: download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("slm: download: unexpected status %s", resp.Status)
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0700)
	if err != nil {
		return "", 0, fmt.Errorf("slm: download: create file: %w", err)
	}

	ok := false
	defer func() {
		closeErr := f.Close()
		if !ok {
			_ = os.Remove(dst)
			return
		}
		// Success path: surface the close error by wiring it into the named return.
		if closeErr != nil && err == nil {
			err = fmt.Errorf("slm: download: close: %w", closeErr)
		}
	}()

	h := sha256.New()
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, fn: progress}
	n, err := io.Copy(io.MultiWriter(f, h), pr)
	if err != nil {
		return "", 0, fmt.Errorf("slm: download: write: %w", err)
	}

	ok = true
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// hashFile computes the SHA256 of the file at path and returns the hex digest and size.
func hashFile(path string) (sha256hex string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

type progressReader struct {
	r     io.Reader
	total int64
	read  int64
	fn    func(downloaded, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.fn != nil {
		p.fn(p.read, p.total)
	}
	return n, err
}
