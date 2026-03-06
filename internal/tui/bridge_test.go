package tui

import "testing"

func TestParseBridgeRunning(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "empty output means no processes",
			output: "",
			want:   false,
		},
		{
			name:   "only slmodemd parent running",
			output: "100 slmodemd -e /usr/local/bin/slmodem-asterisk-bridge\n",
			want:   false,
		},
		{
			name:   "bridge child running alongside slmodemd parent",
			output: "100 slmodemd -e /usr/local/bin/slmodem-asterisk-bridge\n212 /usr/local/bin/slmodem-asterisk-bridge\n",
			want:   true,
		},
		{
			name:   "bridge child running without slmodemd in output",
			output: "212 /usr/local/bin/slmodem-asterisk-bridge\n",
			want:   true,
		},
		{
			name:   "bridge child with arguments",
			output: "100 slmodemd -e /usr/local/bin/slmodem-asterisk-bridge\n212 /usr/local/bin/slmodem-asterisk-bridge --dial 17186945647\n",
			want:   true,
		},
		{
			name:   "unrelated processes only",
			output: "300 some-other-process\n400 asterisk -f\n",
			want:   false,
		},
		{
			name:   "whitespace-only lines ignored",
			output: "  \n\n  \n",
			want:   false,
		},
		{
			name:   "bridge with bare binary name",
			output: "212 slmodem-asterisk-bridge\n",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBridgeRunning(tt.output)
			if got != tt.want {
				t.Errorf("parseBridgeRunning(%q) = %v, want %v",
					tt.output, got, tt.want)
			}
		})
	}
}
