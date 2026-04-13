package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const defaultBufferSize = 1024 * 1024 // 1 MB

// SHA256File returns a SHA-256 hash for the file path using chunk reads.
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	buffer := make([]byte, defaultBufferSize)

	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			if _, err := hasher.Write(buffer[:n]); err != nil {
				return "", err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
