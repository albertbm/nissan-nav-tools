// Package clone copies a card image without ever touching the source.
package clone

import (
	"io"
	"os"
	"os/exec"
	"runtime"

	"nissan-nav-tools/internal/progress"
)

func SameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

func Copy(src, dst string) error {
	return CopyWithProgress(src, dst, nil)
}

func CopyWithProgress(src, dst string, onProgress func(int64)) error {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("cp", "-c", src, dst).Run(); err == nil {
			if onProgress != nil {
				if fi, statErr := os.Stat(dst); statErr == nil {
					onProgress(fi.Size())
				}
			}
			// A clone keeps src's mode; a read-only original would leave dst unwritable.
			return os.Chmod(dst, 0o644)
		}
	}
	return plainCopy(src, dst, onProgress)
}

func plainCopy(src, dst string, onProgress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var r io.Reader = in
	if onProgress != nil {
		r = progress.NewCountingReader(in, onProgress)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
