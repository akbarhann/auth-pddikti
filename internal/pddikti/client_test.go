package pddikti

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// All data below is fictitious.

type fakeUpstream struct {
	search      map[string][]StudentSearchResult // keyword -> results
	details     map[string]StudentDetail         // id -> detail
	status      int                              // force this status on every request
	rawBody     string                           // force this body on every request
	delay       time.Duration
	lastHeaders http.Header
	lastPath    string
}

func (f *fakeUpstream) handler(w http.ResponseWriter, r *http.Request) {
	f.lastHeaders = r.Header.Clone()
	f.lastPath = r.URL.EscapedPath()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.rawBody))
		return
	}
	if f.rawBody != "" {
		_, _ = w.Write([]byte(f.rawBody))
		return
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/pencarian/mhs/"):
		kw := strings.TrimPrefix(r.URL.Path, "/pencarian/mhs/")
		res, ok := f.search[kw]
		if !ok {
			// Mirrors upstream: no result -> 200 {"status":"success","data":null}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Status: "success", Data: res})
	case strings.HasPrefix(r.URL.Path, "/detail/mhs/"):
		id := strings.TrimPrefix(r.URL.Path, "/detail/mhs/")
		d, ok := f.details[id]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid id","status":"error"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(DetailResponse{Status: "success", Data: d})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestClient(t *testing.T, f *fakeUpstream) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return NewClient(WithBaseURL(srv.URL), WithTimeout(2*time.Second))
}

func student(id, nim, pt, singkatan string) (StudentSearchResult, StudentDetail) {
	return StudentSearchResult{ID: id, Nama: "CONTOH MAHASISWA", NIM: nim, NamaPT: pt, SingkatanPT: singkatan, NamaProdi: "TEKNIK INFORMATIKA"},
		StudentDetail{ID: id, Nama: "CONTOH MAHASISWA", NIM: nim, NamaPT: pt, KodePT: "001001 ", Prodi: "Teknik Informatika", StatusSaatIni: "Aktif-2026/2027 Ganjil"}
}

func TestVerifyNIM_ExactMatch(t *testing.T) {
	s, d := student("id-a", "23010000001", "UNIVERSITAS CONTOH", "UC")
	f := &fakeUpstream{
		search:  map[string][]StudentSearchResult{"23010000001": {s}},
		details: map[string]StudentDetail{"id-a": d},
	}
	got, err := newTestClient(t, f).VerifyNIM(" 23010000001 ", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NIM != "23010000001" {
		t.Fatalf("got %+v", got)
	}
}

// Regression: a partial NIM used to fall back to the first candidate and
// "verify" someone else.
func TestVerifyNIM_PartialNIMIsNotFound(t *testing.T) {
	s1, d1 := student("id-a", "23010000001", "UNIVERSITAS CONTOH", "UC")
	s2, d2 := student("id-b", "23010000002", "UNIVERSITAS CONTOH", "UC")
	f := &fakeUpstream{
		search:  map[string][]StudentSearchResult{"2301000000": {s1, s2}},
		details: map[string]StudentDetail{"id-a": d1, "id-b": d2},
	}
	_, err := newTestClient(t, f).VerifyNIM("2301000000", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestVerifyNIM_NoResult(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestClient(t, f).VerifyNIM("99999999999", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestVerifyNIM_EmptyNIM(t *testing.T) {
	_, err := newTestClient(t, &fakeUpstream{}).VerifyNIM("   ", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestVerifyNIM_AmbiguousAcrossCampuses(t *testing.T) {
	s1, d1 := student("id-a", "12345", "UNIVERSITAS CONTOH", "UC")
	s2, d2 := student("id-b", "12345", "POLITEKNIK CONTOH", "PC")
	f := &fakeUpstream{
		search:  map[string][]StudentSearchResult{"12345": {s1, s2}},
		details: map[string]StudentDetail{"id-a": d1, "id-b": d2},
	}
	c := newTestClient(t, f)

	_, err := c.VerifyNIM("12345", "")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("want *AmbiguousError, got %v", err)
	}
	if len(amb.Candidates) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(amb.Candidates))
	}

	for _, campus := range []string{"pc", "politeknik contoh", "Politeknik"} {
		got, err := c.VerifyNIM("12345", campus)
		if err != nil {
			t.Fatalf("campus %q: unexpected error %v", campus, err)
		}
		if got.ID != "id-b" {
			t.Fatalf("campus %q: got %s, want id-b", campus, got.ID)
		}
	}

	if _, err := c.VerifyNIM("12345", "INSTITUT LAIN"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown campus: want ErrNotFound, got %v", err)
	}
	// "CONTOH" matches both campuses -> still ambiguous.
	if _, err := c.VerifyNIM("12345", "contoh"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("broad campus: want ErrAmbiguous, got %v", err)
	}
}

func TestVerifyNIM_DetailNIMMismatch(t *testing.T) {
	s, _ := student("id-a", "23010000001", "UNIVERSITAS CONTOH", "UC")
	_, other := student("id-a", "99990000000", "UNIVERSITAS CONTOH", "UC")
	f := &fakeUpstream{
		search:  map[string][]StudentSearchResult{"23010000001": {s}},
		details: map[string]StudentDetail{"id-a": other},
	}
	if _, err := newTestClient(t, f).VerifyNIM("23010000001", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
