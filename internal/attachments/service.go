package attachments

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/store"
	_ "golang.org/x/image/webp"
)

const MaxSize = 10 << 20

var safeName = regexp.MustCompile(`[^a-zA-Z0-9._ -]+`)

type Service struct {
	DB   *pgxpool.Pool
	Root string
}
type Metadata struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

func ValidateStorage(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("create attachment storage: %w", err)
	}
	probe, err := os.CreateTemp(root, ".write-probe-")
	if err != nil {
		return fmt.Errorf("attachment storage is not writable: %w", err)
	}
	name := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	if err = os.Chmod(name, 0600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err = os.Remove(name); err != nil {
		return err
	}
	return nil
}

func (s *Service) Upload(ctx context.Context, tenant, project, ticket, name string, reader io.Reader) (Metadata, error) {
	limited := io.LimitReader(reader, MaxSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Metadata{}, err
	}
	if len(data) == 0 || len(data) > MaxSize {
		return Metadata{}, errors.New("attachment size must be 1..10 MiB")
	}
	media := http.DetectContentType(data)
	allowed := media == "image/jpeg" || media == "image/png" || media == "image/webp" || media == "application/pdf" || strings.HasPrefix(media, "text/plain")
	if !allowed || bytes.Contains(bytes.ToLower(data[:min(len(data), 512)]), []byte("<svg")) || bytes.Contains(bytes.ToLower(data[:min(len(data), 512)]), []byte("<html")) {
		return Metadata{}, errors.New("unsupported attachment content")
	}
	if strings.HasPrefix(media, "image/") {
		cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
			return Metadata{}, errors.New("invalid or oversized image dimensions")
		}
	}
	var random [32]byte
	_, _ = rand.Read(random[:])
	key := hex.EncodeToString(random[:])
	if err = os.MkdirAll(s.Root, 0700); err != nil {
		return Metadata{}, err
	}
	path := filepath.Join(s.Root, key)
	if err = os.WriteFile(path, data, 0600); err != nil {
		return Metadata{}, err
	}
	sum := sha256.Sum256(data)
	display := strings.Trim(safeName.ReplaceAllString(filepath.Base(name), "_"), " .")
	if display == "" {
		display = "attachment"
	}
	out := Metadata{ID: uuid.Must(uuid.NewV7()).String(), Name: display, MediaType: media, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	err = store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ticket_attachments WHERE project_id=$1 AND ticket_id=$2 AND deleted_at IS NULL`, project, ticket).Scan(&count); err != nil {
			return err
		}
		if count >= 5 {
			return errors.New("attachment limit reached")
		}
		_, err := tx.Exec(ctx, `INSERT INTO ticket_attachments(id,tenant_id,project_id,ticket_id,storage_key,display_name,media_type,size_bytes,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, out.ID, tenant, project, ticket, key, display, media, len(data), sum[:])
		return err
	})
	if err != nil {
		_ = os.Remove(path)
		return Metadata{}, err
	}
	return out, nil
}
func (s *Service) Open(ctx context.Context, tenant, project, id string) (io.ReadCloser, Metadata, error) {
	var key string
	var out Metadata
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,display_name,media_type,size_bytes,encode(sha256,'hex'),storage_key FROM ticket_attachments WHERE project_id=$1 AND id=$2 AND deleted_at IS NULL`, project, id).Scan(&out.ID, &out.Name, &out.MediaType, &out.Size, &out.SHA256, &key)
	})
	if err != nil {
		return nil, out, err
	}
	file, err := os.Open(filepath.Join(s.Root, key))
	return file, out, err
}
func (s *Service) Delete(ctx context.Context, tenant, project, id string) error {
	var key string
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE ticket_attachments SET deleted_at=now() WHERE project_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING storage_key`, project, id).Scan(&key); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO storage_deletions(storage_key) VALUES($1) ON CONFLICT(storage_key) DO NOTHING`, key)
		return err
	})
	if err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(s.Root, key)); err == nil || errors.Is(err, os.ErrNotExist) {
		_, _ = s.DB.Exec(ctx, `UPDATE storage_deletions SET completed_at=now() WHERE storage_key=$1`, key)
		return nil
	}
	return nil
}
func (s *Service) Cleanup(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT storage_key FROM storage_deletions WHERE completed_at IS NULL AND next_attempt_at<=now() LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return err
		}
		keys = append(keys, key)
	}
	for _, key := range keys {
		err = os.Remove(filepath.Join(s.Root, key))
		if err == nil || errors.Is(err, os.ErrNotExist) {
			_, _ = s.DB.Exec(ctx, `UPDATE storage_deletions SET completed_at=now() WHERE storage_key=$1`, key)
		} else {
			_, _ = s.DB.Exec(ctx, `UPDATE storage_deletions SET attempts=attempts+1,next_attempt_at=now()+interval '5 minutes' WHERE storage_key=$1`, key)
		}
	}
	return rows.Err()
}

func Disposition(name, media string) string {
	kind := "attachment"
	if strings.HasPrefix(media, "image/") {
		kind = "inline"
	}
	return fmt.Sprintf(`%s; filename="%s"`, kind, strings.ReplaceAll(name, `"`, "_"))
}
