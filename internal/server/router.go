// Package server wires the HTTP API on top of the PDDIKTI client.
package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auth-pddikti/internal/pddikti"
)

// Searcher is the subset of *pddikti.Client the API needs.
type Searcher interface {
	Search(keyword string) ([]pddikti.StudentSearchResult, error)
	GetDetail(id string) (*pddikti.StudentDetail, error)
	VerifyNIM(nim, campus string) (*pddikti.StudentDetail, error)
}

func NewRouter(client Searcher) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(corsMiddleware())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "auth-pddikti",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	api := r.Group("/api")

	// GET /api/search?q=keyword -> candidate list
	api.GET("/search", func(c *gin.Context) {
		query := strings.TrimSpace(c.Query("q"))
		if query == "" {
			badRequest(c, "Query parameter 'q' is required")
			return
		}

		results, err := client.Search(query)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"query":  query,
			"total":  len(results),
			"data":   results,
		})
	})

	// GET /api/detail/:id -> full detail by encrypted ID
	api.GET("/detail/:id", func(c *gin.Context) {
		detail, err := client.GetDetail(c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": detail})
	})

	// GET /api/verify/:nim?kampus=...  |  POST /api/verify {"nim": "...", "kampus": "..."}
	handleVerify := func(c *gin.Context, nim, campus string) {
		nim = strings.TrimSpace(nim)
		if nim == "" {
			badRequest(c, "NIM is required")
			return
		}

		detail, err := client.VerifyNIM(nim, campus)
		if err != nil {
			writeError(c, err)
			return
		}

		isActive := detail.IsActive()
		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"verified":  true,
			"is_active": isActive,
			// eligible is what callers should gate access on.
			"eligible": isActive,
			"student": gin.H{
				"nim":             detail.NIM,
				"nama":            detail.Nama,
				"kampus":          detail.NamaPT,
				"kode_pt":         strings.TrimSpace(detail.KodePT),
				"prodi":           detail.Prodi,
				"jenjang":         detail.Jenjang,
				"jenis_kelamin":   detail.JenisKelamin,
				"status_akademik": detail.StatusSaatIni,
				"tanggal_masuk":   detail.TanggalMasuk,
			},
		})
	}

	api.GET("/verify/:nim", func(c *gin.Context) {
		handleVerify(c, c.Param("nim"), c.Query("kampus"))
	})

	api.POST("/verify", func(c *gin.Context) {
		var req struct {
			NIM    string `json:"nim" binding:"required"`
			Kampus string `json:"kampus"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			badRequest(c, "JSON body with field 'nim' is required")
			return
		}
		handleVerify(c, req.NIM, req.Kampus)
	})

	return r
}

func badRequest(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"status": "error", "code": "bad_request", "error": msg})
}

// writeError maps client errors to HTTP status codes and stable error codes.
func writeError(c *gin.Context, err error) {
	var amb *pddikti.AmbiguousError
	if errors.As(err, &amb) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"status":     "error",
			"code":       "ambiguous_nim",
			"error":      "NIM terdaftar di lebih dari satu perguruan tinggi; sertakan parameter 'kampus'",
			"candidates": amb.Candidates,
		})
		return
	}

	status, code := http.StatusBadGateway, "upstream_error"
	switch {
	case errors.Is(err, pddikti.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, pddikti.ErrInvalidID):
		status, code = http.StatusBadRequest, "invalid_id"
	case errors.Is(err, pddikti.ErrRateLimited):
		status, code = http.StatusTooManyRequests, "upstream_rate_limited"
		c.Header("Retry-After", "60")
	case errors.Is(err, pddikti.ErrBlocked):
		status, code = http.StatusBadGateway, "upstream_blocked"
	case errors.Is(err, pddikti.ErrTimeout):
		status, code = http.StatusGatewayTimeout, "upstream_timeout"
	}
	c.AbortWithStatusJSON(status, gin.H{"status": "error", "code": code, "error": err.Error()})
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
