// Package migration applies checksummed framework and application migrations.
package migration

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4/source"
)

type Entry struct {
	Version  uint
	Name     string
	Up, Down []byte
	Checksum string
}
type Source interface {
	Load() (map[string][]Entry, error)
}
type FileSource struct{ root fs.FS }

func NewFileSource(root fs.FS) (*FileSource, error) {
	if root == nil {
		return nil, errors.New("migration filesystem is required")
	}
	s := &FileSource{root: root}
	if _, err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileSource) Load() (map[string][]Entry, error) {
	result := make(map[string][]Entry, 2)
	pattern := regexp.MustCompile(`^([0-9]{14})_([a-z][a-z0-9_]*)\.(up|down)\.sql$`)
	for _, stream := range []string{"framework", "app"} {
		files, err := fs.ReadDir(s.root, stream)
		if err != nil {
			return nil, fmt.Errorf("read %s migration directory: %w", stream, err)
		}
		versions := make(map[uint]*Entry)
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".sql") {
				continue
			}
			if !file.Type().IsRegular() {
				return nil, errors.New("migration files must be regular files")
			}
			match := pattern.FindStringSubmatch(file.Name())
			if match == nil {
				return nil, fmt.Errorf("invalid migration filename: %s", file.Name())
			}
			if _, err := time.Parse("20060102150405", match[1]); err != nil {
				return nil, errors.New("migration version must be a valid UTC timestamp")
			}
			v, err := strconv.ParseUint(match[1], 10, 64)
			if err != nil || v == 0 || v > math.MaxInt64 {
				return nil, errors.New("invalid migration version")
			}
			entry := versions[uint(v)]
			if entry == nil {
				entry = &Entry{Version: uint(v), Name: match[2]}
				versions[uint(v)] = entry
			}
			if entry.Name != match[2] {
				return nil, errors.New("duplicate migration version")
			}
			path := stream + "/" + file.Name()
			f, err := s.root.Open(path)
			if err != nil {
				return nil, err
			}
			body, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
			closeErr := f.Close()
			if readErr != nil || closeErr != nil {
				return nil, errors.New("read migration failed")
			}
			if len(bytes.TrimSpace(body)) == 0 || len(body) > 1<<20 {
				return nil, errors.New("migration must contain SQL and be at most 1 MiB")
			}
			switch {
			case strings.HasSuffix(file.Name(), ".up.sql"):
				if entry.Up != nil {
					return nil, errors.New("duplicate up migration")
				}
				entry.Up = body
			case strings.HasSuffix(file.Name(), ".down.sql"):
				if entry.Down != nil {
					return nil, errors.New("duplicate down migration")
				}
				entry.Down = body
			default:
				return nil, errors.New("migration direction must be up or down")
			}
		}
		result[stream] = []Entry{}
		for _, entry := range versions {
			if entry.Up == nil || entry.Down == nil {
				return nil, fmt.Errorf("migration %d requires both directions", entry.Version)
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", entry.Version, entry.Name, entry.Up, entry.Down)))
			entry.Checksum = fmt.Sprintf("%x", sum)
			result[stream] = append(result[stream], *entry)
		}
		sort.Slice(result[stream], func(i, j int) bool { return result[stream][i].Version < result[stream][j].Version })
	}
	if len(result["framework"]) == 0 {
		return nil, errors.New("framework migration baseline is required")
	}
	return result, nil
}

type memorySource struct{ entries []Entry }

func newMemorySource(entries []Entry) *memorySource { return &memorySource{entries: entries} }
func (*memorySource) Open(string) (source.Driver, error) {
	return nil, errors.New("use instance construction")
}
func (*memorySource) Close() error { return nil }
func (s *memorySource) First() (uint, error) {
	if len(s.entries) == 0 {
		return 0, fs.ErrNotExist
	}
	return s.entries[0].Version, nil
}
func (s *memorySource) Prev(v uint) (uint, error) {
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].Version < v {
			return s.entries[i].Version, nil
		}
	}
	return 0, fs.ErrNotExist
}
func (s *memorySource) Next(v uint) (uint, error) {
	for _, e := range s.entries {
		if e.Version > v {
			return e.Version, nil
		}
	}
	return 0, fs.ErrNotExist
}
func (s *memorySource) ReadUp(v uint) (io.ReadCloser, string, error)   { return s.read(v, true) }
func (s *memorySource) ReadDown(v uint) (io.ReadCloser, string, error) { return s.read(v, false) }
func (s *memorySource) read(v uint, up bool) (io.ReadCloser, string, error) {
	for _, e := range s.entries {
		if e.Version == v {
			body := e.Down
			if up {
				body = e.Up
			}
			return io.NopCloser(bytes.NewReader(body)), e.Name, nil
		}
	}
	return nil, "", fs.ErrNotExist
}

var _ Source = (*FileSource)(nil)
var _ source.Driver = (*memorySource)(nil)
