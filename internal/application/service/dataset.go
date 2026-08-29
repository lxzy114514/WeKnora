package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/parquet-go/parquet-go"
)

// DatasetService provides operations for working with datasets
type DatasetService struct {
	root string
}

var safeDatasetID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// NewDatasetService creates a new DatasetService instance
func NewDatasetService() interfaces.DatasetService {
	return NewDatasetServiceWithRoot(filepath.Join("dataset"))
}

// NewDatasetServiceWithRoot is an explicit test/deployment seam. Production
// uses ./dataset; tests can point at a temporary catalog without changing CWD.
func NewDatasetServiceWithRoot(root string) *DatasetService {
	return &DatasetService{root: root}
}

// TextInfo represents text data with ID in parquet format
type TextInfo struct {
	ID   int64  `parquet:"id"`   // Unique identifier
	Text string `parquet:"text"` // Text content
}

// RelsInfo represents question-passage relations in parquet format
type RelsInfo struct {
	QID int64 `parquet:"qid"` // Question ID
	PID int64 `parquet:"pid"` // Passage ID
}

// QaInfo represents question-answer relations in parquet format
type QaInfo struct {
	QID int64 `parquet:"qid"` // Question ID
	AID int64 `parquet:"aid"` // Answer ID
}

// GetDatasetByID retrieves QA pairs from dataset by ID
func (d *DatasetService) GetDatasetByID(ctx context.Context, datasetID string) ([]*types.QAPair, error) {
	logger.Info(ctx, "Start getting dataset by ID")
	logger.Infof(ctx, "Getting dataset with ID: %s", datasetID)

	snapshot, err := d.GetDatasetSnapshot(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	qaPairs := snapshot.QAPairs

	logger.Infof(ctx, "Retrieved %d QA pairs from dataset", len(qaPairs))
	return qaPairs, nil
}

// GetDatasetSnapshot resolves the default alias, verifies every declared file,
// and only then parses the dataset. This turns accidental data edits into an
// explicit version bump rather than a silent evaluation change.
func (d *DatasetService) GetDatasetSnapshot(ctx context.Context, datasetID string) (*types.DatasetSnapshot, error) {
	if datasetID == "" || datasetID == "default" {
		datasetID = "baseline-v1"
	}
	if !safeDatasetID.MatchString(datasetID) || strings.Contains(datasetID, "..") {
		return nil, fmt.Errorf("invalid dataset ID %q", datasetID)
	}

	manifestPath := filepath.Join(d.root, "manifests", datasetID+".json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read dataset manifest %q: %w", datasetID, err)
	}
	var manifest types.DatasetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode dataset manifest %q: %w", datasetID, err)
	}
	if manifest.ID != datasetID || strings.TrimSpace(manifest.Version) == "" {
		return nil, fmt.Errorf("dataset manifest identity mismatch: requested %q, declared %q", datasetID, manifest.ID)
	}

	required := []string{"queries", "corpus", "answers", "qrels", "qas"}
	paths := make(map[string]string, len(required))
	for _, role := range required {
		declared, ok := manifest.Files[role]
		if !ok || declared.Path == "" || declared.Size <= 0 || declared.SHA256 == "" {
			return nil, fmt.Errorf("dataset manifest %q is missing %s", datasetID, role)
		}
		fullPath, err := secureDatasetPath(d.root, declared.Path)
		if err != nil {
			return nil, fmt.Errorf("dataset %s path: %w", role, err)
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat dataset %s: %w", role, err)
		}
		if info.Size() != declared.Size {
			return nil, fmt.Errorf(
				"dataset %s size mismatch: expected %d, got %d", role, declared.Size, info.Size(),
			)
		}
		actual, err := hashFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("hash dataset %s: %w", role, err)
		}
		if !strings.EqualFold(actual, declared.SHA256) {
			return nil, fmt.Errorf("dataset %s hash mismatch: expected %s, got %s", role, declared.SHA256, actual)
		}
		paths[role] = fullPath
	}

	loaded, err := loadDataset(paths)
	if err != nil {
		return nil, err
	}
	if len(loaded.queries) == 0 || len(loaded.corpus) == 0 || len(loaded.answers) == 0 ||
		len(loaded.qrels) == 0 || len(loaded.qas) == 0 {
		return nil, fmt.Errorf("dataset manifest %q resolved to an incomplete dataset", datasetID)
	}
	loaded.PrintStats(ctx)
	canonical, _ := json.Marshal(manifest)
	sum := sha256.Sum256(canonical)
	return &types.DatasetSnapshot{
		ID:      manifest.ID,
		Version: manifest.Version,
		Hash:    hex.EncodeToString(sum[:]),
		Files:   manifest.Files,
		QAPairs: loaded.Iterate(),
	}, nil
}

// DefaultDataset loads and initializes the default dataset from parquet files
func DefaultDataset() dataset {
	datasetDir := filepath.Join("dataset", "samples")
	paths := map[string]string{
		"queries": filepath.Join(datasetDir, "queries.parquet"),
		"corpus":  filepath.Join(datasetDir, "corpus.parquet"),
		"answers": filepath.Join(datasetDir, "answers.parquet"),
		"qrels":   filepath.Join(datasetDir, "qrels.parquet"),
		"qas":     filepath.Join(datasetDir, "qas.parquet"),
	}
	loaded, err := loadDataset(paths)
	if err != nil {
		panic(err)
	}
	return loaded
}

func loadDataset(paths map[string]string) (dataset, error) {
	queries, err := loadParquet[TextInfo](paths["queries"])
	if err != nil {
		return dataset{}, fmt.Errorf("load queries: %w", err)
	}
	corpus, err := loadParquet[TextInfo](paths["corpus"])
	if err != nil {
		return dataset{}, fmt.Errorf("load corpus: %w", err)
	}
	answers, err := loadParquet[TextInfo](paths["answers"])
	if err != nil {
		return dataset{}, fmt.Errorf("load answers: %w", err)
	}
	qrels, err := loadParquet[RelsInfo](paths["qrels"])
	if err != nil {
		return dataset{}, fmt.Errorf("load qrels: %w", err)
	}
	qas, err := loadParquet[QaInfo](paths["qas"])
	if err != nil {
		return dataset{}, fmt.Errorf("load qas: %w", err)
	}

	res := dataset{
		queries: make(map[int64]string),  // qid -> question text
		corpus:  make(map[int64]string),  // pid -> passage text
		answers: make(map[int64]string),  // aid -> answer text
		qrels:   make(map[int64][]int64), // qid -> list of pid
		qas:     make(map[int64]int64),   // qid -> aid
	}
	for _, qi := range queries {
		res.queries[qi.ID] = qi.Text
	}
	for _, ci := range corpus {
		res.corpus[ci.ID] = ci.Text
	}
	for _, ai := range answers {
		res.answers[ai.ID] = ai.Text
	}
	for _, ri := range qrels {
		res.qrels[ri.QID] = append(res.qrels[ri.QID], ri.PID)
	}
	for _, qi := range qas {
		res.qas[qi.QID] = qi.AID
	}
	return res, nil
}

// dataset represents the in-memory dataset structure
type dataset struct {
	queries map[int64]string  // qid -> question text
	corpus  map[int64]string  // pid -> passage text
	answers map[int64]string  // aid -> answer text
	qrels   map[int64][]int64 // qid -> list of related pids
	qas     map[int64]int64   // qid -> aid
}

// Iterate generates QA pairs from the dataset
func (d *dataset) Iterate() []*types.QAPair {
	var pairs []*types.QAPair

	qids := make([]int64, 0, len(d.queries))
	for qid := range d.queries {
		qids = append(qids, qid)
	}
	sort.Slice(qids, func(i, j int) bool { return qids[i] < qids[j] })
	for _, qid := range qids {
		question := d.queries[qid]
		// Get answer info
		aid, hasAnswer := d.qas[qid]
		answer := ""
		if hasAnswer {
			answer = d.answers[aid]
		}

		// Get related passages
		pids := d.qrels[qid]
		var pidStr []int
		for _, pid := range pids {
			pidStr = append(pidStr, int(pid))
		}
		var passages []string
		for _, pid := range pids {
			passages = append(passages, d.corpus[pid])
		}

		pairs = append(pairs, &types.QAPair{
			QID:      int(qid),
			Question: question,
			PIDs:     pidStr,
			Passages: passages,
			AID:      int(aid),
			Answer:   answer,
		})
	}

	return pairs
}

func secureDatasetPath(root, declared string) (string, error) {
	if filepath.IsAbs(declared) {
		return "", errors.New("absolute paths are not allowed")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(declared)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes dataset root")
	}
	return fullPath, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GetContextForQID retrieves context passages for a given question ID
func (d *dataset) GetContextForQID(qid int64) ([]string, error) {
	pids, ok := d.qrels[qid]
	if !ok {
		return nil, errors.New("question ID not found")
	}

	var contextParts []string
	for _, pid := range pids {
		if text, exists := d.corpus[pid]; exists {
			contextParts = append(contextParts, text)
		}
	}

	return contextParts, nil
}

// PrintStats prints dataset statistics to the logger
func (d *dataset) PrintStats(ctx context.Context) {
	logger.Infof(ctx, "QA System Statistics:")
	logger.Infof(ctx, "- Total queries: %d", len(d.queries))
	logger.Infof(ctx, "- Total corpus passages: %d", len(d.corpus))
	logger.Infof(ctx, "- Total answers: %d", len(d.answers))

	// Calculate average passages per query
	totalRelations := 0
	for _, pids := range d.qrels {
		totalRelations += len(pids)
	}
	avgPassages := float64(totalRelations) / float64(len(d.qrels))
	logger.Infof(ctx, "- Average passages per query: %.2f", avgPassages)

	// Calculate coverage
	coveredQueries := len(d.qas)
	coverage := float64(coveredQueries) / float64(len(d.queries)) * 100
	logger.Infof(ctx, "- Answer coverage: %.2f%% (%d/%d)", coverage, coveredQueries, len(d.queries))
}

// PrintRandomQA prints a random question with its related passages and answer
func (d *dataset) PrintRandomQA() error {
	// Get a random qid
	var qid int64
	for k := range d.qas {
		qid = k
		break
	}
	if qid == 0 {
		return errors.New("no questions available")
	}

	// Get question text
	question, ok := d.queries[qid]
	if !ok {
		return fmt.Errorf("question %d not found", qid)
	}

	// Get answer info
	aid, ok := d.qas[qid]
	if !ok {
		return fmt.Errorf("answer for question %d not found", qid)
	}
	answer, ok := d.answers[aid]
	if !ok {
		return fmt.Errorf("answer %d not found", aid)
	}

	// Print formatted QA
	fmt.Println("===== Random QA =====")
	fmt.Printf("QID: %d\n", qid)
	fmt.Printf("Question: %s\n", question)

	// Print passages if available
	if pids, exists := d.qrels[qid]; exists && len(pids) > 0 {
		fmt.Println("\nRelated passages:")
		for i, pid := range pids {
			if text, exists := d.corpus[pid]; exists {
				fmt.Printf("\nPassage %d (PID: %d):\n%s\n", i+1, pid, text)
			}
		}
	} else {
		fmt.Println("\nNo related passages found")
	}

	// Print answer
	fmt.Printf("\nAnswer (AID: %d):\n%s\n", aid, answer)

	return nil
}

// loadParquet loads data from parquet file into specified type
func loadParquet[T any](filePath string) ([]T, error) {
	rows, err := parquet.ReadFile[T](filePath)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
