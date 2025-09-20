package ingestion

import (
	"context"
	"errors"
	"testing"

	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

type fakeLoader struct {
	data string
	err  error
}

func (f fakeLoader) Load(path string) (string, error) {
	return f.data, f.err
}

type fakeNormalizer struct{}

func (fakeNormalizer) Normalize(sql string) string {
	return sql
}

type fakeAdminRepo struct {
	statements []string
	err        error
}

func (f *fakeAdminRepo) Exec(ctx context.Context, sql string) error {
	if f.err != nil {
		return f.err
	}
	f.statements = append(f.statements, sql)
	return nil
}

func TestRefreshLoadsFilesAndExecutesStatements(t *testing.T) {
	loader := fakeLoader{data: "SELECT 1;"}
	repo := &fakeAdminRepo{}
	uc := New(loader, fakeNormalizer{}, repo, Options{})

	if err := uc.Refresh(context.Background(), RefreshOptions{
		WilayahSQLPath:    "wilayah.sql",
		PostalSQLPath:     "postal.sql",
		BPSMappingSQLPath: "bps.sql",
	}); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if len(repo.statements) == 0 {
		t.Fatalf("expected statements to be executed")
	}
}

func TestRefreshPropagatesLoaderError(t *testing.T) {
	loader := fakeLoader{err: errors.New("read error")}
	uc := New(loader, fakeNormalizer{}, &fakeAdminRepo{}, Options{})

	err := uc.Refresh(context.Background(), RefreshOptions{WilayahSQLPath: "missing.sql", PostalSQLPath: "postal.sql", BPSMappingSQLPath: "bps.sql"})
	if err == nil || !sharederrors.Is(err, sharederrors.CodeInvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}
