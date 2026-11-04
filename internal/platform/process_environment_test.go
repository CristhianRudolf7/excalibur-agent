package platform

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestWindowsEnvironmentBlock(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  []string
		want string
	}{
		{"empty", nil, "\x00\x00"},
		{"sort and merge", []string{"z=1", "Path=old", "PATH=new", "a=", "invalid"}, "a=\x00PATH=new\x00z=1\x00\x00"},
		{"unicode and drive", []string{"name=🙂", "=C:=C:\\work"}, "=C:=C:\\work\x00name=🙂\x00\x00"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowsEnvironmentBlock(tt.env)
			want := utf16.Encode([]rune(tt.want))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, %v; want %v", got, err, want)
			}
		})
	}
	if _, err := windowsEnvironmentBlock([]string{"x=bad\x00value"}); err == nil {
		t.Fatal("NUL accepted")
	}
}
