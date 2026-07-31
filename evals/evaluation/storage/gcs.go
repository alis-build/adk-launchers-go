package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"cloud.google.com/go/storage"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/api/iterator"
)

const gcsEvalSetsDir = "evals/eval_sets"
const gcsEvalHistoryDir = "evals/eval_history"

// GCSManagers bundles GCS-backed eval set and result managers for one bucket.
type GCSManagers struct {
	Sets    *GCSEvalSetsManager
	Results *GCSEvalSetResultsManager
}

// NewGCSManagers creates GCS managers for gs://bucket. The bucket must exist.
func NewGCSManagers(ctx context.Context, bucketName string, client *storage.Client) (*GCSManagers, error) {
	if client == nil {
		var err error
		client, err = storage.NewClient(ctx)
		if err != nil {
			return nil, err
		}
	}
	bucket := client.Bucket(bucketName)
	if _, err := bucket.Attrs(ctx); err != nil {
		return nil, fmt.Errorf("bucket %q: %w", bucketName, err)
	}
	return &GCSManagers{
		Sets:    &GCSEvalSetsManager{bucket: bucket, bucketName: bucketName},
		Results: &GCSEvalSetResultsManager{bucket: bucket},
	}, nil
}

// GCSEvalSetsManager stores eval sets in GCS at {app}/evals/eval_sets/{id}.evalset.json.
type GCSEvalSetsManager struct {
	bucket     *storage.BucketHandle
	bucketName string
}

// blobName returns the GCS object path for an eval set JSON file.
func (m *GCSEvalSetsManager) blobName(appName, evalSetID string) (string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return "", err
	}
	if err := ValidateEvalSetID(evalSetID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s%s", appName, gcsEvalSetsDir, evalSetID, evalSetFileExtension), nil
}

func (m *GCSEvalSetsManager) GetEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	name, err := m.blobName(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	rc, err := m.bucket.Object(name).NewReader(context.Background())
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	set, err := models.ParseEvalSetFile(evalSetID, data)
	if err != nil {
		return nil, err
	}
	return &set, nil
}

func (m *GCSEvalSetsManager) CreateEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	existing, err := m.GetEvalSet(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("EvalSet %s already exists for app %s", evalSetID, appName)
	}
	name := evalSetID
	set := models.EvalSet{
		EvalSetID:         evalSetID,
		Name:              &name,
		EvalCases:         []models.EvalCase{},
		CreationTimestamp: float64(nowUnix()),
	}
	if err := m.writeSet(appName, evalSetID, set); err != nil {
		return nil, err
	}
	return &set, nil
}

func (m *GCSEvalSetsManager) UpdateEvalSet(appName string, set models.EvalSet) error {
	existing, err := m.GetEvalSet(appName, set.EvalSetID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, set.EvalSetID)
	}
	return m.writeSet(appName, set.EvalSetID, set)
}

func (m *GCSEvalSetsManager) writeSet(appName, evalSetID string, set models.EvalSet) error {
	name, err := m.blobName(appName, evalSetID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	w := m.bucket.Object(name).NewWriter(context.Background())
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (m *GCSEvalSetsManager) ListEvalSets(appName string) ([]string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s/%s/", appName, gcsEvalSetsDir)
	it := m.bucket.Objects(context.Background(), &storage.Query{Prefix: prefix})
	var ids []string
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		base := strings.TrimPrefix(attrs.Name, prefix)
		if strings.HasSuffix(base, evalSetFileExtension) {
			ids = append(ids, strings.TrimSuffix(base, evalSetFileExtension))
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: eval directory for app %q not found", ErrNotFound, appName)
	}
	sort.Strings(ids)
	return StringSliceForJSON(ids), nil
}

func (m *GCSEvalSetsManager) GetEvalCase(appName, evalSetID, evalCaseID string) (*models.EvalCase, error) {
	set, err := m.GetEvalSet(appName, evalSetID)
	if err != nil || set == nil {
		return nil, err
	}
	return getEvalCaseFromSet(set, evalCaseID), nil
}

func (m *GCSEvalSetsManager) AddEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	set, err := m.requireSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := addEvalCaseToSet(set, evalCase); err != nil {
		return err
	}
	return m.writeSet(appName, evalSetID, *set)
}

func (m *GCSEvalSetsManager) UpdateEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	set, err := m.requireSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := updateEvalCaseInSet(set, evalCase); err != nil {
		return err
	}
	return m.writeSet(appName, evalSetID, *set)
}

func (m *GCSEvalSetsManager) DeleteEvalCase(appName, evalSetID, evalCaseID string) error {
	set, err := m.requireSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := deleteEvalCaseFromSet(set, evalCaseID); err != nil {
		return err
	}
	return m.writeSet(appName, evalSetID, *set)
}

func (m *GCSEvalSetsManager) DeleteEvalSet(appName, evalSetID string) error {
	name, err := m.blobName(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := m.bucket.Object(name).Delete(context.Background()); errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	return err
}

// requireSet loads an eval set from GCS or returns ErrNotFound.
func (m *GCSEvalSetsManager) requireSet(appName, evalSetID string) (*models.EvalSet, error) {
	set, err := m.GetEvalSet(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	return set, nil
}

// GCSEvalSetResultsManager stores results at {app}/evals/eval_history/{id}.evalset_result.json.
type GCSEvalSetResultsManager struct {
	bucket *storage.BucketHandle
}

func (m *GCSEvalSetResultsManager) SaveEvalSetResult(appName, evalSetID string, caseResults []models.EvalCaseResult) (*models.EvalSetResult, error) {
	result := createEvalSetResult(appName, evalSetID, caseResults)
	name := fmt.Sprintf("%s/%s/%s%s", appName, gcsEvalHistoryDir, *result.EvalSetResultName, evalResultFileExtension)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	w := m.bucket.Object(name).NewWriter(context.Background())
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *GCSEvalSetResultsManager) GetEvalSetResult(appName, evalSetResultID string) (*models.EvalSetResult, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return nil, err
	}
	if err := ValidatePathSegment(evalSetResultID, "eval result id"); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s/%s/%s%s", appName, gcsEvalHistoryDir, evalSetResultID, evalResultFileExtension)
	rc, err := m.bucket.Object(name).NewReader(context.Background())
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, fmt.Errorf("%w: eval result %q not found", ErrNotFound, evalSetResultID)
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return parseEvalSetResultJSON(data)
}

func (m *GCSEvalSetResultsManager) ListEvalSetResults(appName string) ([]string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s/%s/", appName, gcsEvalHistoryDir)
	it := m.bucket.Objects(context.Background(), &storage.Query{Prefix: prefix})
	var ids []string
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		base := strings.TrimPrefix(attrs.Name, prefix)
		if strings.HasSuffix(base, evalResultFileExtension) {
			ids = append(ids, strings.TrimSuffix(base, evalResultFileExtension))
		}
	}
	sort.Strings(ids)
	return StringSliceForJSON(ids), nil
}
