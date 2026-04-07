package api

import (
	"slices"

	"github.com/tidwall/gjson"
)

func generatePatchRequest(current, desired gjson.Result, ignoredFields []string) Request {
	req := Request{}
	for key, value := range desired.Map() {
		if slices.Contains(ignoredFields, key) {
			continue
		}
		req[key] = value.Value()
	}
	for key, _ := range current.Map() {
		if slices.Contains(ignoredFields, key) {
			continue
		}
		if !desired.Get(key).Exists() {
			req[key] = nil
		}
	}
	return req
}

func compareObjs(a, b gjson.Result, ignoredFields []string) bool {
	for key, value := range a.Map() {
		if slices.Contains(ignoredFields, key) {
			continue
		}
		if !b.Get(key).Exists() || b.Get(key).String() != value.String() {
			return false
		}
	}
	for key, _ := range b.Map() {
		if slices.Contains(ignoredFields, key) {
			continue
		}
		if !a.Get(key).Exists() {
			return false
		}
	}
	return true
}
