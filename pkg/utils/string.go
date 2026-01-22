package utils

import "strings"

// Prints a string list as comma separated string.
func StrList(strs []string) string {
	str := ""
	for i, s := range strs {
		str += s
		if i < len(strs)-1 {
			str += ", "
		}
	}
	return str
}

func LastLine(s string) string {
	s = strings.Trim(s, "\n")
	lines := strings.Split(s, "\n")
	return strings.Trim(lines[len(lines)-1], "\n")
}
