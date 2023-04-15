package utils

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

func MD5(str string) (string, error) {
	return readerMD5(strings.NewReader(str))
}

func FileMD5(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	return readerMD5(f)
}

func readerMD5(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
