package kb

import (
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

// TextSplitter splits text into chunks based on token count with overlap.
// Uses tiktoken for token counting (cl100k_base encoding, compatible with
// OpenAI models). Token counts for non-OpenAI models may have minor deviations.
type TextSplitter struct {
	chunkSize    int
	chunkOverlap int
	minChunkSize int
	tp           *tiktoken.Tiktoken
}

// NewTextSplitter creates a TextSplitter with the given chunk size, overlap, and minChunkSize.
// Chunks smaller than minChunkSize tokens are merged into the previous chunk.
func NewTextSplitter(chunkSize, chunkOverlap, minChunkSize int) *TextSplitter {
	tp, err := tiktoken.EncodingForModel("text-embedding-3-small")
	if err != nil {
		tp, _ = tiktoken.GetEncoding("cl100k_base")
	}
	return &TextSplitter{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		minChunkSize: minChunkSize,
		tp:           tp,
	}
}

// Chunk represents a text segment with position information.
type Chunk struct {
	Content     string
	ChunkIndex  int
	StartOffset int
	EndOffset   int
}

// Split splits text into chunks that respect token limits with overlap.
func (s *TextSplitter) Split(text string) []Chunk {
	if text == "" {
		return nil
	}

	paragraphs := s.splitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil
	}

	var chunks []Chunk
	var currentParts []string
	currentTokens := 0
	chunkIndex := 0
	startOffset := 0

	flush := func() {
		if len(currentParts) == 0 {
			return
		}
		content := strings.Join(currentParts, "\n\n")
		chunks = append(chunks, Chunk{
			Content:     content,
			ChunkIndex:  chunkIndex,
			StartOffset: startOffset,
			EndOffset:   startOffset + len(content),
		})
		chunkIndex++
		startOffset += len(content) - s.overlapCharCount(currentParts)
		currentParts = nil
		currentTokens = 0
	}

	for _, para := range paragraphs {
		paraTokens := len(s.tp.Encode(para, nil, nil))

		if currentTokens+paraTokens > s.chunkSize && len(currentParts) > 0 {
			flush()
		}

		if paraTokens > s.chunkSize {
			if len(currentParts) > 0 {
				flush()
			}
			subChunks := s.splitLargeParagraph(para, chunkIndex, startOffset)
			for _, sc := range subChunks {
				sc.ChunkIndex = chunkIndex
				chunks = append(chunks, sc)
				chunkIndex++
			}
			if len(subChunks) > 0 {
				last := subChunks[len(subChunks)-1]
				startOffset = last.EndOffset
			}
			continue
		}

		currentParts = append(currentParts, para)
		currentTokens += paraTokens
	}

	flush()

	chunks = s.mergeSmallTailChunks(chunks)
	return chunks
}

func (s *TextSplitter) splitParagraphs(text string) []string {
	lines := strings.Split(text, "\n")
	var paragraphs []string
	var current []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, strings.Join(current, "\n"))
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, strings.Join(current, "\n"))
	}
	return paragraphs
}

func (s *TextSplitter) splitLargeParagraph(para string, chunkIndex, startOffset int) []Chunk {
	words := strings.Fields(para)
	var chunks []Chunk
	var currentWords []string
	currentTokens := 0
	offset := startOffset

	for _, word := range words {
		wordTokens := len(s.tp.Encode(word, nil, nil))
		if currentTokens+wordTokens > s.chunkSize && len(currentWords) > 0 {
			content := strings.Join(currentWords, " ")
			chunks = append(chunks, Chunk{
				Content:     content,
				ChunkIndex:  chunkIndex,
				StartOffset: offset,
				EndOffset:   offset + len(content),
			})
			offset += len(content)
			currentWords = nil
			currentTokens = 0
		}
		currentWords = append(currentWords, word)
		currentTokens += wordTokens
	}

	if len(currentWords) > 0 {
		content := strings.Join(currentWords, " ")
		chunks = append(chunks, Chunk{
			Content:     content,
			ChunkIndex:  chunkIndex,
			StartOffset: offset,
			EndOffset:   offset + len(content),
		})
	}

	return chunks
}

func (s *TextSplitter) overlapCharCount(parts []string) int {
	if len(parts) == 0 || s.chunkOverlap == 0 {
		return 0
	}
	overlapTokens := 0
	overlapChars := 0
	for i := len(parts) - 1; i >= 0; i-- {
		tokens := len(s.tp.Encode(parts[i], nil, nil))
		if overlapTokens+tokens > s.chunkOverlap {
			break
		}
		overlapTokens += tokens
		overlapChars += len(parts[i]) + 2
	}
	return overlapChars
}

// mergeSmallTailChunks merges the last chunk into the previous one if its
// token count is below minChunkSize. This avoids producing tiny fragments
// that degrade retrieval quality. Re-indexes ChunkIndex after merging.
func (s *TextSplitter) mergeSmallTailChunks(chunks []Chunk) []Chunk {
	if s.minChunkSize <= 0 || len(chunks) <= 1 {
		return chunks
	}

	last := &chunks[len(chunks)-1]
	lastTokens := len(s.tp.Encode(last.Content, nil, nil))
	if lastTokens >= s.minChunkSize {
		return chunks
	}

	prev := &chunks[len(chunks)-2]
	prev.Content = prev.Content + "\n\n" + last.Content
	prev.EndOffset = last.EndOffset

	merged := chunks[:len(chunks)-1]
	for i := range merged {
		merged[i].ChunkIndex = i
	}
	return merged
}
