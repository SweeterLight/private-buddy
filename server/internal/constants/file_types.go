package constants

// AllowedFileExtensions defines the file types supported for knowledge base document upload.
// These formats can be processed by the text extraction pipeline.
var AllowedFileExtensions = map[string]bool{
	".txt": true,
	".md":  true,
	".pdf": true,
}

// IsAllowedFileExtension checks if a file extension is in the allowed list.
func IsAllowedFileExtension(ext string) bool {
	return AllowedFileExtensions[ext]
}
