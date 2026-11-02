package internal

import (
	"os"
	"strings"
)

const ()

func localHistoryPath(storagePath string) string {
	storagePath = strings.TrimSpace(os.ExpandEnv(storagePath))
	if storagePath == "" {
		return ""
	}
	return HistoryFile(storagePath)
}
