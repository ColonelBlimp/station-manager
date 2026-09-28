//go:build !linux

package archive

import "errors"

// SignatureOf is Linux-only: the change signature needs the device, inode and
// ctime, which this build has no portable way to read. A summary without a
// signature simply reads as possibly out of date.
func SignatureOf(string) (FileSignature, error) {
	return FileSignature{}, errors.New("archive file signature is not supported on this platform")
}
