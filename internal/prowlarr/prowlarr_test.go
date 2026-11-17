package prowlarr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"appName":"Prowlarr","version":"3.0.2"}`))
	})
	mux.HandleFunc("/api/v1/indexer", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"name":"NorBits","enable":true,"protocol":"torrent","privacy":"private"},{"id":2,"name":"Usenet","enable":true,"protocol":"usenet"}]`))
	})
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("query") != "farmen" || strings.Join(q["indexerIds"], ",") != "1" || strings.Join(q["categories"], ",") != "5000" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"title":"Farmen.S22","indexerId":1,"indexer":"NorBits","size":5,"seeders":3,"protocol":"torrent","downloadUrl":"` +
			"http://" + r.Host + `/1/download?apikey=key&link=x","indexerFlags":["freeleech"]},{"title":"nzb","protocol":"usenet"}]`))
	})
	mux.HandleFunc("/1/download", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("link") {
		case "magnet":
			http.Redirect(w, r, "magnet:?xt=urn:btih:abc", http.StatusFound)
		case "login":
			_, _ = w.Write([]byte("<html>Please log in</html>"))
		default:
			_, _ = w.Write([]byte("d4:infod4:name1:aee"))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClient(t *testing.T) {
	srv := server(t)
	ctx := context.Background()
	if c, _ := New(srv.URL, "wrong"); true {
		if _, err := c.Version(ctx); !errors.Is(err, ErrAuth) {
			t.Errorf("wrong key: %v", err)
		}
	}
	c, err := New(srv.URL+"/", "key")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := c.Version(ctx); err != nil || v != "3.0.2" {
		t.Errorf("version %q %v", v, err)
	}
	idx, err := c.Indexers(ctx)
	if err != nil || len(idx) != 1 || !idx[0].Private {
		t.Errorf("indexers %+v %v", idx, err)
	}
	res, err := c.Search(ctx, "farmen", []int{1}, []int{5000})
	if err != nil || len(res) != 1 || res[0].Title != "Farmen.S22" || *res[0].Seeders != 3 {
		t.Fatalf("search %+v %v", res, err)
	}
	f, err := c.Fetch(ctx, res[0])
	if err != nil || string(f.Torrent) != "d4:infod4:name1:aee" {
		t.Errorf("fetch %q %v", f.Torrent, err)
	}
	res[0].DownloadURL = strings.Replace(res[0].DownloadURL, "link=x", "link=magnet", 1)
	if f, err := c.Fetch(ctx, res[0]); err != nil || f.Magnet != "magnet:?xt=urn:btih:abc" {
		t.Errorf("magnet %+v %v", f, err)
	}
	res[0].DownloadURL = strings.Replace(res[0].DownloadURL, "link=magnet", "link=login", 1)
	if _, err := c.Fetch(ctx, res[0]); err == nil {
		t.Error("an HTML page was taken for a torrent")
	}
	if _, err := New("192.168.50.5:9696", "k"); err == nil {
		t.Error("an address without a scheme was accepted")
	}
}
