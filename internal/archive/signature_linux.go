//go:build linux

package archive

import (
	"fmt"
	"os"
	"syscall"
)

// SignatureOf reads the change signature of the file at path (see FileSignature).
func SignatureOf(path string) (FileSignature, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileSignature{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileSignature{}, fmt.Errorf("stat %s: no device/inode information", path)
	}
	return FileSignature{
		Dev:     uint64(st.Dev), //nolint:unconvert // Dev's width differs by architecture
		Ino:     st.Ino,
		CtimeNs: st.Ctim.Nano(),
		Size:    fi.Size(),
		MtimeNs: fi.ModTime().UnixNano(),
	}, nil
}
