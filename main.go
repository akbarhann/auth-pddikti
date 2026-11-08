package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auth-pddikti/internal/pddikti"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	client := pddikti.NewClient(pddikti.WithTimeout(10 * time.Second))

	router := gin.Default()

	// Enable CORS for frontend integration
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Health Check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "auth-pddikti",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// 1. Search student (by NIM or Name) -> returns candidate array
	// GET /api/search?q=akbar
	router.GET("/api/search", func(c *gin.Context) {
		query := c.Query("q")
		if strings.TrimSpace(query) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
			return
		}

		results, err := client.Search(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"query":  query,
			"total":  len(results),
			"data":   results,
		})
	})

	// 2. Get full student detail by encrypted ID
	// GET /api/detail/:id
	router.GET("/api/detail/:id", func(c *gin.Context) {
		id := c.Param("id")
		detail, err := client.GetDetail(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   detail,
		})
	})

	// 3. Instant Verification & Auto-fill by NIM (All-In-One Bridge)
	// GET /api/verify/:nim
	// POST /api/verify {"nim": "23010000001"}
	handleVerify := func(c *gin.Context, nim string) {
		nim = strings.TrimSpace(nim)
		if nim == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "NIM is required"})
			return
		}

		detail, err := client.VerifyNIM(nim)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		// Check active status
		isActive := strings.HasPrefix(strings.ToLower(detail.StatusSaatIni), "aktif")

		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"verified":  true,
			"is_active": isActive,
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

	router.GET("/api/verify/:nim", func(c *gin.Context) {
		handleVerify(c, c.Param("nim"))
	})

	router.POST("/api/verify", func(c *gin.Context) {
		var req struct {
			NIM string `json:"nim" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "JSON body with field 'nim' is required"})
			return
		}
		handleVerify(c, req.NIM)
	})

	log.Printf("[+] auth-pddikti service running on http://localhost:%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("[-] Failed to run server: %v", err)
	}
}
