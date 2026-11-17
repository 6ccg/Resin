package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebUIAssetCompressionAndCaching(t *testing.T) {
	const scriptPath = "assets/index-AbCd1234.js"
	const script = "console.log('resin')"
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(w, script); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	handler := newWebUIHandler(fstest.MapFS{
		"index.html":       {Data: []byte("<html>Resin</html>")},
		scriptPath:         {Data: []byte(script)},
		scriptPath + ".gz": {Data: compressed.Bytes()},
	})

	request := func(path, acceptEncoding string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/ui/"+path, nil)
		req.Header.Set("Accept-Encoding", acceptEncoding)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, res.Code)
		}
		return res
	}

	zipped := request(scriptPath, "br, gzip")
	if zipped.Header().Get("Content-Encoding") != "gzip" ||
		!strings.Contains(zipped.Header().Get("Content-Type"), "javascript") ||
		!bytes.Equal(zipped.Body.Bytes(), compressed.Bytes()) {
		t.Fatalf("gzip script was not served correctly: %v", zipped.Header())
	}
	plain := request(scriptPath, "*, gzip;q=0")
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != script {
		t.Fatal("client opted out of gzip but did not receive the plain script")
	}
	for _, res := range []*httptest.ResponseRecorder{zipped, plain} {
		if res.Header().Get("Vary") != "Accept-Encoding" ||
			res.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("incorrect script caching headers: %v", res.Header())
		}
	}
	if got := request("nodes", "gzip").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("SPA entry cache policy = %q", got)
	}
}
