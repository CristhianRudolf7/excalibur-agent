package platform

import (
	"reflect"
	"testing"
)

func TestProcessTreePostorder(t *testing.T) {
	for _, tt := range []struct {
		name    string
		root    uint32
		entries []processParent
		want    []uint32
	}{
		{"missing", 8, nil, []uint32{8}},
		{"descendants", 1, []processParent{{2, 1}, {3, 2}, {4, 1}, {9, 8}}, []uint32{3, 2, 4, 1}},
		{"cycle", 1, []processParent{{2, 1}, {1, 2}, {2, 1}}, []uint32{2, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := processTreePostorder(tt.root, tt.entries); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
