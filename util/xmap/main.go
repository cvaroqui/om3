package xmap

import (
	"fmt"
	"reflect"
	"strings"
)

// Keys returns the slice of a map string keys.
func Keys(i interface{}) []string {
	m := reflect.ValueOf(i).MapKeys()
	l := make([]string, 0)
	for _, k := range m {
		l = append(l, k.String())
	}
	return l
}

func Copy[K, V comparable](m map[K]V) map[K]V {
	result := make(map[K]V)
	for k, v := range m {
		result[k] = v
	}
	return result
}

func Diff(old, new map[string]string) string {
	var changes []string

	// Check for added and changed
	for k, newV := range new {
		if oldV, exists := old[k]; !exists {
			changes = append(changes, fmt.Sprintf("+%s=%s", k, newV))
		} else if oldV != newV {
			changes = append(changes, fmt.Sprintf("~%s=%s->%s", k, oldV, newV))
		}
	}

	// Check for removed
	for k, oldV := range old {
		if _, exists := new[k]; !exists {
			changes = append(changes, fmt.Sprintf("-%s=%s", k, oldV))
		}
	}

	if len(changes) == 0 {
		return ""
	}
	return strings.Join(changes, ", ")
}
