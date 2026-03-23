package meta

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Station-Manager/errors"
	"github.com/goccy/go-json"
)

type SqliteMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type SqliteMetadata struct {
	DatabaseFiles []SqliteMeta
}

func NewSqliteMetadata() *SqliteMetadata {
	return &SqliteMetadata{}
}

func (m *SqliteMetadata) Load(databaseDir string) error {
	const op errors.Op = "meta.LoadSqliteDatabaseList"
	databaseDir = strings.TrimSpace(databaseDir)
	if databaseDir == "" {
		return errors.New(op).Msg("Database directory cannot be empty")
	}

	data, err := readFile(filepath.Join(databaseDir, ".meta.json"))
	if err != nil {
		return errors.New(op).Err(err)
	}

	var meta []SqliteMeta
	if err = json.Unmarshal(data, &meta); err != nil {
		return errors.New(op).Err(err)
	}

	m.DatabaseFiles = meta

	return nil
}

func (m *SqliteMetadata) Save(databaseDir string, meta []SqliteMeta) error {
	const op errors.Op = "meta.SaveSqliteDatabaseList"
	databaseDir = strings.TrimSpace(databaseDir)
	if databaseDir == "" {
		return errors.New(op).Msg("Database directory cannot be empty")
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return errors.New(op).Err(err)
	}

	if err = writeFile(filepath.Join(databaseDir, ".meta.json"), data); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

//
//func (m *SqliteMetadata) Scan(databaseDir string) error {
//	const op errors.Op = "meta.ScanSqliteDatabaseList"
//
//	entries, err := os.ReadDir(databaseDir)
//	if err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	var slice []string
//	for _, entry := range entries {
//		if entry.IsDir() {
//			continue
//		}
//		matched, merr := filepath.Match("*.db", entry.Name())
//		if merr != nil {
//			return errors.New(op).Err(merr)
//		}
//		if matched {
//			slice = append(slice, strings.ReplaceAll(entry.Name(), ".db", ""))
//		}
//	}
//
//	m.DatabaseFiles = make([]SqliteMeta, len(slice))
//	return nil
//}

func readFile(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var data []byte
	if data, err = io.ReadAll(file); err != nil {
		return nil, err
	}

	return data, nil
}

func writeFile(filePath string, data []byte) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err = file.Write(data); err != nil {
		return err
	}

	return nil
}
