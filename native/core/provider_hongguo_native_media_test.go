package core

import "testing"

func TestHongguoMediaShortEdgeQuality(t *testing.T) {
	tests := []struct {
		name       string
		width      int
		height     int
		definition int
		want       int
	}{
		{name: "portrait prefers short edge over long-edge definition", width: 720, height: 1280, definition: 1280, want: 720},
		{name: "landscape uses short edge and ignores conflicting definition", width: 1920, height: 1080, definition: 1920, want: 1080},
		{name: "square uses the common edge", width: 1080, height: 1080, definition: 1080, want: 1080},
		{name: "definition is fallback when dimensions are absent", definition: 720, want: 720},
		{name: "width is fallback when only width is present", width: 720, want: 720},
		{name: "height is fallback when only height is present", height: 720, want: 720},
		{name: "missing dimensions and definition return zero", want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hongguoMediaShortEdgeQuality(test.width, test.height, test.definition)
			if got != test.want {
				t.Fatalf("hongguoMediaShortEdgeQuality(%d, %d, %d) = %d, want %d",
					test.width, test.height, test.definition, got, test.want)
			}
		})
	}
}
