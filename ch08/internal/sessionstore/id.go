package sessionstore

import (
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"time"
)

var idPattern = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{4}$`)

func NewID(now time.Time, random io.Reader) (string, error) {
	var b [2]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", now.Format("20060102-150405"), hex.EncodeToString(b[:])), nil
}

func ValidID(id string) bool {
	return idPattern.MatchString(id)
}
