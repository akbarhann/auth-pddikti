package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auth-pddikti/internal/pddikti"
)

// All data below is fictitious.

type stubClient struct {
	searchRes []pddikti.StudentSearchResult
	detail    *pddikti.StudentDetail
	err       error
	gotNIM    string
	gotCampus string
}

func (s *stubClient) Search(string) ([]pddikti.StudentSearchResult, error) {
	return s.searchRes, s.err
}
func (s *stubClient) GetDetail(string) (*pddikti.StudentDetail, error) { return s.detail, s.err }
func (s *stubClient) VerifyNIM(nim, campus string) (*pddikti.StudentDetail, error) {
	s.gotNIM, s.gotCampus = nim, campus
	return s.detail, s.err
}

func init() { gin.SetMode(gin.TestMode) }

func newTestRouter(t *testing.T, c Searcher) http.Handler {
	t.Helper()
	return NewRouter(c)
}

func do(h http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", w.Body.String(), err)
	}
	return m
}

func activeStudent() *pddikti.StudentDetail {
	return &pddikti.StudentDetail{ID: "id-a", NIM: "23010000001", Nama: "CONTOH MAHASISWA", NamaPT: "Universitas Contoh", KodePT: "001001 ", StatusSaatIni: "Aktif-2026/2027 Ganjil"}
}

func TestHealth(t *testing.T) {
	w := do(newTestRouter(t, &stubClient{}), "GET", "/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d", w.Code)
	}
}

func TestVerify_Success(t *testing.T) {
	stub := &stubClient{detail: activeStudent()}
	h := newTestRouter(t, stub)

	w := do(h, "GET", "/api/verify/23010000001?kampus=UC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	m := decode(t, w)
	if m["verified"] != true || m["is_active"] != true || m["eligible"] != true {
		t.Fatalf("unexpected flags: %v", m)
	}
	if kode := m["student"].(map[string]any)["kode_pt"]; kode != "001001" {
		t.Fatalf("kode_pt not trimmed: %q", kode)
	}
	if stub.gotCampus != "UC" {
		t.Fatalf("kampus not forwarded: %q", stub.gotCampus)
	}

	w = do(h, "POST", "/api/verify", `{"nim":"23010000001","kampus":"Universitas Contoh"}`)
	if w.Code != http.StatusOK || stub.gotCampus != "Universitas Contoh" {
		t.Fatalf("POST status %d campus %q", w.Code, stub.gotCampus)
	}
}

func TestVerify_InactiveIsNotEligible(t *testing.T) {
	d := activeStudent()
	d.StatusSaatIni = "Dikeluarkan-2024/2025 Genap"
	m := decode(t, do(newTestRouter(t, &stubClient{detail: d}), "GET", "/api/verify/23010000001", ""))
	if m["is_active"] != false || m["eligible"] != false {
		t.Fatalf("inactive student must not be eligible: %v", m)
	}
}

func TestVerify_BadRequests(t *testing.T) {
	h := newTestRouter(t, &stubClient{})
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/verify", `{}`},
		{"POST", "/api/verify", `not json`},
		{"POST", "/api/verify", `{"nim":"   "}`},
		{"GET", "/api/verify/%20", ""},
	} {
		if w := do(h, tc.method, tc.path, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s %s: got %d, want 400", tc.method, tc.path, tc.body, w.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
		slug string
	}{
		{pddikti.ErrNotFound, http.StatusNotFound, "not_found"},
		{pddikti.ErrInvalidID, http.StatusBadRequest, "invalid_id"},
		{pddikti.ErrRateLimited, http.StatusTooManyRequests, "upstream_rate_limited"},
		{pddikti.ErrBlocked, http.StatusBadGateway, "upstream_blocked"},
		{pddikti.ErrTimeout, http.StatusGatewayTimeout, "upstream_timeout"},
		{fmt.Errorf("%w: status 500", pddikti.ErrUpstream), http.StatusBadGateway, "upstream_error"},
		{errors.New("unknown"), http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		h := newTestRouter(t, &stubClient{err: tc.err})
		for _, path := range []string{"/api/verify/123", "/api/detail/abc", "/api/search?q=abc"} {
			w := do(h, "GET", path, "")
			if w.Code != tc.code || decode(t, w)["code"] != tc.slug {
				t.Errorf("%v on %s: got %d %s, want %d %s", tc.err, path, w.Code, w.Body, tc.code, tc.slug)
			}
		}
	}
}

func TestErrorMapping_Ambiguous(t *testing.T) {
	err := &pddikti.AmbiguousError{NIM: "12345", Candidates: []pddikti.Campus{{NamaPT: "A"}, {NamaPT: "B"}}}
	w := do(newTestRouter(t, &stubClient{err: err}), "GET", "/api/verify/12345", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d", w.Code)
	}
	m := decode(t, w)
	if m["code"] != "ambiguous_nim" || len(m["candidates"].([]any)) != 2 {
		t.Fatalf("unexpected body: %v", m)
	}
}

func TestSearch(t *testing.T) {
	stub := &stubClient{searchRes: []pddikti.StudentSearchResult{{ID: "x", NIM: "1"}}}
	h := newTestRouter(t, stub)

	if w := do(h, "GET", "/api/search", ""); w.Code != http.StatusBadRequest {
		t.Errorf("missing q: got %d", w.Code)
	}
	w := do(h, "GET", "/api/search?q=abc", "")
	if w.Code != http.StatusOK || decode(t, w)["total"] != float64(1) {
		t.Errorf("valid q: got %d %s", w.Code, w.Body)
	}
}

func TestCORS(t *testing.T) {
	w := do(newTestRouter(t, &stubClient{}), "OPTIONS", "/api/verify", "", "Origin", "https://a.example")
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS: %d %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
}
