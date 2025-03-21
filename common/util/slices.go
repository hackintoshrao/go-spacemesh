package util

import "fmt"

// UniqueSliceStringer is a type that can be used to unique a slice of fmt.Stringer interface.
func UniqueSliceStringer[T fmt.Stringer](s []T) []T {
	inResult := make(map[string]struct{})
	var result []T
	for _, str := range s {
		if _, ok := inResult[str.String()]; !ok {
			inResult[str.String()] = struct{}{}
			result = append(result, str)
		}
	}
	return result
}
