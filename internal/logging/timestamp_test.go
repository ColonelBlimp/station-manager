package logging

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// File records carry millisecond timestamps (W-0011, operator ruling
// 2026-09-13): the CAT stop → status-answer → re-sent stop sequence resolves
// inside one second, and whole-second stamps could not order it. RFC 3339 with
// three fractional digits and the local offset, so existing readers that
// parse RFC 3339 still succeed.
func TestFileRecords_CarryMillisecondTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()
	cfg.WithTimestamp = true
	cfg.FileLogging = true
	cfg.ConsoleLogging = false
	service := &Service{WorkingDir: tmpDir, ConfigService: newTestConfigService(cfg)}
	require.NoError(t, service.Initialize())
	t.Cleanup(func() { _ = service.Close() })

	service.InfoWith().Msg("timestamp precision probe")

	files, err := filepath.Glob(filepath.Join(tmpDir, "*.log"))
	require.NoError(t, err)
	require.Len(t, files, 1, "expected exactly one log file in %s", tmpDir)
	raw, err := os.ReadFile(files[0])
	require.NoError(t, err)

	re := regexp.MustCompile(`"time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}(Z|[+-]\d\d:\d\d)"`)
	require.Regexp(t, re, string(raw), "file record time field must carry milliseconds; got %s", raw)
}
