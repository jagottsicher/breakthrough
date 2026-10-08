package logview

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		want Format
		in   []string
	}{
		{
			name: "json lines",
			want: FormatJSON,
			in: []string{
				`{"time":"2026-10-07T16:04:21Z","level":"error","msg":"timeout"}`,
				`{"time":"2026-10-07T16:04:22Z","level":"info","msg":"reconnecting"}`,
			},
		},
		{
			name: "rfc3164 syslog",
			want: FormatSyslogRFC3164,
			in: []string{
				"Oct  7 16:04:22 server kernel: eth0: link down",
				"Oct  7 16:04:23 server app[123]: started",
			},
		},
		{
			name: "rfc5424 syslog",
			want: FormatSyslogRFC5424,
			in: []string{
				"<34>1 2026-10-07T16:04:22Z server app 123 ID1 - timeout",
			},
		},
		{
			name: "generic timestamp+level",
			want: FormatGeneric,
			in: []string{
				"2026-10-07 16:04:23 ERROR database connection timeout",
				"2026-10-07 16:04:24 INFO  connection established",
			},
		},
		{
			name: "plain fallback",
			want: FormatPlain,
			in: []string{
				"just some unstructured text",
				"another line with no shape at all",
			},
		},
		{
			name: "empty sample",
			want: FormatPlain,
			in:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.in); got != tc.want {
				t.Errorf("Detect(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDetectBelowThresholdFallsBackToPlain(t *testing.T) {
	sample := []string{
		`{"time":"2026-10-07T16:04:21Z","level":"error","msg":"timeout"}`,
		"not json at all",
		"neither is this",
		"nor this one",
	}
	if got := Detect(sample); got != FormatPlain {
		t.Errorf("Detect = %v, want FormatPlain (only 1/4 lines matched JSON)", got)
	}
}
