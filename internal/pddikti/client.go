package pddikti

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://pddikti.kemdiktisaintek.go.id/api"
	DefaultTimeout = 8 * time.Second
	UserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
	Origin         = "https://pddikti.kemdiktisaintek.go.id"
	Referer        = "https://pddikti.kemdiktisaintek.go.id/"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another upstream (used by tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithTimeout sets the upstream HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// StudentSearchResult represents an item returned from /api/pencarian/mhs/{keyword}
type StudentSearchResult struct {
	ID          string `json:"id"`
	Nama        string `json:"nama"`
	NIM         string `json:"nim"`
	NamaPT      string `json:"nama_pt"`
	SingkatanPT string `json:"sinkatan_pt"`
	NamaProdi   string `json:"nama_prodi"`
}

type SearchResponse struct {
	Status  string                `json:"status"`
	Message string                `json:"message,omitempty"`
	Data    []StudentSearchResult `json:"data"`
}

// StudentDetail represents full academic details returned from /api/detail/mhs/{id}
type StudentDetail struct {
	ID            string `json:"id"`
	NamaPT        string `json:"nama_pt"`
	KodePT        string `json:"kode_pt"`
	KodeProdi     string `json:"kode_prodi"`
	Prodi         string `json:"prodi"`
	Nama          string `json:"nama"`
	NIM           string `json:"nim"`
	JenisDaftar   string `json:"jenis_daftar"`
	IDPT          string `json:"id_pt"`
	IDSMS         string `json:"id_sms"`
	JenisKelamin  string `json:"jenis_kelamin"`
	Jenjang       string `json:"jenjang"`
	StatusSaatIni string `json:"status_saat_ini"` // e.g. "Aktif-2026/2027 Ganjil"
	TanggalMasuk  string `json:"tanggal_masuk"`
}

type DetailResponse struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Data    StudentDetail `json:"data"`
}

func (c *Client) prepareRequest(endpoint string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	return req, nil
}

// Search queries student by NIM or Name
func (c *Client) Search(keyword string) ([]StudentSearchResult, error) {
	cleanKeyword := strings.TrimSpace(keyword)
	// Clean slashes to avoid routing errors
	cleanKeyword = strings.ReplaceAll(cleanKeyword, "/", " ")
	encoded := url.PathEscape(cleanKeyword)

	endpoint := c.baseURL + "/pencarian/mhs/" + encoded
	req, err := c.prepareRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error reaching pddikti: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("pddikti WAF challenge or missing headers (403 Forbidden)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("pddikti rate limit exceeded (60 req/min limit)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pddikti returned status %d", resp.StatusCode)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return searchResp.Data, nil
}

// GetDetail retrieves full student information using encrypted ID
func (c *Client) GetDetail(id string) (*StudentDetail, error) {
	endpoint := c.baseURL + "/detail/mhs/" + url.PathEscape(id)
	req, err := c.prepareRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("detail endpoint returned status %d", resp.StatusCode)
	}

	var detailResp DetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&detailResp); err != nil {
		return nil, fmt.Errorf("failed to decode detail response: %w", err)
	}

	return &detailResp.Data, nil
}

// VerifyNIM finds the student whose NIM matches nim exactly and returns the
// full detail. It never falls back to a partial or fuzzy match.
//
// NIM is not unique nationwide: when several campuses share the same NIM,
// campus (nama PT or singkatan, case-insensitive; empty = no filter) is used
// to narrow it down, otherwise an *AmbiguousError is returned.
func (c *Client) VerifyNIM(nim, campus string) (*StudentDetail, error) {
	cleanNIM := strings.TrimSpace(nim)
	if cleanNIM == "" {
		return nil, ErrNotFound
	}

	candidates, err := c.Search(cleanNIM)
	if err != nil {
		return nil, err
	}

	var matches []StudentSearchResult
	for _, item := range candidates {
		if strings.TrimSpace(item.NIM) == cleanNIM && matchesCampus(item, campus) {
			matches = append(matches, item)
		}
	}

	switch len(matches) {
	case 0:
		return nil, ErrNotFound
	case 1:
	default:
		amb := &AmbiguousError{NIM: cleanNIM}
		for _, m := range matches {
			amb.Candidates = append(amb.Candidates, Campus{
				NamaPT:      strings.TrimSpace(m.NamaPT),
				SingkatanPT: strings.TrimSpace(m.SingkatanPT),
				NamaProdi:   strings.TrimSpace(m.NamaProdi),
			})
		}
		return nil, amb
	}

	detail, err := c.GetDetail(matches[0].ID)
	if err != nil {
		return nil, err
	}
	// Defence in depth: the detail must belong to the NIM that was asked for.
	if strings.TrimSpace(detail.NIM) != cleanNIM {
		return nil, ErrNotFound
	}
	return detail, nil
}

func matchesCampus(item StudentSearchResult, campus string) bool {
	campus = strings.ToUpper(strings.TrimSpace(campus))
	if campus == "" {
		return true
	}
	nama := strings.ToUpper(strings.TrimSpace(item.NamaPT))
	singkatan := strings.ToUpper(strings.TrimSpace(item.SingkatanPT))
	return (singkatan != "" && singkatan == campus) || strings.Contains(nama, campus)
}
