//go:build windows

package disk

import (
	"fmt"
	"os"
	"runtime"
)

var errUnsupported = fmt.Errorf("reading a card by device is not supported on %s yet", runtime.GOOS)

func platformNode(id Info) string { return id.Identifier }

func platformList() ([]Info, error) { return nil, errUnsupported }

func platformOpen(id Info, write bool) (*os.File, error) { return nil, errUnsupported }

func platformUnmount(id string) error { return errUnsupported }

func platformEject(id string) error { return errUnsupported }
