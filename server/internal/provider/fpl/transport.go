package fpl

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// DirTransport answers FPL API requests from recorded JSON files instead
// of the network: GET <base>/event/7/live reads event/7/live.json from
// fsys. Missing files answer 404. Tests and the demo data use it, so
// neither ever calls FPL.
type DirTransport struct {
	FS fs.FS
}

// RoundTrip implements http.RoundTripper.
func (t DirTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	p := req.URL.Path
	if i := strings.Index(p, "/api/"); i >= 0 {
		p = p[i+len("/api/"):]
	}
	p = strings.Trim(path.Clean("/"+p), "/") + ".json"
	body, err := fs.ReadFile(t.FS, p)
	status := http.StatusOK
	switch {
	case errors.Is(err, fs.ErrNotExist):
		status, body = http.StatusNotFound, []byte(`{"detail":"Not found."}`)
	case err != nil:
		return nil, err
	}
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
		Proto:         "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
	}, nil
}
