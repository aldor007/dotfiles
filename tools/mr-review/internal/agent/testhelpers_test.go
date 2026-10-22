package agent

import (
	"os"
	"time"
)

// small os wrappers kept so tests read without importing os everywhere
func osOpenAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func mkdir(path string) { _ = os.MkdirAll(path, 0o755) }

func writeFile(path, s string) { _ = os.WriteFile(path, []byte(s), 0o600) }

func appendFile(path, s string) {
	f, err := osOpenAppend(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}

func osChtimes(path string, atime, mtime time.Time) error {
	return os.Chtimes(path, atime, mtime)
}
