package types

// DatasetManifest declares an immutable, content-addressed evaluation dataset.
type DatasetManifest struct {
	ID          string                         `json:"id"`
	Version     string                         `json:"version"`
	Description string                         `json:"description,omitempty"`
	Files       map[string]DatasetManifestFile `json:"files"`
}

type DatasetManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// DatasetSnapshot is the verified input captured by an evaluation run.
type DatasetSnapshot struct {
	ID      string                         `json:"id"`
	Version string                         `json:"version"`
	Hash    string                         `json:"hash"`
	Files   map[string]DatasetManifestFile `json:"files"`
	QAPairs []*QAPair                      `json:"-"`
}

// QAPair represents a complete QA example with question, related passages and answer
type QAPair struct {
	QID      int      // Question ID
	Question string   // Question text
	PIDs     []int    // Related passage IDs
	Passages []string // Passage texts
	AID      int      // Answer ID
	Answer   string   // Answer text
}
