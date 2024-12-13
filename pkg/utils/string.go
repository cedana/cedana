package utils

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
