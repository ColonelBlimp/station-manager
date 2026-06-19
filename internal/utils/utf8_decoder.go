package utils

import (
	"golang.org/x/net/html/charset"
	"io"
	"strings"
)

// DecodeStringToUTF8 reads input through a UTF-8 reader and returns the result.
// It does NOT sniff or convert from an unknown source charset — the label is
// always "utf-8" — so for already-UTF-8 input it is effectively a validating
// pass-through; bytes that aren't valid UTF-8 are replaced per the charset
// reader's policy. Returns an error only if constructing the reader or reading
// fails.
func DecodeStringToUTF8(input string) (string, error) {
	decoded, err := charset.NewReaderLabel("utf-8", strings.NewReader(input))
	if err != nil {
		return emptyString, err
	}

	// Read the output as a string
	decodedStr, err := io.ReadAll(decoded)
	if err != nil {
		return emptyString, err
	}

	return string(decodedStr), nil
}
