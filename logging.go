package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	logFileName  = "win2mqtt.log"
	logMaxBytes  = 5 * 1024 * 1024
	logOpenFlags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
)

// setupLogging writes log output to dir/win2mqtt.log and, when one is available,
// the console. The file is truncated when it has grown past logMaxBytes.
func setupLogging(dir string) (func(), error) {
	path := filepath.Join(dir, logFileName)

	flags := logOpenFlags
	if info, err := os.Stat(path); err == nil && info.Size() > logMaxBytes {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, err
	}

	log.SetOutput(io.MultiWriter(file, consoleWriter()))
	return func() { file.Close() }, nil
}
