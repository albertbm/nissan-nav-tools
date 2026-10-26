// Package humansize formats byte counts for the CLI and GUI logs.
package humansize

import "fmt"

// Format renders n as a short byte count, e.g. "4.0 KiB".
func Format(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	units := [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}
