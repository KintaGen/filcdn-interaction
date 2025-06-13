package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

var (
	pdpToolPath string
	db          *pgx.Conn
)

// ------------------------------------------------------------
// INITIALIZATION
// ------------------------------------------------------------
func init() {
	// -------- pdptool binary location --------
	pdpToolPath = os.Getenv("PDPTOOL_PATH")
	if pdpToolPath == "" {
		pdpToolPath = "/workspaces/kingen/curio/cmd/pdptool/pdptool"
	}
	fmt.Printf("[INIT] pdptool: %s\n", pdpToolPath)

	// -------- Postgres connection --------
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://filcdn:filcdnpassword@db:5432/filcdn_db"
	}
	var err error
	db, err = pgx.Connect(context.Background(), dsn)
	if err != nil {
		panic(fmt.Errorf("cannot connect to Postgres: %w", err))
	}
	fmt.Printf("[DB] Connected: %s\n", dsn)

	// Create all tables
	createTables := []string{
		// Existing table
		`CREATE TABLE IF NOT EXISTS file_cids (
			id SERIAL PRIMARY KEY,
			filename TEXT NOT NULL,
			cid TEXT NOT NULL,
			uploaded_at TIMESTAMPTZ DEFAULT NOW()
		);`,

		// Paper table
		`CREATE TABLE IF NOT EXISTS paper (
			cid TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			journal TEXT,
			year INTEGER,
			keywords TEXT[], -- PostgreSQL array type for string array
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`,

		// Spectrum table
		`CREATE TABLE IF NOT EXISTS spectrum (
			cid TEXT PRIMARY KEY,
			compound TEXT,
			technique_nmr_ir_ms TEXT, -- Using snake_case as typical in SQL
			metadata_json JSONB, -- JSONB is better than TEXT for JSON data
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`,

		// Genome table
		`CREATE TABLE IF NOT EXISTS genome (
			cid TEXT PRIMARY KEY,
			organism TEXT,
			assembly_version TEXT,
			notes TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`,
	}

	// Execute each CREATE TABLE statement
	for i, createSQL := range createTables {
		_, err = db.Exec(context.Background(), createSQL)
		if err != nil {
			panic(fmt.Errorf("failed to create table %d: %w", i+1, err))
		}
	}

	fmt.Println("[DB] All tables created successfully")
}

// newPDPCommand returns a command configured to run pdptool with debug output
func newPDPCommand(args ...string) *exec.Cmd {
	dir := filepath.Dir(pdpToolPath)
	cmd := exec.Command(pdpToolPath, args...)
	cmd.Dir = dir
	fmt.Printf("[CMD] Dir: %s, Executable: %s, Args: %v\n", dir, pdpToolPath, args)
	// List files in dir for debugging
	files, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("[DEBUG] Error reading dir %s: %v\n", dir, err)
	} else {
		fmt.Printf("[DEBUG] Files in %s: ", dir)
		for _, f := range files {
			fmt.Printf("%s ", f.Name())
		}
		fmt.Println()
	}
	return cmd
}

func main() {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.Use(cors.Default())

	// Combined orchestrator endpoint
	r.POST("/api/pdp", orchestrateHandler)

	// Specialized upload endpoints
	r.POST("/api/upload/paper", uploadAndAddPaperHandler)
	r.POST("/api/upload/genome", uploadAndAddGenomeHandler)
	r.POST("/api/upload/spectrum", uploadAndAddSpectrumHandler)

	// Generic query endpoint - flexible data retrieval
	r.GET("/api/data/:type", queryDataHandler)
	r.GET("/api/data/:type/:cid", getDataByIDHandler)

	// Legacy endpoints
	r.POST("/api/ping", pingHandler)
	r.POST("/api/proof-sets", createProofSetHandler)
	r.GET("/api/proof-sets/:txHash/status", getProofSetStatusHandler)
	r.POST("/api/upload", uploadFileHandler)
	r.POST("/api/proof-sets/:proofSetId/roots", addRootsHandler)
	r.POST("/api/proofset/upload-and-add-root", uploadAndAddRootHandler)
	r.GET("/api/cids", listCIDsHandler)

	fmt.Println("[START] Server listening on :8080")
	r.Run(":8080")
}

// queryDataHandler provides flexible querying for all data types
// GET /api/data/paper?search=quantum&year=2023&limit=10&offset=0
// GET /api/data/genome?organism=human&limit=5
// GET /api/data/spectrum?compound=caffeine&technique=NMR
func queryDataHandler(c *gin.Context) {
	dataType := c.Param("type")

	// Validate data type
	validTypes := map[string]bool{
		"paper":     true,
		"genome":    true,
		"spectrum":  true,
		"file_cids": true,
	}

	if !validTypes[dataType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid data type. Valid types: paper, genome, spectrum, file_cids",
		})
		return
	}

	// Parse query parameters
	limit := parseIntParam(c, "limit", 20)  // default 20
	offset := parseIntParam(c, "offset", 0) // default 0
	sortBy := c.DefaultQuery("sort", "created_at")
	sortOrder := c.DefaultQuery("order", "DESC")

	// Validate sort order
	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	fmt.Printf("[QUERY] Type: %s, Limit: %d, Offset: %d, Sort: %s %s\n",
		dataType, limit, offset, sortBy, sortOrder)

	var results interface{}
	var totalCount int
	var err error

	switch dataType {
	case "paper":
		results, totalCount, err = queryPapers(c, limit, offset, sortBy, sortOrder)
	case "genome":
		results, totalCount, err = queryGenomes(c, limit, offset, sortBy, sortOrder)
	case "spectrum":
		results, totalCount, err = querySpectrums(c, limit, offset, sortBy, sortOrder)
	case "file_cids":
		results, totalCount, err = queryFileCids(c, limit, offset, sortBy, sortOrder)
	}

	if err != nil {
		fmt.Printf("[QUERY ERROR] %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": results,
		"pagination": gin.H{
			"total":  totalCount,
			"limit":  limit,
			"offset": offset,
			"count":  getResultCount(results),
		},
		"sort": gin.H{
			"by":    sortBy,
			"order": sortOrder,
		},
	})
}

// getDataByIDHandler retrieves a specific record by CID
// GET /api/data/paper/QmX123...
// GET /api/data/genome/QmY456...
func getDataByIDHandler(c *gin.Context) {
	dataType := c.Param("type")
	cid := c.Param("cid")

	if cid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CID is required"})
		return
	}

	fmt.Printf("[GET BY ID] Type: %s, CID: %s\n", dataType, cid)

	var result interface{}
	var err error

	switch dataType {
	case "paper":
		result, err = getPaperByCID(cid)
	case "genome":
		result, err = getGenomeByCID(cid)
	case "spectrum":
		result, err = getSpectrumByCID(cid)
	case "file_cids":
		result, err = getFileCidByCID(cid)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid data type. Valid types: paper, genome, spectrum, file_cids",
		})
		return
	}

	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		fmt.Printf("[GET BY ID ERROR] %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}

// Query functions for each data type
func queryPapers(c *gin.Context, limit, offset int, sortBy, sortOrder string) (interface{}, int, error) {
	// Build WHERE clause based on query parameters
	var whereClauses []string
	var args []interface{}
	argIndex := 1

	// Search in title and journal
	if search := c.Query("search"); search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(title ILIKE $%d OR journal ILIKE $%d)", argIndex, argIndex))
		args = append(args, "%"+search+"%")
		argIndex++
	}

	// Filter by year
	if yearStr := c.Query("year"); yearStr != "" {
		if year, err := strconv.Atoi(yearStr); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("year = $%d", argIndex))
			args = append(args, year)
			argIndex++
		}
	}

	// Filter by journal
	if journal := c.Query("journal"); journal != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("journal ILIKE $%d", argIndex))
		args = append(args, "%"+journal+"%")
		argIndex++
	}

	// Filter by keyword
	if keyword := c.Query("keyword"); keyword != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("$%d = ANY(keywords)", argIndex))
		args = append(args, keyword)
		argIndex++
	}

	// Build query
	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Validate sortBy for papers
	validSortFields := map[string]bool{
		"created_at": true,
		"title":      true,
		"journal":    true,
		"year":       true,
		"cid":        true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM paper %s", whereClause)
	var totalCount int
	err := db.QueryRow(context.Background(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get results
	query := fmt.Sprintf(`
		SELECT cid, title, journal, year, keywords, created_at 
		FROM paper %s 
		ORDER BY %s %s 
		LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := db.Query(context.Background(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var papers []map[string]interface{}
	for rows.Next() {
		var cid, title string
		var journal *string
		var year *int
		var keywords []string
		var createdAt time.Time

		err := rows.Scan(&cid, &title, &journal, &year, &keywords, &createdAt)
		if err != nil {
			return nil, 0, err
		}

		paper := map[string]interface{}{
			"cid":        cid,
			"title":      title,
			"journal":    journal,
			"year":       year,
			"keywords":   keywords,
			"created_at": createdAt,
		}
		papers = append(papers, paper)
	}

	return papers, totalCount, nil
}

func queryGenomes(c *gin.Context, limit, offset int, sortBy, sortOrder string) (interface{}, int, error) {
	var whereClauses []string
	var args []interface{}
	argIndex := 1

	// Search in organism and notes
	if search := c.Query("search"); search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(organism ILIKE $%d OR notes ILIKE $%d)", argIndex, argIndex))
		args = append(args, "%"+search+"%")
		argIndex++
	}

	// Filter by organism
	if organism := c.Query("organism"); organism != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("organism ILIKE $%d", argIndex))
		args = append(args, "%"+organism+"%")
		argIndex++
	}

	// Filter by assembly version
	if assembly := c.Query("assembly"); assembly != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("assembly_version ILIKE $%d", argIndex))
		args = append(args, "%"+assembly+"%")
		argIndex++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Validate sortBy for genomes
	validSortFields := map[string]bool{
		"created_at":       true,
		"organism":         true,
		"assembly_version": true,
		"cid":              true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM genome %s", whereClause)
	var totalCount int
	err := db.QueryRow(context.Background(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get results
	query := fmt.Sprintf(`
		SELECT cid, organism, assembly_version, notes, created_at 
		FROM genome %s 
		ORDER BY %s %s 
		LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := db.Query(context.Background(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var genomes []map[string]interface{}
	for rows.Next() {
		var cid, organism string
		var assemblyVersion, notes *string
		var createdAt time.Time

		err := rows.Scan(&cid, &organism, &assemblyVersion, &notes, &createdAt)
		if err != nil {
			return nil, 0, err
		}

		genome := map[string]interface{}{
			"cid":              cid,
			"organism":         organism,
			"assembly_version": assemblyVersion,
			"notes":            notes,
			"created_at":       createdAt,
		}
		genomes = append(genomes, genome)
	}

	return genomes, totalCount, nil
}

func querySpectrums(c *gin.Context, limit, offset int, sortBy, sortOrder string) (interface{}, int, error) {
	var whereClauses []string
	var args []interface{}
	argIndex := 1

	// Search in compound
	if search := c.Query("search"); search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("compound ILIKE $%d", argIndex))
		args = append(args, "%"+search+"%")
		argIndex++
	}

	// Filter by compound
	if compound := c.Query("compound"); compound != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("compound ILIKE $%d", argIndex))
		args = append(args, "%"+compound+"%")
		argIndex++
	}

	// Filter by technique
	if technique := c.Query("technique"); technique != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("technique_nmr_ir_ms ILIKE $%d", argIndex))
		args = append(args, "%"+technique+"%")
		argIndex++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Validate sortBy for spectrums
	validSortFields := map[string]bool{
		"created_at":          true,
		"compound":            true,
		"technique_nmr_ir_ms": true,
		"cid":                 true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM spectrum %s", whereClause)
	var totalCount int
	err := db.QueryRow(context.Background(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get results
	query := fmt.Sprintf(`
		SELECT cid, compound, technique_nmr_ir_ms, metadata_json, created_at 
		FROM spectrum %s 
		ORDER BY %s %s 
		LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := db.Query(context.Background(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var spectrums []map[string]interface{}
	for rows.Next() {
		var cid, compound string
		var technique *string
		var metadataJson *string
		var createdAt time.Time

		err := rows.Scan(&cid, &compound, &technique, &metadataJson, &createdAt)
		if err != nil {
			return nil, 0, err
		}

		// Parse JSON metadata
		var metadata interface{}
		if metadataJson != nil && *metadataJson != "" {
			json.Unmarshal([]byte(*metadataJson), &metadata)
		}

		spectrum := map[string]interface{}{
			"cid":        cid,
			"compound":   compound,
			"technique":  technique,
			"metadata":   metadata,
			"created_at": createdAt,
		}
		spectrums = append(spectrums, spectrum)
	}

	return spectrums, totalCount, nil
}

func queryFileCids(c *gin.Context, limit, offset int, sortBy, sortOrder string) (interface{}, int, error) {
	var whereClauses []string
	var args []interface{}
	argIndex := 1

	// Search in filename
	if search := c.Query("search"); search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("filename ILIKE $%d", argIndex))
		args = append(args, "%"+search+"%")
		argIndex++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Validate sortBy for file_cids
	validSortFields := map[string]bool{
		"uploaded_at": true,
		"filename":    true,
		"cid":         true,
		"id":          true,
	}
	if !validSortFields[sortBy] {
		sortBy = "uploaded_at"
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM file_cids %s", whereClause)
	var totalCount int
	err := db.QueryRow(context.Background(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get results
	query := fmt.Sprintf(`
		SELECT id, filename, cid, uploaded_at 
		FROM file_cids %s 
		ORDER BY %s %s 
		LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := db.Query(context.Background(), query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var files []map[string]interface{}
	for rows.Next() {
		var id int
		var filename, cid string
		var uploadedAt time.Time

		err := rows.Scan(&id, &filename, &cid, &uploadedAt)
		if err != nil {
			return nil, 0, err
		}

		file := map[string]interface{}{
			"id":          id,
			"filename":    filename,
			"cid":         cid,
			"uploaded_at": uploadedAt,
		}
		files = append(files, file)
	}

	return files, totalCount, nil
}

// Individual record retrieval functions
func getPaperByCID(cid string) (interface{}, error) {
	var title string
	var journal *string
	var year *int
	var keywords []string
	var createdAt time.Time

	err := db.QueryRow(context.Background(),
		"SELECT cid, title, journal, year, keywords, created_at FROM paper WHERE cid = $1",
		cid).Scan(&cid, &title, &journal, &year, &keywords, &createdAt)

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"cid":        cid,
		"title":      title,
		"journal":    journal,
		"year":       year,
		"keywords":   keywords,
		"created_at": createdAt,
	}, nil
}

func getGenomeByCID(cid string) (interface{}, error) {
	var organism string
	var assemblyVersion, notes *string
	var createdAt time.Time

	err := db.QueryRow(context.Background(),
		"SELECT cid, organism, assembly_version, notes, created_at FROM genome WHERE cid = $1",
		cid).Scan(&cid, &organism, &assemblyVersion, &notes, &createdAt)

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"cid":              cid,
		"organism":         organism,
		"assembly_version": assemblyVersion,
		"notes":            notes,
		"created_at":       createdAt,
	}, nil
}

func getSpectrumByCID(cid string) (interface{}, error) {
	var compound string
	var technique *string
	var metadataJson *string
	var createdAt time.Time

	err := db.QueryRow(context.Background(),
		"SELECT cid, compound, technique_nmr_ir_ms, metadata_json, created_at FROM spectrum WHERE cid = $1",
		cid).Scan(&cid, &compound, &technique, &metadataJson, &createdAt)

	if err != nil {
		return nil, err
	}

	// Parse JSON metadata
	var metadata interface{}
	if metadataJson != nil && *metadataJson != "" {
		json.Unmarshal([]byte(*metadataJson), &metadata)
	}

	return map[string]interface{}{
		"cid":        cid,
		"compound":   compound,
		"technique":  technique,
		"metadata":   metadata,
		"created_at": createdAt,
	}, nil
}

func getFileCidByCID(cid string) (interface{}, error) {
	var id int
	var filename string
	var uploadedAt time.Time

	err := db.QueryRow(context.Background(),
		"SELECT id, filename, cid, uploaded_at FROM file_cids WHERE cid = $1",
		cid).Scan(&id, &filename, &cid, &uploadedAt)

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":          id,
		"filename":    filename,
		"cid":         cid,
		"uploaded_at": uploadedAt,
	}, nil
}

// Helper functions
func parseIntParam(c *gin.Context, param string, defaultValue int) int {
	if str := c.Query(param); str != "" {
		if val, err := strconv.Atoi(str); err == nil && val > 0 {
			return val
		}
	}
	return defaultValue
}

func getResultCount(results interface{}) int {
	switch r := results.(type) {
	case []map[string]interface{}:
		return len(r)
	default:
		return 0
	}
}

func uploadAndAddRootHandler(c *gin.Context) {
	fmt.Printf("[DEBUG] Starting uploadAndAddRootHandler\n")

	// Log the full request details
	fmt.Printf("[DEBUG] Request Method: %s\n", c.Request.Method)
	fmt.Printf("[DEBUG] Request URL: %s\n", c.Request.URL.String())
	fmt.Printf("[DEBUG] Request Headers: %+v\n", c.Request.Header)
	fmt.Printf("[DEBUG] Content-Type: %s\n", c.Request.Header.Get("Content-Type"))
	fmt.Printf("[DEBUG] Content-Length: %d\n", c.Request.ContentLength)

	// Log all form values
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		fmt.Printf("[DEBUG] Failed to parse multipart form: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] All form values: %+v\n", c.Request.Form)
		fmt.Printf("[DEBUG] All multipart form values: %+v\n", c.Request.MultipartForm.Value)
		if c.Request.MultipartForm.File != nil {
			fmt.Printf("[DEBUG] Form files: %+v\n", c.Request.MultipartForm.File)
		}
	}

	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	proofSetID := c.PostForm("proofSetID")

	fmt.Printf("[DEBUG] Form params - serviceUrl: %s, serviceName: %s, proofSetID: %s\n",
		serviceUrl, serviceName, proofSetID)

	if proofSetID == "" {
		fmt.Printf("[DEBUG] Missing proofSetID, returning 400\n")
		c.JSON(http.StatusBadRequest, gin.H{"error": "proofSetID is required"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Printf("[DEBUG] Failed to get form file: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	fmt.Printf("[DEBUG] File details - Name: %s, Size: %d bytes, Header: %+v\n",
		header.Filename, header.Size, header.Header)
	fmt.Printf("[DEBUG] Content-Type from header: %s\n", header.Header.Get("Content-Type"))
	fmt.Printf("[UPLOAD+ADD] %s → proofSet %s\n", header.Filename, proofSetID)

	// Detect if this is an encrypted file
	isEncrypted := strings.HasSuffix(strings.ToLower(header.Filename), ".enc")
	fmt.Printf("[DEBUG] File is encrypted: %v\n", isEncrypted)

	// write to temp
	fmt.Printf("[DEBUG] Creating temporary file\n")
	tmpFile, err := os.CreateTemp("", "pdp-upload-*")
	if err != nil {
		fmt.Printf("[DEBUG] Failed to create temp file: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tmpPath := tmpFile.Name()
	fmt.Printf("[DEBUG] Temp file created: %s\n", tmpPath)
	defer os.Remove(tmpPath)

	bytesWritten, err := io.Copy(tmpFile, file)
	if err != nil {
		fmt.Printf("[DEBUG] Failed to copy file to temp: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to copy file"})
		return
	} else {
		fmt.Printf("[DEBUG] Copied %d bytes to temp file\n", bytesWritten)
	}
	tmpFile.Close()

	// Verify temp file size
	if stat, err := os.Stat(tmpPath); err == nil {
		fmt.Printf("[DEBUG] Temp file size on disk: %d bytes\n", stat.Size())
	}

	// upload-file
	fmt.Printf("[DEBUG] Executing upload-file command\n")
	cmd := newPDPCommand("upload-file", "--service-url", serviceUrl, "--service-name", serviceName, tmpPath)
	fmt.Printf("[DEBUG] Command: %s\n", cmd.String())

	upOut, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[DEBUG] upload-file command failed: %v\n", err)
		fmt.Printf("[DEBUG] upload-file output: %s\n", string(upOut))
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(upOut)})
		return
	}

	fmt.Printf("[DEBUG] upload-file output: %s\n", string(upOut))
	lines := strings.Split(strings.TrimSpace(string(upOut)), "\n")
	rootCID := strings.TrimSpace(lines[len(lines)-1]) // full line
	fmt.Printf("[UPLOAD+ADD] rootCID=%s\n", rootCID)

	// For encrypted files, add a delay to allow service synchronization
	if isEncrypted {
		fmt.Printf("[DEBUG] Encrypted file detected, waiting for service synchronization...\n")
		time.Sleep(3 * time.Second)
	}

	// add-roots with retry logic
	fmt.Printf("[DEBUG] Executing add-roots command\n")
	var arOut []byte
	var addRootsSuccess bool
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("[DEBUG] add-roots attempt %d/%d\n", attempt, maxRetries)

		arCmd := newPDPCommand(
			"add-roots", "--service-url", serviceUrl, "--service-name", serviceName,
			"--proof-set-id", proofSetID, "--root", rootCID,
		)
		fmt.Printf("[DEBUG] Command: %s\n", arCmd.String())

		arOut, err = arCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("[DEBUG] add-roots attempt %d failed: %v\n", attempt, err)
			fmt.Printf("[DEBUG] add-roots output: %s\n", string(arOut))

			// Check if it's the "not found" error and we have more retries
			if strings.Contains(string(arOut), "not found or does not belong to service") && attempt < maxRetries {
				fmt.Printf("[DEBUG] Retrying after delay (attempt %d/%d)...\n", attempt, maxRetries)
				time.Sleep(time.Duration(attempt*2) * time.Second) // Exponential backoff
				continue
			}

			// If it's the last attempt or a different error, return the error
			if attempt == maxRetries {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": string(arOut),
					"details": map[string]interface{}{
						"rootCID":     rootCID,
						"attempts":    attempt,
						"isEncrypted": isEncrypted,
					},
				})
				return
			}
		} else {
			fmt.Printf("[DEBUG] add-roots succeeded on attempt %d\n", attempt)
			addRootsSuccess = true
			break
		}
	}

	if !addRootsSuccess {
		fmt.Printf("[DEBUG] add-roots failed after all retries\n")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": string(arOut),
			"details": map[string]interface{}{
				"rootCID":     rootCID,
				"maxRetries":  maxRetries,
				"isEncrypted": isEncrypted,
			},
		})
		return
	}

	fmt.Printf("[DEBUG] add-roots output: %s\n", string(arOut))

	// save mapping to DB
	fmt.Printf("[DEBUG] Saving file mapping to database\n")
	if _, err := db.Exec(context.Background(),
		"INSERT INTO file_cids (filename,cid) VALUES ($1,$2)", header.Filename, rootCID); err != nil {
		fmt.Printf("[DB ERROR] %v\n", err)
	} else {
		fmt.Printf("[DEBUG] Successfully saved to database: %s -> %s\n", header.Filename, rootCID)
	}

	fmt.Printf("[DEBUG] Request completed successfully\n")
	c.JSON(http.StatusOK, gin.H{
		"proofSetID":  proofSetID,
		"rootCID":     rootCID,
		"addRoots":    strings.TrimSpace(string(arOut)),
		"isEncrypted": isEncrypted,
	})
}

// uploadAndAddPaperHandler handles paper file uploads and database insertion
func uploadAndAddPaperHandler(c *gin.Context) {
	fmt.Printf("[DEBUG] Starting uploadAndAddPaperHandler\n")

	// Log request details
	fmt.Printf("[DEBUG] Request Method: %s\n", c.Request.Method)
	fmt.Printf("[DEBUG] Request URL: %s\n", c.Request.URL.String())

	// Parse form data
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		fmt.Printf("[DEBUG] Failed to parse multipart form: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse form"})
		return
	}

	// Get form parameters
	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	proofSetID := c.PostForm("proofSetID")
	title := c.PostForm("title")
	journal := c.PostForm("journal")
	yearStr := c.PostForm("year")
	keywordsStr := c.PostForm("keywords") // comma-separated

	fmt.Printf("[DEBUG] Form params - serviceUrl: %s, serviceName: %s, proofSetID: %s\n",
		serviceUrl, serviceName, proofSetID)
	fmt.Printf("[DEBUG] Paper metadata - title: %s, journal: %s, year: %s, keywords: %s\n",
		title, journal, yearStr, keywordsStr)

	if proofSetID == "" || title == "" {
		fmt.Printf("[DEBUG] Missing required fields, returning 400\n")
		c.JSON(http.StatusBadRequest, gin.H{"error": "proofSetID and title are required"})
		return
	}

	// Parse year
	var year *int
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil {
			year = &y
		}
	}

	// Parse keywords
	var keywords []string
	if keywordsStr != "" {
		keywords = strings.Split(strings.TrimSpace(keywordsStr), ",")
		for i, k := range keywords {
			keywords[i] = strings.TrimSpace(k)
		}
	}

	// Handle file upload (same as original)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Printf("[DEBUG] Failed to get form file: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	fmt.Printf("[DEBUG] File details - Name: %s, Size: %d bytes\n", header.Filename, header.Size)
	fmt.Printf("[UPLOAD+ADD PAPER] %s → proofSet %s\n", header.Filename, proofSetID)

	// Upload to storage (reuse existing logic)
	rootCID, err := uploadFileToStorage(file, header, serviceUrl, serviceName)
	if err != nil {
		fmt.Printf("[DEBUG] Upload failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Add to proof set (reuse existing logic)
	if err := addRootToProofSet(serviceUrl, serviceName, proofSetID, rootCID); err != nil {
		fmt.Printf("[DEBUG] Add roots failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save to database
	fmt.Printf("[DEBUG] Saving paper to database\n")
	if _, err := db.Exec(context.Background(),
		"INSERT INTO paper (cid, title, journal, year, keywords) VALUES ($1, $2, $3, $4, $5)",
		rootCID, title, journal, year, keywords); err != nil {
		fmt.Printf("[DB ERROR] Failed to save paper: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save paper metadata"})
		return
	}

	// Also save to file_cids for compatibility
	if _, err := db.Exec(context.Background(),
		"INSERT INTO file_cids (filename, cid) VALUES ($1, $2)", header.Filename, rootCID); err != nil {
		fmt.Printf("[DB ERROR] Failed to save file_cids: %v\n", err)
	}

	fmt.Printf("[DEBUG] Paper saved successfully: %s -> %s\n", title, rootCID)
	c.JSON(http.StatusOK, gin.H{
		"proofSetID": proofSetID,
		"rootCID":    rootCID,
		"title":      title,
		"journal":    journal,
		"year":       year,
		"keywords":   keywords,
	})
}

// uploadAndAddGenomeHandler handles genome file uploads and database insertion
func uploadAndAddGenomeHandler(c *gin.Context) {
	fmt.Printf("[DEBUG] Starting uploadAndAddGenomeHandler\n")

	// Parse form data
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		fmt.Printf("[DEBUG] Failed to parse multipart form: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse form"})
		return
	}

	// Get form parameters
	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	proofSetID := c.PostForm("proofSetID")
	organism := c.PostForm("organism")
	assemblyVersion := c.PostForm("assemblyVersion")
	notes := c.PostForm("notes")

	fmt.Printf("[DEBUG] Form params - serviceUrl: %s, serviceName: %s, proofSetID: %s\n",
		serviceUrl, serviceName, proofSetID)
	fmt.Printf("[DEBUG] Genome metadata - organism: %s, assemblyVersion: %s, notes: %s\n",
		organism, assemblyVersion, notes)

	if proofSetID == "" || organism == "" {
		fmt.Printf("[DEBUG] Missing required fields, returning 400\n")
		c.JSON(http.StatusBadRequest, gin.H{"error": "proofSetID and organism are required"})
		return
	}

	// Handle file upload
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Printf("[DEBUG] Failed to get form file: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	fmt.Printf("[UPLOAD+ADD GENOME] %s → proofSet %s\n", header.Filename, proofSetID)

	// Upload to storage
	rootCID, err := uploadFileToStorage(file, header, serviceUrl, serviceName)
	if err != nil {
		fmt.Printf("[DEBUG] Upload failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Add to proof set
	if err := addRootToProofSet(serviceUrl, serviceName, proofSetID, rootCID); err != nil {
		fmt.Printf("[DEBUG] Add roots failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save to database
	fmt.Printf("[DEBUG] Saving genome to database\n")
	if _, err := db.Exec(context.Background(),
		"INSERT INTO genome (cid, organism, assembly_version, notes) VALUES ($1, $2, $3, $4)",
		rootCID, organism, assemblyVersion, notes); err != nil {
		fmt.Printf("[DB ERROR] Failed to save genome: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save genome metadata"})
		return
	}

	// Also save to file_cids for compatibility
	if _, err := db.Exec(context.Background(),
		"INSERT INTO file_cids (filename, cid) VALUES ($1, $2)", header.Filename, rootCID); err != nil {
		fmt.Printf("[DB ERROR] Failed to save file_cids: %v\n", err)
	}

	fmt.Printf("[DEBUG] Genome saved successfully: %s -> %s\n", organism, rootCID)
	c.JSON(http.StatusOK, gin.H{
		"proofSetID":      proofSetID,
		"rootCID":         rootCID,
		"organism":        organism,
		"assemblyVersion": assemblyVersion,
		"notes":           notes,
	})
}

// uploadAndAddSpectrumHandler handles spectrum file uploads and database insertion
func uploadAndAddSpectrumHandler(c *gin.Context) {
	fmt.Printf("[DEBUG] Starting uploadAndAddSpectrumHandler\n")

	// Parse form data
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		fmt.Printf("[DEBUG] Failed to parse multipart form: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse form"})
		return
	}

	// Get form parameters
	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	proofSetID := c.PostForm("proofSetID")
	compound := c.PostForm("compound")
	technique := c.PostForm("technique")   // NMR, IR, MS, etc.
	metadataJson := c.PostForm("metadata") // JSON string

	fmt.Printf("[DEBUG] Form params - serviceUrl: %s, serviceName: %s, proofSetID: %s\n",
		serviceUrl, serviceName, proofSetID)
	fmt.Printf("[DEBUG] Spectrum metadata - compound: %s, technique: %s, metadata: %s\n",
		compound, technique, metadataJson)

	if proofSetID == "" || compound == "" {
		fmt.Printf("[DEBUG] Missing required fields, returning 400\n")
		c.JSON(http.StatusBadRequest, gin.H{"error": "proofSetID and compound are required"})
		return
	}

	// Validate JSON metadata if provided
	var metadataJsonb interface{}
	if metadataJson != "" {
		if err := json.Unmarshal([]byte(metadataJson), &metadataJsonb); err != nil {
			fmt.Printf("[DEBUG] Invalid JSON metadata: %v\n", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON metadata"})
			return
		}
	}

	// Handle file upload
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Printf("[DEBUG] Failed to get form file: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	fmt.Printf("[UPLOAD+ADD SPECTRUM] %s → proofSet %s\n", header.Filename, proofSetID)

	// Upload to storage
	rootCID, err := uploadFileToStorage(file, header, serviceUrl, serviceName)
	if err != nil {
		fmt.Printf("[DEBUG] Upload failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Add to proof set
	if err := addRootToProofSet(serviceUrl, serviceName, proofSetID, rootCID); err != nil {
		fmt.Printf("[DEBUG] Add roots failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save to database
	fmt.Printf("[DEBUG] Saving spectrum to database\n")
	if _, err := db.Exec(context.Background(),
		"INSERT INTO spectrum (cid, compound, technique_nmr_ir_ms, metadata_json) VALUES ($1, $2, $3, $4)",
		rootCID, compound, technique, metadataJson); err != nil {
		fmt.Printf("[DB ERROR] Failed to save spectrum: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save spectrum metadata"})
		return
	}

	// Also save to file_cids for compatibility
	if _, err := db.Exec(context.Background(),
		"INSERT INTO file_cids (filename, cid) VALUES ($1, $2)", header.Filename, rootCID); err != nil {
		fmt.Printf("[DB ERROR] Failed to save file_cids: %v\n", err)
	}

	fmt.Printf("[DEBUG] Spectrum saved successfully: %s -> %s\n", compound, rootCID)
	c.JSON(http.StatusOK, gin.H{
		"proofSetID": proofSetID,
		"rootCID":    rootCID,
		"compound":   compound,
		"technique":  technique,
		"metadata":   metadataJsonb,
	})
}

// Helper function to upload file to storage (extracted from common logic)
func uploadFileToStorage(file multipart.File, header *multipart.FileHeader, serviceUrl, serviceName string) (string, error) {
	// Detect if this is an encrypted file
	isEncrypted := strings.HasSuffix(strings.ToLower(header.Filename), ".enc")
	fmt.Printf("[DEBUG] File is encrypted: %v\n", isEncrypted)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "pdp-upload-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Copy file content
	_, err = io.Copy(tmpFile, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}
	tmpFile.Close()

	// Execute upload-file command
	cmd := newPDPCommand("upload-file", "--service-url", serviceUrl, "--service-name", serviceName, tmpPath)
	upOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("upload-file failed: %s", string(upOut))
	}

	// Extract root CID from output
	lines := strings.Split(strings.TrimSpace(string(upOut)), "\n")
	rootCID := strings.TrimSpace(lines[len(lines)-1])

	// For encrypted files, add delay
	if isEncrypted {
		fmt.Printf("[DEBUG] Encrypted file detected, waiting for service synchronization...\n")
		time.Sleep(3 * time.Second)
	}

	return rootCID, nil
}

// Helper function to add root to proof set (extracted from common logic)
func addRootToProofSet(serviceUrl, serviceName, proofSetID, rootCID string) error {
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("[DEBUG] add-roots attempt %d/%d\n", attempt, maxRetries)

		arCmd := newPDPCommand(
			"add-roots", "--service-url", serviceUrl, "--service-name", serviceName,
			"--proof-set-id", proofSetID, "--root", rootCID,
		)

		arOut, err := arCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("[DEBUG] add-roots attempt %d failed: %v\n", attempt, err)
			fmt.Printf("[DEBUG] add-roots output: %s\n", string(arOut))

			// Check if it's the "not found" error and we have more retries
			if strings.Contains(string(arOut), "not found or does not belong to service") && attempt < maxRetries {
				fmt.Printf("[DEBUG] Retrying after delay (attempt %d/%d)...\n", attempt, maxRetries)
				time.Sleep(time.Duration(attempt*2) * time.Second)
				continue
			}

			return fmt.Errorf("add-roots failed: %s", string(arOut))
		}

		fmt.Printf("[DEBUG] add-roots succeeded on attempt %d\n", attempt)
		return nil
	}

	return fmt.Errorf("add-roots failed after %d attempts", maxRetries)
}

// -------------------------------------------------------------------
//  3. List stored filename ↔ CID rows
//     GET /api/cids           -> entire table
//     GET /api/cids?filename=foo.png  -> filter by filename
//
// -------------------------------------------------------------------
func listCIDsHandler(c *gin.Context) {
	filename := c.Query("filename") // may be empty for "all"

	rows, err := db.Query(
		context.Background(),
		`SELECT filename, cid, uploaded_at
           FROM file_cids
          WHERE ($1 = '' OR filename = $1)
          ORDER BY uploaded_at DESC`,
		filename,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type entry struct {
		Filename   string    `json:"filename"`
		CID        string    `json:"cid"`
		UploadedAt time.Time `json:"uploaded_at"`
	}
	var result []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Filename, &e.CID, &e.UploadedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		result = append(result, e)
	}
	c.JSON(http.StatusOK, result)
}

// orchestrateHandler runs full PDP flow: create -> poll -> upload -> add-roots
func orchestrateHandler(c *gin.Context) {
	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	recordKeeper := c.PostForm("recordkeeper")
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Println("[ERROR] No file provided:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()
	fmt.Printf("[FLOW] Received file %s (size: %d)\n", header.Filename, header.Size)

	// Step 1: create-proof-set
	out, err := newPDPCommand(
		"create-proof-set",
		"--service-url", serviceUrl,
		"--service-name", serviceName,
		"--recordkeeper", recordKeeper,
	).CombinedOutput()
	fmt.Printf("[STEP1] create-proof-set output:\n%s\n", string(out))
	if err != nil {
		fmt.Printf("[ERROR] create-proof-set failed: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create-proof-set failed"})
		return
	}
	// Parse txHash
	lines := strings.Split(string(out), "\n")
	var txHash string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Location:") {
			idx := strings.Index(line, "/pdp/proof-sets/created/")
			if idx >= 0 {
				txHash = line[idx+len("/pdp/proof-sets/created/"):]
				txHash = strings.TrimSpace(txHash)
				break
			}
		}
	}

	if txHash == "" {
		fmt.Println("[ERROR] txHash not found in output")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "txHash parse failed"})
		return
	}
	fmt.Printf("[FLOW] Parsed txHash: %s\n", txHash)

	// Step 2: poll until ProofSet Created
	var proofSetID string
	count := 0
	for {
		count++
		fmt.Printf("[STEP2] Poll #%d for txHash %s\n", count, txHash)
		statusOut, _ := newPDPCommand(
			"get-proof-set-create-status",
			"--service-url", serviceUrl,
			"--service-name", serviceName,
			"--tx-hash", txHash,
		).CombinedOutput()
		sout := string(statusOut)
		fmt.Printf("[STEP2] status output:\n%s\n", sout)
		if strings.Contains(strings.ToLower(sout), "proofset created: true") {
			fmt.Println("[FLOW] ProofSet Created!")
			// extract ProofSet ID robustly (case insensitive)
			idx := strings.Index(strings.ToLower(sout), "proofset id: ")
			if idx >= 0 {
				idStart := idx + len("proofset id: ")
				rest := sout[idStart:]
				idEnd := strings.Index(rest, "\n")
				if idEnd < 0 {
					proofSetID = strings.TrimSpace(rest)
				} else {
					proofSetID = strings.TrimSpace(rest[:idEnd])
				}
				fmt.Printf("[FLOW] Parsed proofSetID: %s\n", proofSetID)
			}
			break
		}
		time.Sleep(3 * time.Second)
	}

	// Step 3: save file to temp and upload
	tmpFile, err := os.CreateTemp("", "pdp-upload-*")
	if err != nil {
		fmt.Println("[ERROR] tmp file create failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "temp file error"})
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	defer tmpFile.Close()
	fmt.Printf("[FLOW] Writing upload file to %s\n", tmpPath)
	io.Copy(tmpFile, file)

	uOut, err := newPDPCommand(
		"upload-file",
		"--service-url", serviceUrl,
		"--service-name", serviceName,
		tmpPath,
	).CombinedOutput()
	fmt.Printf("[STEP3] upload-file output:\n%s\n", string(uOut))
	if err != nil {
		fmt.Println("[ERROR] upload-file failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload-file failed"})
		return
	}
	// parse rootCID
	uLines := strings.Split(strings.TrimSpace(string(uOut)), "\n")
	rootCID := strings.SplitN(uLines[len(uLines)-1], ":", 2)[0]
	fmt.Printf("[FLOW] Parsed rootCID: %s\n", rootCID)

	// Step 4: add root
	arOut, err := newPDPCommand(
		"add-roots",
		"--service-url", serviceUrl,
		"--service-name", serviceName,
		"--proof-set-id", proofSetID,
		"--root", rootCID,
	).CombinedOutput()
	fmt.Printf("[STEP4] add-roots output:\n%s\n", string(arOut))
	if err != nil {
		fmt.Println("[ERROR] add-roots failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "add-roots failed"})
		return
	}

	// Final response
	c.JSON(http.StatusOK, gin.H{
		"txHash":     txHash,
		"proofSetID": proofSetID,
		"rootCID":    rootCID,
		"addRoots":   strings.TrimSpace(string(arOut)),
	})
}

// pingHandler checks connectivity
func pingHandler(c *gin.Context) {
	var req struct {
		ServiceURL  string `json:"serviceUrl" binding:"required"`
		ServiceName string `json:"serviceName" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := newPDPCommand(
		"ping",
		"--service-url", req.ServiceURL,
		"--service-name", req.ServiceName,
	).CombinedOutput()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(out)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": string(out)})
}

// createProofSetHandler invokes create-proof-set
func createProofSetHandler(c *gin.Context) {
	var req struct {
		ServiceURL   string `json:"serviceUrl" binding:"required"`
		ServiceName  string `json:"serviceName" binding:"required"`
		RecordKeeper string `json:"recordkeeper" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := newPDPCommand(
		"create-proof-set",
		"--service-url", req.ServiceURL,
		"--service-name", req.ServiceName,
		"--recordkeeper", req.RecordKeeper,
	).CombinedOutput()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(out)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"output": string(out)})
}

// getProofSetStatusHandler polls create status
func getProofSetStatusHandler(c *gin.Context) {
	txHash := c.Param("txHash")
	serviceUrl := c.Query("serviceUrl")
	serviceName := c.Query("serviceName")
	out, err := newPDPCommand(
		"get-proof-set-create-status",
		"--service-url", serviceUrl,
		"--service-name", serviceName,
		"--tx-hash", txHash,
	).CombinedOutput()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(out)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": string(out)})
}

// uploadFileHandler handles separate upload
func uploadFileHandler(c *gin.Context) {
	serviceUrl := c.PostForm("serviceUrl")
	serviceName := c.PostForm("serviceName")
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fmt.Println("[ERROR] No file provided:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()
	fmt.Printf("[UPLOAD] Received file %s (size: %d)\n", header.Filename, header.Size)

	tmpFile, err := os.CreateTemp("", "pdp-upload-*")
	if err != nil {
		fmt.Println("[ERROR] tmp file create failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "temp file error"})
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	defer tmpFile.Close()
	fmt.Printf("[UPLOAD] Writing upload file to %s\n", tmpPath)
	io.Copy(tmpFile, file)

	out, err := newPDPCommand(
		"upload-file",
		"--service-url", serviceUrl,
		"--service-name", serviceName,
		tmpPath,
	).CombinedOutput()
	fmt.Printf("[UPLOAD] upload-file output:\n%s\n", string(out))
	if err != nil {
		fmt.Println("[ERROR] upload-file failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload-file failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"output": string(out)})
}

// addRootsHandler attaches root CID to proof set
func addRootsHandler(c *gin.Context) {
	proofSetId := c.Param("proofSetId")
	var req struct {
		ServiceURL  string `json:"serviceUrl" binding:"required"`
		ServiceName string `json:"serviceName" binding:"required"`
		RootCID     string `json:"root" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := newPDPCommand(
		"add-roots",
		"--service-url", req.ServiceURL,
		"--service-name", req.ServiceName,
		"--proof-set-id", proofSetId,
		"--root", req.RootCID,
	).CombinedOutput()
	fmt.Printf("[ADDROOTS] add-roots output:\n%s\n", string(out))
	if err != nil {
		fmt.Println("[ERROR] add-roots failed:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "add-roots failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": string(out)})
}
