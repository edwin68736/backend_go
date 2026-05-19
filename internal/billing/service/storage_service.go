package service

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"tukifac/pkg/tenantstorage"
)

// StorageMetadata contains information about the stored file.
type StorageMetadata struct {
	FilePath      string `json:"file_path"`
	AbsolutePath  string `json:"absolute_path"`
	Filename      string `json:"filename"`
	Base64Content string `json:"base64_content"`
}

// FileType represents the type of file to store (xml, cdr, signed).
type FileType string

const (
	FileTypeXML    FileType = "xml"
	FileTypeCDR    FileType = "cdr"
	FileTypeSigned FileType = "signed"
)

// BillingStorageService handles file storage for electronic invoices.
type BillingStorageService struct {
	BasePath string
}

// NewBillingStorageService creates a new instance of BillingStorageService.
func NewBillingStorageService(basePath string) *BillingStorageService {
	if basePath == "" {
		basePath = "./storage/invoices"
	}
	return &BillingStorageService{BasePath: basePath}
}

// SaveFile saves a file to the tenant's storage directory.
// Directory structure: /storage/tenants/{ruc}/{provider}/{fileType}
// File naming: RUC-TIPO-SERIE-NUMERO.xml (or .zip for CDR)
func (s *BillingStorageService) SaveFile(ruc, provider, docType, series, number string, fileType FileType, content []byte) (*StorageMetadata, error) {
	// 1. Validate inputs
	if ruc == "" || provider == "" || docType == "" || series == "" || number == "" {
		return nil, fmt.Errorf("missing required parameters for file storage")
	}

	// 2. Determine extension
	ext := "xml"
	if fileType == FileTypeCDR {
		ext = "zip" // CDRs are usually zipped XMLs
	}

	// 3. Construct paths
	safeRUC := tenantstorage.SanitizeRUC(ruc)
	dirPath := filepath.Join(s.BasePath, "tenants", safeRUC, tenantstorage.SanitizePathSegment(provider), string(fileType))
	filename := fmt.Sprintf("%s-%s-%s-%s.%s", safeRUC, docType, series, number, ext)
	fullPath := filepath.Join(dirPath, filename)

	// 4. Ensure directory exists
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("error creating storage directory: %w", err)
	}

	// 5. Save file
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	// 6. Return metadata
	relPath := filepath.ToSlash(filepath.Join("tenants", safeRUC, tenantstorage.SanitizePathSegment(provider), string(fileType), filename))
	return &StorageMetadata{
		FilePath:      relPath,
		AbsolutePath:  fullPath,
		Filename:      filename,
		Base64Content: base64.StdEncoding.EncodeToString(content),
	}, nil
}

