package repository

import "strings"

// LikeContains turns a search box value into a LIKE pattern that matches it
// anywhere, with LIKE's wildcards escaped (pair it with ESCAPE '\'). An empty
// or blank search returns "", which the queries read as "no filter".
func LikeContains(search string) string {
	search = strings.TrimSpace(search)
	if search == "" {
		return ""
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
	return "%" + escaped + "%"
}
