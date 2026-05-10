package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/constants"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/kb"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type KBHandler struct {
	crudKB  *service.CRUDBase[model.KnowledgeBase]
	crudDoc *service.CRUDBase[model.Document]
}

func NewKBHandler() *KBHandler {
	return &KBHandler{
		crudKB:  service.NewCRUDBase[model.KnowledgeBase]("Knowledge base"),
		crudDoc: service.NewCRUDBase[model.Document]("Document"),
	}
}

func (h *KBHandler) CreateKnowledgeBase(c *gin.Context) {
	var req schema.KnowledgeBaseCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	entity := model.KnowledgeBase{
		Name:              req.Name,
		Description:       req.Description,
		EmbeddingConfigID: req.EmbeddingConfigID,
	}

	if err := kb.CreateKnowledgeBase(&entity); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, entity)
}

func (h *KBHandler) ListKnowledgeBases(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudKB.GetMulti(skip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	type kbWithStats struct {
		model.KnowledgeBase
		DocumentCount int64 `json:"document_count"`
	}

	results := make([]kbWithStats, 0)
	for _, entity := range entities {
		var count int64
		database.DB.Model(&model.Document{}).Where("knowledge_base_id = ?", entity.ID).Count(&count)
		results = append(results, kbWithStats{
			KnowledgeBase: entity,
			DocumentCount: count,
		})
	}

	c.JSON(http.StatusOK, results)
}

func (h *KBHandler) GetKnowledgeBase(c *gin.Context) {
	entity, err := h.crudKB.Get(getPathID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entity)
}

func (h *KBHandler) UpdateKnowledgeBase(c *gin.Context) {
	var req schema.KnowledgeBaseUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	id := getPathID(c)
	entity, err := h.crudKB.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}

	updates := req.BuildUpdates()
	if len(updates) > 0 {
		if err := h.crudKB.Update(entity, updates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, entity)
}

func (h *KBHandler) DeleteKnowledgeBase(c *gin.Context) {
	id := getPathID(c)
	if err := kb.DeleteKnowledgeBase(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *KBHandler) ListDocuments(c *gin.Context) {
	kbID := getPathID(c)
	var documents []model.Document
	if err := database.DB.Where("knowledge_base_id = ?", kbID).Order("created_at DESC").Find(&documents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, documents)
}

func (h *KBHandler) UploadDocument(c *gin.Context) {
	kbID := getPathID(c)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "No file provided"})
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !constants.IsAllowedFileExtension(ext) {
		c.JSON(http.StatusBadRequest, gin.H{
			"detail": fmt.Sprintf("Unsupported file type: %s. Allowed types: .txt, .md, .pdf", ext),
		})
		return
	}

	kbDir := config.Get().GetKBDir()
	docDir := filepath.Join(kbDir, fmt.Sprintf("%d", kbID), "files")
	if err := os.MkdirAll(docDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	filePath := filepath.Join(docDir, header.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	doc := model.Document{
		KnowledgeBaseID: kbID,
		Title:           header.Filename,
		FilePath:        filePath,
		Status:          model.DocumentStatusPending,
	}

	if err := database.DB.Create(&doc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	kb.SubmitDocument(doc.ID)

	c.JSON(http.StatusCreated, doc)
}

func (h *KBHandler) GetDocument(c *gin.Context) {
	docID := getPathIDByParam(c, "doc_id")
	entity, err := h.crudDoc.Get(docID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entity)
}

func (h *KBHandler) DeleteDocument(c *gin.Context) {
	docID := getPathIDByParam(c, "doc_id")
	var doc model.Document
	if err := database.DB.First(&doc, docID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}

	if doc.FilePath != "" {
		os.Remove(doc.FilePath)
	}

	var chunkCount int64
	database.DB.Model(&model.DocumentChunk{}).Where("document_id = ? AND deleted = 0", docID).Count(&chunkCount)
	if chunkCount > 0 {
		database.DB.Model(&model.DocumentChunk{}).Where("document_id = ? AND deleted = 0", docID).Update("deleted", 1)
		database.DB.Model(&model.KnowledgeBase{}).Where("id = ?", doc.KnowledgeBaseID).
			Update("deleted_count", gorm.Expr("deleted_count + ?", chunkCount))
	}

	database.DB.Delete(&doc)

	c.JSON(http.StatusNoContent, nil)
}

func (h *KBHandler) SearchKB(c *gin.Context) {
	kbID := getPathID(c)
	var req schema.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	results, err := kb.SearchKB(c.Request.Context(), kbID, req.Query, req.TopK)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

func (h *KBHandler) SearchMultiKB(c *gin.Context) {
	var req schema.MultiKBSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	results, err := kb.SearchMultiKB(c.Request.Context(), req.KBIDs, req.Query, req.TopK)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

// isImageFile checks if the file extension indicates an image.
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}
	for _, e := range imageExts {
		if ext == e {
			return true
		}
	}
	return false
}
