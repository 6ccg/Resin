package api

import (
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	embeddedwebui "github.com/Resinat/Resin/webui"
)

var fingerprintedWebAsset = regexp.MustCompile(`^assets/.+-[a-zA-Z0-9_-]{8}\.[^/]+$`)

func registerEmbeddedWebUI(mux *http.ServeMux) {
	distFS, err := embeddedwebui.DistFS()
	if err != nil {
		log.Printf("WebUI embed disabled: %v", err)
		return
	}
	mux.Handle("/", newRootRedirectHandler())
	mux.Handle("/ui", newUIRootRedirectHandler())
	mux.Handle("/ui/", newWebUIHandler(distFS))
}

func newWebUIHandler(distFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/ui/") {
			http.NotFound(w, r)
			return
		}

		assetPath := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/ui/")), "/")
		if assetPath == "" || assetPath == "." {
			assetPath = "index.html"
		}
		if strings.HasSuffix(assetPath, ".gz") {
			http.NotFound(w, r)
			return
		}

		if info, err := fs.Stat(distFS, assetPath); err == nil && !info.IsDir() {
			if fingerprintedWebAsset.MatchString(assetPath) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if gzip, err := fs.Stat(distFS, assetPath+".gz"); err == nil && !gzip.IsDir() {
				w.Header().Add("Vary", "Accept-Encoding")
				if acceptsGzip(r.Header.Values("Accept-Encoding")) {
					w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(assetPath)))
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Content-Length", strconv.FormatInt(gzip.Size(), 10))
					assetPath += ".gz"
				}
			}
			http.ServeFileFS(w, r, distFS, assetPath)
			return
		}

		// Missing requests with file-like paths should remain 404.
		if path.Ext(assetPath) != "" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, distFS, "index.html")
	})
}

func acceptsGzip(values []string) bool {
	wildcard := false
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			coding, params, _ := strings.Cut(token, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding != "gzip" && coding != "*" {
				continue
			}
			allowed := true
			for param := range strings.SplitSeq(params, ";") {
				key, raw, _ := strings.Cut(param, "=")
				if strings.EqualFold(strings.TrimSpace(key), "q") {
					q, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
					allowed = err == nil && q > 0 && q <= 1
				}
			}
			if coding == "gzip" {
				return allowed
			}
			wildcard = allowed
		}
	}
	return wildcard
}

func newRootRedirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
}

func newUIRootRedirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ui" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
}
