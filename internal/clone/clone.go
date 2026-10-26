// Package clone copies a card image without ever touching the source.
package clone

import (
	"io"
	"os"
	"os/exec"
	"runtime"
)

// Copy writes dst as a copy of src. On macOS it tries an APFS clonefile via
// "cp -c", which shares blocks with src until either copy is written to;
// everywhere else, and if that fails, it falls back to a plain byte copy.
func Copy(src, dst string) error {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("cp", "-c", src, dst).Run(); err == nil {
			return nil
		}
	}
	return plainCopy(src, dst)
}

func plainCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
