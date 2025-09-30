package keep

import (
	"bufio"
	"strings"
)

// CountWords counts the number of words in a given string.
func CountWords(inputs ... string) int {
	count := 0
	for _, inputs := range inputs {
		scanner := bufio.NewScanner(strings.NewReader(inputs))
		scanner.Split(bufio.ScanWords)
		for scanner.Scan() {
			count++
		}
	}
	return count
}