package utils

import (
	"fmt"
	"math/big"
)

var metricSuffixes = []string{"", "K", "M", "G", "T", "P", "E", "Z", "Y"}

// FormatCount formats a big.Int with metric suffixes.
func FormatCount(n *big.Int) string {
	if n.Sign() == 0 {
		return "0"
	}

	// Use big.Float for easy division and decimal formatting
	f := new(big.Float).SetInt(n)
	absF := new(big.Float).Abs(f)
	thousand := big.NewFloat(1000)

	idx := 0
	// Loop: divide by 1000 until the number is < 1000, OR we run out of suffixes
	for absF.Cmp(thousand) >= 0 && idx < len(metricSuffixes)-1 {
		absF.Quo(absF, thousand)
		f.Quo(f, thousand)
		idx++
	}

	// If it exceeds the largest suffix "Y", fallback to scientific notation (eXX)
	if absF.Cmp(thousand) >= 0 {
		return fmt.Sprintf("%.2e", f)
	}

	// For standard numbers < 1000, don't show decimal points
	if idx == 0 {
		return fmt.Sprintf("%.0f", f)
	}

	// Format with 1 decimal place (e.g., 1.5M).
	return fmt.Sprintf("%.1f%s", f, metricSuffixes[idx])
}
