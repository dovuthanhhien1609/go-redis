package commands

import (
	"fmt"
	"strconv"
)

// parseInt64 parses s as a base-10 int64.
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// formatInt64 converts n to its decimal string representation.
func formatInt64(n int64) string {
	return fmt.Sprintf("%d", n)
}
