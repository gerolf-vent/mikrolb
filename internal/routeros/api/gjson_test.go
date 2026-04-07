package api

import (
	"reflect"
	"testing"

	"github.com/tidwall/gjson"
)

func TestCompareObjs(t *testing.T) {
	tests := []struct {
		name          string
		a             string
		b             string
		ignoredFields []string
		want          bool
	}{
		{
			name: "identical objects",
			a:    `{"address":"10.0.0.1/24","interface":"ether1"}`,
			b:    `{"address":"10.0.0.1/24","interface":"ether1"}`,
			want: true,
		},
		{
			name: "different values",
			a:    `{"address":"10.0.0.1/24","interface":"ether1"}`,
			b:    `{"address":"10.0.0.2/24","interface":"ether1"}`,
			want: false,
		},
		{
			name: "extra key in a",
			a:    `{"address":"10.0.0.1/24","interface":"ether1","extra":"value"}`,
			b:    `{"address":"10.0.0.1/24","interface":"ether1"}`,
			want: false,
		},
		{
			name: "extra key in b",
			a:    `{"address":"10.0.0.1/24","interface":"ether1"}`,
			b:    `{"address":"10.0.0.1/24","interface":"ether1","extra":"value"}`,
			want: false,
		},
		{
			name:          "different but ignored field",
			a:             `{".id":"*1","address":"10.0.0.1/24"}`,
			b:             `{".id":"*2","address":"10.0.0.1/24"}`,
			ignoredFields: []string{".id"},
			want:          true,
		},
		{
			name:          "extra key in a but ignored",
			a:             `{"address":"10.0.0.1/24","dynamic":"true"}`,
			b:             `{"address":"10.0.0.1/24"}`,
			ignoredFields: []string{"dynamic"},
			want:          true,
		},
		{
			name:          "extra key in b but ignored",
			a:             `{"address":"10.0.0.1/24"}`,
			b:             `{"address":"10.0.0.1/24","dynamic":"true"}`,
			ignoredFields: []string{"dynamic"},
			want:          true,
		},
		{
			name: "empty objects",
			a:    `{}`,
			b:    `{}`,
			want: true,
		},
		{
			name: "one empty one not",
			a:    `{}`,
			b:    `{"key":"value"}`,
			want: false,
		},
		{
			name:          "all fields ignored",
			a:             `{"a":"1","b":"2"}`,
			b:             `{"a":"3","b":"4"}`,
			ignoredFields: []string{"a", "b"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := gjson.Parse(tt.a)
			b := gjson.Parse(tt.b)
			got := compareObjs(a, b, tt.ignoredFields)
			if got != tt.want {
				t.Errorf("compareObjs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeneratePatchRequest(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		desired       string
		ignoredFields []string
		wantKeys      map[string]any // expected key-value pairs
		wantNilKeys   []string       // keys expected to be set to nil
	}{
		{
			name:    "update existing field",
			current: `{"address":"10.0.0.1/24","interface":"ether1"}`,
			desired: `{"address":"10.0.0.2/24","interface":"ether1"}`,
			wantKeys: map[string]any{
				"address":   "10.0.0.2/24",
				"interface": "ether1",
			},
		},
		{
			name:    "add new field",
			current: `{"address":"10.0.0.1/24"}`,
			desired: `{"address":"10.0.0.1/24","comment":"test"}`,
			wantKeys: map[string]any{
				"address": "10.0.0.1/24",
				"comment": "test",
			},
		},
		{
			name:        "remove field",
			current:     `{"address":"10.0.0.1/24","comment":"old"}`,
			desired:     `{"address":"10.0.0.1/24"}`,
			wantKeys:    map[string]any{"address": "10.0.0.1/24"},
			wantNilKeys: []string{"comment"},
		},
		{
			name:          "ignored fields excluded from patch",
			current:       `{".id":"*1","address":"10.0.0.1/24"}`,
			desired:       `{"address":"10.0.0.2/24"}`,
			ignoredFields: []string{".id"},
			wantKeys:      map[string]any{"address": "10.0.0.2/24"},
		},
		{
			name:          "ignored field in desired is excluded",
			current:       `{"address":"10.0.0.1/24"}`,
			desired:       `{".id":"*5","address":"10.0.0.2/24"}`,
			ignoredFields: []string{".id"},
			wantKeys:      map[string]any{"address": "10.0.0.2/24"},
		},
		{
			name:        "empty desired clears all current fields",
			current:     `{"address":"10.0.0.1/24","interface":"ether1"}`,
			desired:     `{}`,
			wantKeys:    map[string]any{},
			wantNilKeys: []string{"address", "interface"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := gjson.Parse(tt.current)
			desired := gjson.Parse(tt.desired)
			got := generatePatchRequest(current, desired, tt.ignoredFields)

			// Check expected key-value pairs
			for key, wantVal := range tt.wantKeys {
				gotVal, ok := got[key]
				if !ok {
					t.Errorf("missing key %q in patch request", key)
					continue
				}
				if !reflect.DeepEqual(gotVal, wantVal) {
					t.Errorf("key %q = %v, want %v", key, gotVal, wantVal)
				}
			}

			// Check nil keys
			for _, key := range tt.wantNilKeys {
				val, ok := got[key]
				if !ok {
					t.Errorf("expected key %q to be present (set to nil)", key)
					continue
				}
				if val != nil {
					t.Errorf("key %q = %v, want nil", key, val)
				}
			}
		})
	}
}
