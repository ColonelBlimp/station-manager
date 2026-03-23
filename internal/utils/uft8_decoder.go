package utils

import (
	"golang.org/x/net/html/charset"
	"io"
	"strings"
)

// DecodeStringToUTF8 converts a given string to UTF-8 encoding, resolving any encoding issues if possible.
// Returns the UTF-8 decoded string or an error if the conversion fails.
func DecodeStringToUTF8(input string) (string, error) {
	// Fix possible encoding issues using a UTF-8 decoder
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
